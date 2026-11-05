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
	_, ok := BashCheck(cmd, protected, root, root)
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
	_, ok := BashCheck(cmd, withRunDir, root, root)
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
	r, _ := BashCheck("aisdlc approve cases", protected, root, root)
	if !strings.Contains(r, "dev") {
		t.Error(r)
	}
	r, _ = BashCheck("rm aisdlc/features/PAY-1/gates.json", protected, root, root)
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

// TestBashBypassAttempts: mọi lệnh ghi vào cases.yaml đã khóa đều bị chặn (Java không chặn dạng cd + đường dẫn
// ngắn; bản Go theo dõi cd nên chặn).
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
		{"cd aisdlc/features && sed -i s/a/b/ PAY-1/cases.yaml", true},
		{"sed -i s/a/b/ ./aisdlc/../aisdlc/features/PAY-1/cases.yaml", true},
	}
	for _, c := range cases {
		eq(t, c.cmd, violatesReal(c.cmd), c.blocked)
	}
}

// hardened: danh sách bảo vệ hook truyền cho Bash (tương đối và tuyệt đối); config.yaml, risk-policy.yaml,
// .aisdlc/ và file trạng thái của mọi tính năng BashCheck luôn tự thêm.
var hardened = []string{
	"aisdlc/features/PAY-1/cases.yaml", "/abs/repo/aisdlc/features/PAY-1/cases.yaml",
	"aisdlc/features/PAY-1/gates.json", "/abs/repo/aisdlc/features/PAY-1/gates.json",
}

func TestBashHardeningBlocked(t *testing.T) {
	for _, c := range []string{
		`sed -i 's/unmanaged: .*/unmanaged: ["**"]/' aisdlc/config.yaml`,
		"echo x >> aisdlc/config.yaml",
		"cd aisdlc && echo x > config.yaml",
		"echo {} > aisdlc/features/PAY-456/gates.json",
		"sed -i s/a/b/ aisdlc/features/../features/PAY-123/gates.json",
		"cd aisdlc/features/PAY-1 && cp /tmp/c cases.yaml",
		"pushd aisdlc/features; sed -i s/a/b/ PAY-1/cases.yaml",
		`cd "$X" && echo {} > gates.json`,
		"rm -rf .aisdlc/grants",
		"cat x > aisdlc/risk-policy.yaml",
	} {
		_, ok := BashCheck(c, hardened, root, root)
		eq(t, c, ok, true)
	}
}

func TestBashHardeningAllowed(t *testing.T) {
	for _, c := range []string{
		"cat aisdlc/config.yaml",
		"grep -r foo aisdlc/features/PAY-1/cases.yaml",
		"cd internal && go test ./...",
		"git diff aisdlc/config.yaml",
		"cd aisdlc/features/PAY-1 && cat cases.yaml",
		"go build ./... > /tmp/build.log",
		"ls aisdlc/features/*/gates.json",
	} {
		_, ok := BashCheck(c, hardened, root, root)
		eq(t, c, ok, false)
	}
}

func TestBashHardeningReasonUsesNormalizedPath(t *testing.T) {
	r, ok := BashCheck("cd aisdlc && echo x > config.yaml", hardened, root, root)
	if !ok || !strings.Contains(r, "(aisdlc/config.yaml)") {
		t.Errorf("%v %q", ok, r)
	}
	// cwd của hook là thư mục con: đường dẫn tương đối tính theo nó
	_, ok = BashCheck("echo x > config.yaml", hardened, root, root+"/aisdlc")
	eq(t, "cwd=aisdlc", ok, true)
	_, ok = BashCheck("echo x > config.yaml", hardened, root, root)
	eq(t, "cwd=root", ok, false)
}

