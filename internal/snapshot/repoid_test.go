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
