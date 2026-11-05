package model

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"aisdlc/internal/gitx"
)

func saRepo(t *testing.T) (string, *gitx.Repo) {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	saGit(t, dir, "init", "-q", "-b", "main")
	saGit(t, dir, "config", "user.name", "Test User")
	saGit(t, dir, "config", "user.email", "test@example.com")
	return dir, gitx.New(dir)
}

func saGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func saCommit(t *testing.T, dir string) {
	t.Helper()
	saGit(t, dir, "add", "-A")
	saGit(t, dir, "commit", "-q", "-m", "c")
}

func TestSessionApproveCommittedOnBaseIsOn(t *testing.T) {
	dir, repo := saRepo(t)
	writeCfg(t, dir, "baseRef: main\nsessionApprove: true\n")
	saCommit(t, dir)
	w, err := LoadRepoConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := ReadSessionApprove(context.Background(), repo, w)
	if !s.Enabled || s.Reason != "theo main" {
		t.Fatalf("%+v", s)
	}
}

func TestSessionApproveWorkingTreeOnlyIsOff(t *testing.T) {
	dir, repo := saRepo(t)
	writeCfg(t, dir, "baseRef: main\n")
	saCommit(t, dir)
	writeCfg(t, dir, "baseRef: main\nsessionApprove: true\n")
	w, err := LoadRepoConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !w.SessionApprove {
		t.Fatal("working phải bật")
	}
	s := ReadSessionApprove(context.Background(), repo, w)
	if s.Enabled || s.Reason != "chưa bật ở main" {
		t.Fatalf("%+v", s)
	}
}

func TestSessionApproveMissingBaseRefIsOffWithReason(t *testing.T) {
	dir, repo := saRepo(t)
	writeCfg(t, dir, "baseRef: nope\nsessionApprove: true\n")
	saCommit(t, dir)
	w, _ := LoadRepoConfig(dir)
	s := ReadSessionApprove(context.Background(), repo, w)
	if s.Enabled || s.Reason != "không đọc được aisdlc/config.yaml ở nope" {
		t.Fatalf("%+v", s)
	}
}

func TestSessionApproveBrokenYamlAtBaseIsOff(t *testing.T) {
	dir, repo := saRepo(t)
	writeCfg(t, dir, "baseRef: [\n")
	saCommit(t, dir)
	w := mustParse(t, "baseRef: main\n")
	s := ReadSessionApprove(context.Background(), repo, w)
	if s.Enabled || s.Reason != "không đọc được aisdlc/config.yaml ở main" {
		t.Fatalf("%+v", s)
	}
}