// TestBashHardeningEdgeCases: cách viết khác của cùng một lệnh ghi, và lệnh đọc không bị chặn thừa.
func TestBashHardeningEdgeCases(t *testing.T) {
	for _, c := range []string{
		"cd .aisdlc && rm active",
		"(cd aisdlc && echo x > config.yaml)",
		`echo "$(cd aisdlc && rm config.yaml)"`,
		"echo x >& aisdlc/./config.yaml",
		"echo x <> aisdlc/./config.yaml",
		`echo x > 'aisdlc'/conf\ig.yaml`,
		"dd if=/tmp/x of=aisdlc/./config.yaml",
		"pushd aisdlc && popd && pushd aisdlc/features && rm PAY-9/steps.json",
		"cd ~ && rm config.yaml",
		"cd /abs/repo/aisdlc && sed -i s/a/b/ config.yaml",
		// từ khóa shell đứng trước cd
		"{ cd aisdlc; echo x > config.yaml; }",
		"if true; then cd aisdlc; fi; echo x > config.yaml",
		"for d in aisdlc; do cd $d; done; echo x > config.yaml",
		"! cd aisdlc; echo x > config.yaml",
		"time cd aisdlc; echo x > config.yaml",
		"echo cd aisdlc; echo x > config.yaml", // có cd nhưng không áp dụng được: thư mục không xác định
		// cd có điều kiện, trong pipeline hoặc chạy nền: không chắc đã đổi thư mục
		"false && cd /tmp; cd aisdlc; echo x > config.yaml",
		"echo | cd /tmp; cd aisdlc; echo x > config.yaml",
		"cd /tmp & cd aisdlc; echo x > config.yaml",
		"if false; then cd /tmp; fi; cd aisdlc; echo x > config.yaml",
		// comment và heredoc không làm lệch nháy/ngoặc
		"true # '\ncd aisdlc && echo x > config.yaml # '",
		"true # (\ncd aisdlc\ntrue # )\necho x > config.yaml",
		"cat <<EOF\ndon't\nEOF\ncd aisdlc && echo x > config.yaml\n'",
		"cat <<-'EOF'\n\tdon't\n\tEOF\ncd aisdlc && echo x > config.yaml",
		"bash <<EOF\ncd aisdlc && echo x > config.yaml\nEOF",
		"sh <<'X'\necho x > aisdlc/config.yaml\nX",
		"env -i FOO=1 /bin/bash <<EOF\ncd aisdlc; rm config.yaml\nEOF",
		// ANSI-C quoting
		"cd aisdlc && echo x > $'config.yaml'",
		`cd aisdlc && echo x > $'conf\x69g.yaml'`,
		`cd aisdlc && echo x > $"config.yaml"`,
		`cd aisdlc && echo x > $'\u0063onfig.yaml'`,
		`echo x > $'aisdlc/\U00000063onfig.yaml'`,
	} {
		_, ok := BashCheck(c, hardened, root, root)
		eq(t, c, ok, true)
	}
	for _, c := range []string{
		"cd .aisdlc && ls | tee /tmp/x",
		"cd .aisdlc/run/PAY-1 && cp results.json /tmp/",
		"cp .aisdlc/run/PAY-1/results.json /tmp/r.json",
		"(cd aisdlc) && echo x > config.yaml",
		"cd /tmp && rm gates.json",
		"go test ./... 2>&1 | tee /tmp/out.log",
		"echo 'a#b' > /tmp/x # cd aisdlc; rm config.yaml",
		"cat <<EOF > /tmp/notes\ncd aisdlc\nEOF\necho x > config.yaml",
		"{ cd /tmp; rm gates.json; }",
		"grep -c x <<< 'config.yaml'",
		// thân heredoc của lệnh không phải shell là dữ liệu (mẫu commit của Claude Code)
		"git commit -m \"$(cat <<'EOF'\nfix(policy): chặn cp đè config.yaml\n\nCo-Authored-By: X\nEOF\n)\"",
		"cat > /tmp/notes.md <<'EOF'\n- mv risk.yaml sang chỗ khác\nEOF",
		"test -d build && cd build && make > out.log",
		"cd internal && go test ./... 2>&1 | tee /tmp/t.log",
	} {
		_, ok := BashCheck(c, hardened, root, root)
		eq(t, c, ok, false)
	}
}
