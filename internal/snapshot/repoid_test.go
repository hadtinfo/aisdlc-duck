package snapshot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"aisdlc/internal/gitx"
)

// port RepoIdentityTest.localWhenNoRemoteAndOriginWhenPresent
func TestRepoIDLocalWhenNoRemoteAndOriginWhenPresent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := filepath.Join(t.TempDir(), "my-repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	sh := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	sh("init", "-q")
	g := gitx.New(repo)
	ctx := context.Background()
	if got := RepoID(ctx, g, repo); got != "local/my-repo" {
		t.Errorf("không remote: %q", got)
	}
	sh("remote", "add", "origin", "git@GitLab.Corp:pay/payment-svc.git")
	if got := RepoID(ctx, g, repo); got != "gitlab.corp/pay/payment-svc" {
		t.Errorf("có origin: %q", got)
	}
	if _, ok, err := g.RemoteURL(ctx, "nope"); ok || err != nil {
		t.Errorf("remote lạ: ok=%v err=%v", ok, err)
	}
	if _, ok, err := g.CurrentBranch(ctx); !ok || err != nil {
		t.Errorf("branch: ok=%v err=%v", ok, err)
	}
}

// Worktree (vd. Orca tạo cho mỗi task) có tên thư mục riêng; không có remote thì mọi worktree vẫn phải cùng định
// danh với repo chính, để web dùng chung chính sách và tính năng.
func TestRepoIDWorktreeWithoutRemoteUsesMainRepoName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	repo := filepath.Join(base, "room-booking")
	wt := filepath.Join(base, "workspaces", "viết-brd-login")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	sh := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	sh("init", "-q")
	sh("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	sh("worktree", "add", "-q", "-b", "task", wt)
	ctx := context.Background()
	if got := RepoID(ctx, gitx.New(wt), wt); got != "local/room-booking" {
		t.Errorf("worktree không remote: got %q, want local/room-booking", got)
	}
	if got := RepoID(ctx, gitx.New(repo), repo); got != "local/room-booking" {
		t.Errorf("repo chính không remote: got %q", got)
	}
	sh("remote", "add", "origin", "git@gitlab.corp:pay/room-booking.git")
	if got := RepoID(ctx, gitx.New(wt), wt); got != "gitlab.corp/pay/room-booking" {
		t.Errorf("worktree có origin: got %q", got)
	}
}

func TestNormalizeRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@gitlab.corp:pay/svc.git":             "gitlab.corp/pay/svc",
		"https://GitLab.Corp/pay/svc.git":         "gitlab.corp/pay/svc",
		"ssh://git@gitlab.corp:2222/pay/svc.git/": "gitlab.corp/pay/svc",
		"https://user:pw@host/a/b/c":              "host/a/b/c",
		"  /local/path  ":                         "/local/path",
	} {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
