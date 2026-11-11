package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aisdlc/internal/model"
	"aisdlc/internal/risk"
)

// SetupCommandTest. Bản Go không còn --package (spec §3.1: setup không hỏi package Java, không sinh
// AisdlcCasesTest.java), nên các lệnh setup ở đây bỏ cờ đó.

func setupRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.presetFlow = true // kiểm đúng file mẫu setup chép vào repo
	return r
}

func presetYAML(t *testing.T, name string) string {
	y, err := risk.PresetYAML(name, risk.PresetLatestVersion(name))
	must(t, err)
	return y
}

func approvalsOf(t *testing.T, root string) string {
	c, err := model.LoadRepoConfig(root)
	must(t, err)
	return c.Approvals
}

func TestDefaultPresetIsGeneric(t *testing.T) {
	r := setupRepo(t)
	eq(t, r.run("setup"), 0)
	eq(t, r.read("aisdlc/risk-policy.yaml"), presetYAML(t, "generic"))
	p, err := risk.ParsePolicy(r.read("aisdlc/risk-policy.yaml"))
	must(t, err)
	eq(t, p.FlowFor("low", "brd"), "auto")
}

func TestPresetBankingWritesPolicy(t *testing.T) {
	r := setupRepo(t)
	eq(t, r.run("setup", "--preset", "banking"), 0)
	eq(t, r.read("aisdlc/risk-policy.yaml"), presetYAML(t, "banking"))
	contains(t, r.out, "Đã tạo aisdlc/risk-policy.yaml")
}

func TestRerunDoesNotOverwrite(t *testing.T) {
	r := setupRepo(t)
	r.write("aisdlc/risk-policy.yaml", "# của dự án\n")
	eq(t, r.run("setup", "--preset", "banking"), 0)
	eq(t, r.read("aisdlc/risk-policy.yaml"), "# của dự án\n")
	contains(t, r.out, "Giữ nguyên aisdlc/risk-policy.yaml")
}

func TestUnknownPresetExit2(t *testing.T) {
	r := setupRepo(t)
	eq(t, r.run("setup", "--preset", "nope"), 2)
	contains(t, r.err, "nope", "generic")
	eq(t, r.exists("aisdlc/risk-policy.yaml"), false)
	eq(t, r.exists("aisdlc/config.yaml"), false)
}

func TestApprovalsDefaultLocalWhenNotLoggedIn(t *testing.T) {
	r := setupRepo(t)
	r.setupDefaultApprovals = true
	eq(t, r.run("setup"), 0)
	eq(t, approvalsOf(t, r.root), "local")
	notContains(t, r.read("aisdlc/config.yaml"), "approvals:")
	contains(t, r.out, "Chế độ duyệt: local")
}

func TestApprovalsDefaultWebWhenLoggedIn(t *testing.T) {
	r := setupRepo(t)
	r.setupDefaultApprovals = true
	home := filepath.Join(r.root, "home")
	must(t, os.MkdirAll(filepath.Join(home, ".config/aisdlc"), 0o755))
	must(t, os.WriteFile(filepath.Join(home, ".config/aisdlc/web.json"),
		[]byte(`{"url":"http://web.test","token":"t"}`), 0o600))
	r.env["HOME"] = home
	eq(t, r.run("setup"), 0)
	eq(t, approvalsOf(t, r.root), "web")
	contains(t, r.out, "Chế độ duyệt: web", "AISDLC_CI_TOKEN")
	notContains(t, r.out, "chưa có cấu hình web")
}

func TestApprovalsFlagOverridesDefault(t *testing.T) {
	r := setupRepo(t)
	r.setupDefaultApprovals = true
	eq(t, r.run("setup", "--approvals", "web"), 0)
	eq(t, approvalsOf(t, r.root), "web")
	contains(t, r.out, "chưa có cấu hình web: chạy `aisdlc web login`")
}

func TestApprovalsFlagInvalidExit2(t *testing.T) {
	r := setupRepo(t)
	eq(t, r.run("setup", "--approvals", "gitlab"), 2)
	contains(t, r.err, "--approvals chỉ nhận web hoặc local")
	eq(t, r.exists("aisdlc/config.yaml"), false)
}

