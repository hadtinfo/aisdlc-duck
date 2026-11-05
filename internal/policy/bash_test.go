package policy

import (
	"strings"
	"testing"
)

const root = "/abs/repo"

var protected = []string{
	"aisdlc/features/PAY-1/cases.yaml", "/abs/repo/aisdlc/features/PAY-1/cases.yaml",
	"aisdlc/features/PAY-1/gates.json", "/abs/repo/aisdlc/features/PAY-1/gates.json",
}

func violates(cmd string) bool {
	_, ok := BashCheck(cmd, protected, root)
	return ok
}

func eq(t *testing.T, cmd string, got, want bool) {
	t.Helper()
	if got != want {
		t.Errorf("%q: got %v, want %v", cmd, got, want)
	}
}

func TestWritesToProtectedPathsAreViolations(t *testing.T) {
	for _, c := range []string{
		"cat > aisdlc/features/PAY-1/cases.yaml <<EOF",
		"sed -i 's/400/200/' aisdlc/features/PAY-1/cases.yaml",
		"git checkout -- aisdlc/features/PAY-1/cases.yaml",
		"rm /abs/repo/aisdlc/features/PAY-1/gates.json",
	} {
		eq(t, c, violates(c), true)
	}
}

func TestDevOnlyCommandsAreViolations(t *testing.T) {
	for _, c := range []string{
		"aisdlc approve cases", "aisdlc step accept S1", "aisdlc reject brd", "aisdlc reopen cases",
		"aisdlc scope allow src/x", "cd x && aisdlc   approve brd",
	} {
		eq(t, c, violates(c), true)
	}
}

func TestReadsAndNormalCommandsAreFine(t *testing.T) {
	for _, c := range []string{
		"cat aisdlc/features/PAY-1/cases.yaml", "grep -n C1 aisdlc/features/PAY-1/cases.yaml",
		"aisdlc step finish S1", "aisdlc step start S1", "mvn -q test", "rm target/foo.txt",
	} {
		eq(t, c, violates(c), false)
	}
}

var withRunDir = []string{
	".aisdlc/", "/abs/repo/.aisdlc/",
	"aisdlc/features/PAY-1/cases.yaml", "/abs/repo/aisdlc/features/PAY-1/cases.yaml",
}

func violatesReal(cmd string) bool {
	_, ok := BashCheck(cmd, withRunDir, root)
	return ok
}

func TestFdDuplicationAndDevNullRedirectsAreNotWrites(t *testing.T) {
	for _, c := range []string{
		"tail -n 50 .aisdlc/run/PAY-1/maven.log 2>&1",
		"cat aisdlc/features/PAY-1/cases.yaml 2>/dev/null",
		"grep C1 aisdlc/features/PAY-1/cases.yaml >/dev/null 2>&1",
		"cat aisdlc/features/PAY-1/cases.yaml > /tmp/copy.yaml",
		"grep -n ERROR /abs/repo/.aisdlc/run/PAY-1/maven.log | head -20",
		"less .aisdlc/run/PAY-1/results.json &> /dev/null",
	} {
		eq(t, c, violatesReal(c), false)
	}
}

func TestRedirectToProtectedPathIsStillAWrite(t *testing.T) {
	for _, c := range []string{
		"echo x > aisdlc/features/PAY-1/cases.yaml",
		"echo x >> /abs/repo/aisdlc/features/PAY-1/cases.yaml 2>&1",
		"echo PAY-2 > .aisdlc/active",
		"tail .aisdlc/run/PAY-1/maven.log > .aisdlc/run/PAY-1/x.log",
		"cat > aisdlc/features/PAY-1/cases.yaml <<EOF",
		"echo x 2>/dev/null >\"aisdlc/features/PAY-1/cases.yaml\"",
		"sed -i 's/a/b/' aisdlc/features/PAY-1/cases.yaml 2>&1",
		"rm .aisdlc/active",
	} {
		eq(t, c, violatesReal(c), true)
	}
}

func TestReasonIsVietnamese(t *testing.T) {
	r, _ := BashCheck("aisdlc approve cases", protected, root)
	if !strings.Contains(r, "dev") {
		t.Error(r)
	}
	r, _ = BashCheck("rm aisdlc/features/PAY-1/gates.json", protected, root)
	if !strings.Contains(r, "gates.json") {
		t.Error(r)
	}
}

func TestReviewCommandIsNotDevOnly(t *testing.T) {
	eq(t, "review", violates("aisdlc review design"), false)
	eq(t, "review json", violates("aisdlc review design --json"), false)
}

func TestSessionAssignIsViolation(t *testing.T) {
	for _, c := range []string{"AISDLC_SESSION=x aisdlc status", "export AISDLC_SESSION=x", "env AISDLC_SESSION=x true"} {
		eq(t, c, violates(c), true)
	}
	eq(t, "read", violates("echo $AISDLC_SESSION"), false)
}

// TestBashBypassAttempts khẳng định hành vi của bản Java cho từng lệnh ghi vào cases.yaml đã khóa.
// blocked=false là chỗ Java KHÔNG chặn (heuristic chuỗi con): giữ nguyên, không thêm luật mới.
func TestBashBypassAttempts(t *testing.T) {
	cases := []struct {
		cmd     string
		blocked bool
	}{
		{"mv x aisdlc/features/PAY-1/cases.yaml", true},
		{"cp /tmp/c aisdlc/features/PAY-1/cases.yaml", true},
		{"echo a > aisdlc/features/PAY-1/cases.yaml", true},
		{"tee aisdlc/features/PAY-1/cases.yaml < x", true},
		{"sed -i s/a/b/ aisdlc/features/PAY-1/cases.yaml", true},
		{"cd aisdlc/features && sed -i s/a/b/ PAY-1/cases.yaml", false}, // Java: không chứa đường dẫn đầy đủ
		{"sed -i s/a/b/ ./aisdlc/../aisdlc/features/PAY-1/cases.yaml", true},
	}
	for _, c := range cases {
		eq(t, c.cmd, violatesReal(c.cmd), c.blocked)
	}
}
