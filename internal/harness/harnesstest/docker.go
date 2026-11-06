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
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

// SkipEnv là biến môi trường để bỏ qua test cần Docker.
const SkipEnv = "AISDLC_SKIP_DOCKER"

// Skipping cho biết AISDLC_SKIP_DOCKER=1.
func Skipping() bool { return os.Getenv(SkipEnv) == "1" }

// Thời hạn của Ryuk (container dọn dẹp của testcontainers) khi chạy test: `go test ./...` chạy nhiều gói Docker song
// song và các gói dùng chung một Ryuk (cùng session = cùng tiến trình cha). Mặc định (kết nối 1 phút, kết nối lại 10
// giây) thì khi máy bận, Ryuk do gói trước mở có thể đã hết hạn và đang thoát lúc gói sau tìm thấy nó → "wait for
// reaper …: context deadline exceeded". Biến môi trường đặt sẵn (vd. từ Makefile hay CI) thì giữ nguyên.
const (
	ryukConnectionTimeout   = "5m"
	ryukReconnectionTimeout = "2m"
)

var tuneOnce sync.Once

// TuneTestcontainers đặt TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT và TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT (nếu
// chưa đặt) trước lần dùng testcontainers đầu tiên của tiến trình test (testcontainers đọc cấu hình một lần).
// RequireDocker gọi nó; chỉ dùng trong test, không đụng binary aisdlc.
func TuneTestcontainers() { tuneOnce.Do(setRyukDefaults) }

// setRyukDefaults đặt các thời hạn Ryuk chưa đặt (testcontainers nhận cả tên không tiền tố TESTCONTAINERS_).
func setRyukDefaults() {
	for k, v := range map[string]string{
		"TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT":   ryukConnectionTimeout,
		"TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT": ryukReconnectionTimeout,
	} {
		if os.Getenv(k) == "" && os.Getenv(strings.TrimPrefix(k, "TESTCONTAINERS_")) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

// RequireDocker bỏ qua test khi AISDLC_SKIP_DOCKER=1; Docker không tới được thì test fail (không tự skip).
func RequireDocker(t *testing.T) {
	t.Helper()
	if Skipping() {
		t.Skip(SkipEnv + "=1")
	}
	TuneTestcontainers()
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
	return CopyDir(t, SampleDir())
}

// FixtureDir là dịch vụ mẫu e2e (e2e/fixture-service ở gốc repo).
func FixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "e2e", "fixture-service")
}

// CopyDir chép thư mục src vào t.TempDir(), git init và commit (git hermetic), trả thư mục gốc của bản chép.
func CopyDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
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
