// Package harnesstest chứa helper dùng chung cho test cần Docker.
package harnesstest

import (
	"context"
	"os"
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
