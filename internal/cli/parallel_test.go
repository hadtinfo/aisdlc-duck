package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"aisdlc/internal/model"
)

// ParallelFeatureTest. Test nào cần phân giải tính năng gọi `submit brd` như Java: brd.md vẫn là template nên lệnh
// chạy tới cuối với exit 1 (có phát hiện).

func parallelRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	return r
}

func (r *testRepo) initAs(session, key string) {
	r.t.Helper()
	if session == "" {
		delete(r.env, "AISDLC_SESSION")
	} else {
		r.env["AISDLC_SESSION"] = session
	}
	r.ok(noConsole(), "init", key)
}

func (r *testRepo) twoFeatures() {
	r.t.Helper()
	r.initAs("a", "PAY-1")
	r.initAs("b", "PAY-2")
	delete(r.env, "AISDLC_SESSION")
}

func TestTwoSessionsSameDirKeepTheirFeature(t *testing.T) {
	r := parallelRepo(t)
	r.twoFeatures()
	r.env["AISDLC_SESSION"] = "a"
	r.ok(noConsole(), "status", "--json")
	contains(t, r.out, `"feature":"PAY-1"`)
	r.env["AISDLC_SESSION"] = "b"
	r.ok(noConsole(), "status", "--json")
	contains(t, r.out, `"feature":"PAY-2"`)
}

func TestNoSessionTwoOpenNoConsoleExits2(t *testing.T) {
	r := parallelRepo(t)
	r.twoFeatures()
	eq(t, r.run("submit", "brd"), 2)
	contains(t, r.err, "PAY-1", "PAY-2", "--feature")
}

func TestNoSessionTwoOpenConsolePicks(t *testing.T) {
	r := parallelRepo(t)
	r.twoFeatures()
	eq(t, r.runC(typed("2"), "submit", "brd"), 1)
	if !strings.HasPrefix(r.out, "[PAY-2]") {
		t.Fatalf("out: %s", r.out)
	}
	// gõ key thay vì số cũng được; lựa chọn sai thì exit 2
	eq(t, r.runC(typed("PAY-1"), "submit", "brd"), 1)
	if !strings.HasPrefix(r.out, "[PAY-1]") {
		t.Fatalf("out: %s", r.out)
	}
	eq(t, r.runC(typed("9"), "submit", "brd"), 2)
	contains(t, r.err, `Lựa chọn không hợp lệ: "9"`)
}

func TestExplicitFeatureWins(t *testing.T) {
	r := parallelRepo(t)
	r.twoFeatures()
	r.env["AISDLC_SESSION"] = "b"
	r.ok(noConsole(), "status", "--feature", "PAY-1", "--json")
	contains(t, r.out, `"feature":"PAY-1"`)
	r.ok(noConsole(), "status", "-f", "PAY-1", "--json")
	contains(t, r.out, `"feature":"PAY-1"`)
}

func TestExplicitFeatureOnSubcommand(t *testing.T) {
	r := parallelRepo(t)
	r.twoFeatures()
	eq(t, r.run("submit", "brd", "--feature", "PAY-2"), 1)
	if !strings.HasPrefix(r.out, "[PAY-2]") {
		t.Fatalf("out: %s", r.out)
	}
	eq(t, r.run("submit", "brd", "--feature", "pay-2"), 2)
}

func TestUnboundSessionOneOpenUsesIt(t *testing.T) {
	r := parallelRepo(t)
	r.initAs("", "PAY-1")
	r.env["AISDLC_SESSION"] = "fresh"
	r.ok(noConsole(), "status", "--json")
	contains(t, r.out, `"feature":"PAY-1"`)
}

func TestBindingMissingOnBranchErrors(t *testing.T) {
	r := parallelRepo(t)
	r.twoFeatures()
	must(t, os.Remove(filepath.Join(r.root, "aisdlc/features/PAY-1/feature.yaml")))
	r.env["AISDLC_SESSION"] = "a"
	eq(t, r.run("status"), 2)
	contains(t, r.err, "không có trên nhánh này")
}

func TestOutputStartsWithKey(t *testing.T) {
	r := parallelRepo(t)
	r.initAs("", "PAY-1")
	r.ok(noConsole(), "status")
	first, _, _ := strings.Cut(r.out, "\n")
	contains(t, first, "[PAY-1]")
}

func TestSameBranchSecondInitWarnsOnce(t *testing.T) {
	r := parallelRepo(t)
	r.initAs("", "PAY-1")
	r.ok(noConsole(), "init", "PAY-2")
	contains(t, r.out, "git worktree")
	r.ok(noConsole(), "init", "PAY-3")
	notContains(t, r.out, "git worktree")
}

