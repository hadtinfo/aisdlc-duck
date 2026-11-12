package golang

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/mod/modfile"

	"aisdlc/internal/i18n"
	"aisdlc/internal/lang"
	"aisdlc/internal/model"
)

// Runtime cài đặt lang.Runtime cho Go.
type Runtime struct{}

var _ lang.Runtime = Runtime{}

// modulePath đọc đường dẫn module từ go.mod ở repoRoot.
func modulePath(repoRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return "", fmt.Errorf(i18n.T("không có go.mod ở %s"), repoRoot)
	}
	m := modfile.ModulePath(data)
	if m == "" {
		return "", fmt.Errorf(i18n.T("go.mod ở %s không có dòng module"), repoRoot)
	}
	return m, nil
}

// Build biên dịch dịch vụ với -cover để đo độ phủ khi chạy case.
func (Runtime) Build(ctx context.Context, repoRoot string, svc model.ServiceConfig, outDir string) (lang.BuildResult, error) {
	mod, err := modulePath(repoRoot)
	if err != nil {
		return lang.BuildResult{Output: err.Error()}, nil
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return lang.BuildResult{}, fmt.Errorf(i18n.T("không tìm thấy go trên PATH: %w"), err)
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return lang.BuildResult{}, err
	}
	if err := os.MkdirAll(absOut, 0o755); err != nil {
		return lang.BuildResult{}, err
	}
	bin := filepath.Join(absOut, "svc")
	args := []string{"build", "-cover", "-covermode=atomic", "-coverpkg=" + mod + "/..."}
	args = append(args, svc.BuildFlags...) // cờ phải đứng trước package
	args = append(args, "-o", bin, svc.Main)
	cmd := exec.CommandContext(ctx, goBin, args...)
	cmd.Dir = repoRoot
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return lang.BuildResult{}, ctx.Err()
		}
		if _, ok := err.(*exec.ExitError); ok {
			return lang.BuildResult{Output: out.String()}, nil
		}
		return lang.BuildResult{}, err
	}
	return lang.BuildResult{OK: true, Binary: bin, Output: out.String()}, nil
}