func TestApprovalsExistingConfigKept(t *testing.T) {
	r := setupRepo(t)
	r.setupDefaultApprovals = true
	r.write("aisdlc/config.yaml", "mavenCmd: mvn\n")
	eq(t, r.run("setup", "--approvals", "web"), 0)
	eq(t, r.read("aisdlc/config.yaml"), "mavenCmd: mvn\n")
	notContains(t, r.out, "Chế độ duyệt")
}

// Bản Go dò package main (cmd/*/main.go) và thư mục migration của repo để điền config.yaml.
func TestSetupDetectsServiceMainAndMigrations(t *testing.T) {
	r := setupRepo(t)
	r.write("cmd/api/main.go", "package main\n\nfunc main() {}\n")
	r.write("migrations/00001_init.sql", "-- +goose Up\n")
	eq(t, r.run("setup"), 0)
	c, err := model.LoadRepoConfig(r.root)
	must(t, err)
	eq(t, c.Service.Main, "./cmd/api")
	eq(t, c.Migrations.Dir, "migrations")
	notContains(t, r.out, "Chưa dò được")
}

func TestSetupUndetectedServiceMainHints(t *testing.T) {
	r := setupRepo(t)
	r.write("cmd/a/main.go", "package main\n")
	r.write("cmd/b/main.go", "package main\n")
	eq(t, r.run("setup"), 0)
	c, err := model.LoadRepoConfig(r.root)
	must(t, err)
	eq(t, c.Service.Main, "./cmd/server")
	eq(t, c.Migrations.Dir, "db/migrations")
	contains(t, r.out, "Chưa dò được package main", "Chưa dò được thư mục migration")
}

// noGitRepo là thư mục dịch vụ chưa có git (dev chạy setup ở thư mục mới tạo).
func noGitRepo(t *testing.T) *testRepo {
	t.Helper()
	hermeticGit(t)
	r := &testRepo{t: t, root: t.TempDir(), env: map[string]string{}, now: testNow}
	r.runner = newFakeRunner(r.root)
	r.verifier = &fakeVerifier{runner: r.runner}
	return r
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Thư mục chưa có git: setup tạo repo git (nhánh main, chưa commit, chưa remote) để init dùng được ngay.
func TestSetupInitsGitWhenMissing(t *testing.T) {
	r := noGitRepo(t)
	eq(t, r.run("setup"), 0)
	if st, err := os.Stat(filepath.Join(r.root, ".git")); err != nil || !st.IsDir() {
		t.Fatalf("setup phải tạo .git: %v", err)
	}
	eq(t, gitOut(t, r.root, "symbolic-ref", "--short", "HEAD"), "main")
	eq(t, gitOut(t, r.root, "remote"), "")
	contains(t, r.out, "Đã tạo git repo", "git remote add origin")
	eq(t, r.run("init", "ROOM-1"), 0)
}

// Repo đã có git: setup không đụng tới git.
func TestSetupKeepsExistingGit(t *testing.T) {
	r := setupRepo(t)
	head := gitOut(t, r.root, "rev-parse", "HEAD")
	eq(t, r.run("setup"), 0)
	eq(t, gitOut(t, r.root, "rev-parse", "HEAD"), head)
	if strings.Contains(r.out, "Đã tạo git repo") {
		t.Fatalf("không được tạo git lần nữa:\n%s", r.out)
	}
}

// Thư mục dịch vụ nằm trong một repo git có sẵn (monorepo): không tạo repo lồng.
func TestSetupNoNestedGitInsideExistingRepo(t *testing.T) {
	r := noGitRepo(t)
	gitOut(t, r.root, "init", "-q", "-b", "main")
	sub := filepath.Join(r.root, "svc")
	must(t, os.MkdirAll(sub, 0o755))
	r.root = sub
	eq(t, r.run("setup"), 0)
	if _, err := os.Stat(filepath.Join(sub, ".git")); !os.IsNotExist(err) {
		t.Fatalf("không được tạo .git lồng trong repo cha: %v", err)
	}
}
