package golang

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"aisdlc/internal/lang"
)

type process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
	once sync.Once
}

func (p *process) Done() <-chan struct{} { return p.done }
func (p *process) ExitErr() error        { return p.err }

func (p *process) killGroup(sig syscall.Signal) {
	_ = syscall.Kill(-p.cmd.Process.Pid, sig)
}

// Stop gửi SIGTERM cho cả nhóm; quá timeout thì SIGKILL cả nhóm.
// Trả true nếu tiến trình tự thoát trong timeout.
func (p *process) Stop(timeout time.Duration) bool {
	select {
	case <-p.done:
		return true
	default:
	}
	p.killGroup(syscall.SIGTERM)
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-p.done:
		return true
	case <-t.C:
	}
	p.killGroup(syscall.SIGKILL)
	<-p.done
	return false
}

// Start chạy binary trong process group riêng, ghi stdout+stderr vào logPath.
func (Runtime) Start(ctx context.Context, binary string, env map[string]string, coverDir, logPath string) (lang.Process, error) {
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	if coverDir != "" {
		if err := os.MkdirAll(coverDir, 0o755); err != nil {
			logf.Close()
			return nil, err
		}
	}
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Env = append(cmd.Env, "GOCOVERDIR="+coverDir)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	p := &process{cmd: cmd, done: make(chan struct{})}
	cmd.Cancel = func() error {
		p.killGroup(syscall.SIGKILL)
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		logf.Close()
		return nil, fmt.Errorf("không khởi động được dịch vụ: %w", err)
	}
	go func() {
		p.err = cmd.Wait()
		logf.Close()
		close(p.done)
	}()
	return p, nil
}
