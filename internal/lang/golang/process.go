package golang

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"aisdlc/internal/i18n"
	"aisdlc/internal/lang"
)

type process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func (p *process) Done() <-chan struct{} { return p.done }
func (p *process) ExitErr() error        { return p.err }

func (p *process) killGroup(sig syscall.Signal) {
	_ = syscall.Kill(-p.cmd.Process.Pid, sig)
}

// Kill SIGKILL cả nhóm ngay, không chờ tiến trình thoát.
func (p *process) Kill() { p.killGroup(syscall.SIGKILL) }

// Stop gửi SIGTERM cho cả nhóm; quá timeout thì SIGKILL cả nhóm.
// Trả true nếu tiến trình chính tự thoát trong timeout. Khi tiến trình chính
// đã thoát (êm hay không), nhóm luôn bị SIGKILL để không sót tiến trình con.
func (p *process) Stop(timeout time.Duration) bool {
	graceful := true
	select {
	case <-p.done:
	default:
		p.killGroup(syscall.SIGTERM)
		t := time.NewTimer(timeout)
		defer t.Stop()
		select {
		case <-p.done:
		case <-t.C:
			graceful = false
			p.killGroup(syscall.SIGKILL)
			<-p.done
		}
	}
	p.killGroup(syscall.SIGKILL) // nhóm rỗng thì ESRCH, bỏ qua
	return graceful
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
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- binary là dịch vụ do harness vừa build từ repo đang eval, không phải đầu vào ngoài
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if coverDir != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+coverDir)
	}
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
		return nil, fmt.Errorf(i18n.T("không khởi động được dịch vụ: %w"), err)
	}
	go func() {
		p.err = cmd.Wait()
		logf.Close()
		close(p.done)
	}()
	return p, nil
}
