// Package harnesstest chứa helper dùng chung cho test cần Docker.
package harnesstest

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

// SkipEnv là biến môi trường để bỏ qua test cần Docker.
const SkipEnv = "AISDLC_SKIP_DOCKER"

// Skipping cho biết AISDLC_SKIP_DOCKER=1.
func Skipping() bool { return os.Getenv(SkipEnv) == "1" }

// RequireDocker bỏ qua test khi AISDLC_SKIP_DOCKER=1; Docker không tới được thì test fail (không tự skip).
func RequireDocker(t *testing.T) {
	t.Helper()
	if Skipping() {
		t.Skip(SkipEnv + "=1")
	}
	if err := Ping(); err != nil {
		t.Fatalf("cần Docker cho test harness: %v; đặt AISDLC_SKIP_DOCKER=1 để bỏ qua", err)
	}
}

// Ping kiểm tra Docker daemon tới được.
func Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p, err := testcontainers.NewDockerProvider()
	if err != nil {
		return err
	}
	defer p.Close()
	return p.Health(ctx)
}

// SampleDir là thư mục dịch vụ mẫu (internal/harness/testdata/sample).
func SampleDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata", "sample")
}

// CopySample chép dịch vụ mẫu vào t.TempDir(), git init và commit (git hermetic), trả thư mục gốc của bản chép.
func CopySample(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := SampleDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatalf("chép dịch vụ mẫu: %v", err)
	}
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dst
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "HOME="+dst,
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	return dst
}

// RequireNoContainers fail test nếu còn container (kể cả đã dừng) gắn nhãn aisdlc.run=<runID>.
func RequireNoContainers(t *testing.T, runID string) {
	t.Helper()
	out, err := exec.Command("docker", "ps", "-a", "-q", "--filter", "label=aisdlc.run="+runID).CombinedOutput()
	if err != nil {
		t.Fatalf("docker ps: %v\n%s", err, out)
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		t.Errorf("còn container aisdlc.run=%s: %s", runID, s)
	}
}