func TestInitAndUseWriteBindingAndActive(t *testing.T) {
	r := parallelRepo(t)
	r.initAs("a", "PAY-1")
	k, ok, err := model.ReadSession(r.root, "a")
	must(t, err)
	eq(t, ok, true)
	eq(t, k, model.FeatureKey("PAY-1"))
	contains(t, r.read(".aisdlc/active"), "PAY-1")
	r.initAs("b", "PAY-2")
	r.env["AISDLC_SESSION"] = "b"
	r.ok(noConsole(), "use", "PAY-1")
	k, _, err = model.ReadSession(r.root, "b")
	must(t, err)
	eq(t, k, model.FeatureKey("PAY-1"))
}

// pay1MergedToBase: PAY-1 đã merge vào baseRef (base), PAY-2 chỉ có trong checkout; extraConfig nối vào config.
func (r *testRepo) pay1MergedToBase(extraConfig string) {
	r.t.Helper()
	r.initAs("", "PAY-1")
	r.initAs("", "PAY-2")
	r.write("aisdlc/config.yaml", strings.Replace(r.read("aisdlc/config.yaml"), "baseRef: origin/main",
		"baseRef: base", 1)+extraConfig)
	r.gitCmd("add", "-A")
	r.gitCmd("rm", "-r", "-q", "--cached", "aisdlc/features/PAY-2")
	r.gitCmd("commit", "-q", "-m", "merge PAY-1")
	r.gitCmd("branch", "-f", "base")
}

func (r *testRepo) open() []string {
	r.t.Helper()
	ctx := r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{})
	keys, err := NewFeatureResolver(ctx).OpenFeatures(r.root)
	must(r.t, err)
	var out []string
	for _, k := range keys {
		out = append(out, k.String())
	}
	return out
}

func eqList(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFeatureAtBaseRefIsNotOpen(t *testing.T) {
	r := parallelRepo(t)
	r.pay1MergedToBase("")
	eqList(t, r.open(), "PAY-2")
	eq(t, r.runC(noConsole(), "submit", "brd"), 1) // không còn mơ hồ
	if !strings.HasPrefix(r.out, "[PAY-2]") {
		t.Fatalf("out: %s", r.out)
	}
}

func TestUnreadableBaseRefFallsBackToReviewCheck(t *testing.T) {
	r := parallelRepo(t)
	r.pay1MergedToBase("")
	r.write("aisdlc/config.yaml", strings.Replace(r.read("aisdlc/config.yaml"), "baseRef: base",
		"baseRef: no-such-ref", 1))
	eqList(t, r.open(), "PAY-1", "PAY-2")
}

func TestWebModeFeatureAtBaseRefWithoutCacheIsNotOpen(t *testing.T) {
	r := parallelRepo(t)
	r.env["HOME"] = filepath.Join(r.root, "home")
	r.pay1MergedToBase("approvals: web\n")
	eq(t, r.exists(".aisdlc/web/gates/PAY-1.json"), false)
	eqList(t, r.open(), "PAY-2")
	// cùng repo khi không đọc được baseRef: thiếu cache = REVIEW chưa duyệt = còn mở
	r.write("aisdlc/config.yaml", strings.Replace(r.read("aisdlc/config.yaml"), "baseRef: base",
		"baseRef: no-such-ref", 1))
	eqList(t, r.open(), "PAY-1", "PAY-2")
}

func TestInitAndUseWarnWhenInClaudeCodeWithoutSession(t *testing.T) {
	r := parallelRepo(t)
	r.env["CLAUDECODE"] = "1"
	r.initAs("", "PAY-1")
	contains(t, r.err, "AISDLC_SESSION", "--feature PAY-1")
	r.ok(noConsole(), "use", "PAY-1")
	contains(t, r.err, "AISDLC_SESSION", "Khởi động lại")
	r.initAs("a", "PAY-2")
	notContains(t, r.err, "AISDLC_SESSION")
	delete(r.env, "CLAUDECODE")
	r.initAs("", "PAY-3")
	notContains(t, r.err, "AISDLC_SESSION")
}

func TestStatusWithoutSessionListsOpenFeaturesAndSessions(t *testing.T) {
	r := parallelRepo(t)
	r.twoFeatures()
	r.ok(noConsole(), "status")
	contains(t, r.out, "PAY-1", "PAY-2", "a", "b")
	contains(t, r.out, "Có 2 tính năng đang mở", "  PAY-1  session: a", "  PAY-2  session: b")
	r.ok(noConsole(), "status", "--json")
	eq(t, strings.TrimSpace(r.out),
		`{"open":[{"feature":"PAY-1","sessions":["a"]},{"feature":"PAY-2","sessions":["b"]}]}`)
}
