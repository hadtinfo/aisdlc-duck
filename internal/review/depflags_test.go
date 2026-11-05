package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aisdlc/internal/gitx"
	"aisdlc/internal/lang/golang"
)

func hermeticGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("HOME", t.TempDir())
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func goMod(reqs ...string) string {
	var sb strings.Builder
	sb.WriteString("module example.com/app\n\ngo 1.22\n\n")
	for _, r := range reqs {
		sb.WriteString("require " + r + " v1.0.0\n")
	}
	return sb.String()
}

func newRepo(t *testing.T) (string, *gitx.Repo) {
	t.Helper()
	hermeticGit(t)
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "user.name", "T")
	gitIn(t, dir, "config", "user.email", "t@e.com")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	return dir, gitx.New(dir)
}

func writeRel(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func listFiles(t *testing.T, r *gitx.Repo) []string {
	t.Helper()
	fs, err := r.ListFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func snapshot(t *testing.T, r *gitx.Repo) string {
	t.Helper()
	s, err := r.SnapshotTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func depFlags(t *testing.T, dir string, r *gitx.Repo, base string, files []string) []ReviewFlag {
	return DependencyFlags(context.Background(), golang.Adapter{}, r, base, dir, files)
}

func TestAddedDependency(t *testing.T) {
	dir, repo := newRepo(t)
	writeRel(t, dir, "go.mod", goMod("github.com/a/lib"))
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "mod")
	base := snapshot(t, repo)
	writeRel(t, dir, "go.mod", goMod("github.com/a/lib", "github.com/b/newlib"))
	fs := depFlags(t, dir, repo, base, listFiles(t, repo))
	if len(fs) != 1 {
		t.Fatalf("flags %v", fs)
	}
	eq(t, "code", fs[0].Code, "DEPENDENCY_ADDED")
	eq(t, "level", fs[0].Level, WARN)
	eq(t, "refs", fs[0].Refs, []string{"github.com/b/newlib"})
	if got := depFlags(t, dir, repo, snapshot(t, repo), listFiles(t, repo)); len(got) != 0 {
		t.Errorf("không đổi thì không có cờ: %v", got)
	}
}

func TestNoPomIsNa(t *testing.T) {
	dir, repo := newRepo(t)
	fs := depFlags(t, dir, repo, snapshot(t, repo), []string{"a.txt"})
	if len(fs) != 1 || fs[0].Level != NA {
		t.Fatalf("flags %v", fs)
	}
	eq(t, "message", fs[0].Message, "Repo không có go.mod, bỏ qua kiểm tra dependency.")
}

func TestMissingBaseTreeIsNa(t *testing.T) {
	dir, repo := newRepo(t)
	writeRel(t, dir, "go.mod", goMod("github.com/a/lib"))
	fs := depFlags(t, dir, repo, "0123456789012345678901234567890123456789", []string{"go.mod"})
	if len(fs) != 1 || fs[0].Level != NA {
		t.Fatalf("flags %v", fs)
	}
}

func TestMovingDependencyBetweenModulesIsNotAdded(t *testing.T) {
	dir, repo := newRepo(t)
	writeRel(t, dir, "a/go.mod", goMod("github.com/a/lib"))
	writeRel(t, dir, "b/go.mod", goMod())
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "mods")
	base := snapshot(t, repo)
	writeRel(t, dir, "a/go.mod", goMod())
	writeRel(t, dir, "b/go.mod", goMod("github.com/a/lib"))
	if fs := depFlags(t, dir, repo, base, listFiles(t, repo)); len(fs) != 0 {
		t.Errorf("chuyển dependency giữa module không phải thêm mới: %v", fs)
	}
}

func TestMalformedPomIsNaWithMessage(t *testing.T) {
	dir, repo := newRepo(t)
	writeRel(t, dir, "go.mod", "module (((\nrequire {{{\n")
	fs := depFlags(t, dir, repo, snapshot(t, repo), []string{"go.mod"})
	if len(fs) != 1 || fs[0].Level != NA {
		t.Fatalf("flags %v", fs)
	}
	if strings.Contains(fs[0].Message, "null") || !strings.HasPrefix(fs[0].Message, "Không đọc được go.mod") {
		t.Errorf("message %q", fs[0].Message)
	}
}
