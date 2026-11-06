package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"aisdlc/internal/harness"
)

// ExitInterrupted là mã thoát khi dev hủy lệnh bằng Ctrl-C/SIGTERM lúc harness chạy (128 + SIGINT).
const ExitInterrupted = 130

const (
	interruptingMsg = "Đang dừng harness và dọn container (tối đa 30 giây)…"
	interruptedMsg  = "Đã hủy: harness đã dừng, không thay đổi trạng thái."
)

// Interrupts là nguồn tín hiệu khi harness chạy. Notify bắt đầu nhận Ctrl-C/SIGTERM (stop thôi nhận, trả lại hành vi
// mặc định); Kill diệt ngay (đồng bộ) dịch vụ harness còn sống; Exit thoát tiến trình. Test tiêm kênh tín hiệu riêng.
type Interrupts struct {
	Notify func() (<-chan os.Signal, func())
	Kill   func()
	Exit   func(code int)
}

// OSInterrupts là nguồn tín hiệu thật của tiến trình.
func OSInterrupts() *Interrupts {
	return &Interrupts{
		Notify: func() (<-chan os.Signal, func()) {
			ch := make(chan os.Signal, 2)
			signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
			return ch, func() { signal.Stop(ch) }
		},
		Kill: harness.KillServices,
		Exit: os.Exit,
	}
}

// interruptible chạy fn (gọi harness hoặc eval qua ctx.Runner/ctx.Verifier/ctx.Std) với ctx hủy được bằng tín hiệu.
// Chỉ trong lúc fn chạy mới bắt Ctrl-C/SIGTERM: lời nhắc stdin (approve/reject) và hook giữ hành vi mặc định.
// Tín hiệu đầu: in thông báo ra stderr và hủy ctx (harness dừng dịch vụ, dọn container); tín hiệu thứ hai: SIGKILL
// ngay dịch vụ rồi thoát ExitInterrupted. Sau fn, nếu ctx đã bị hủy (tín hiệu hoặc ctx của lệnh) thì trả Abort
// ExitInterrupted để lệnh dừng trước mọi thay đổi trạng thái (step finish, nộp cổng, ghi report), bỏ qua kết quả của fn.
func (c *Context) interruptible(fn func() error) error {
	parent := c.std
	ctx, cancel := context.WithCancel(parent)
	c.std = ctx
	defer func() {
		c.std = parent
		cancel()
	}()
	if in := c.interrupts; in != nil {
		ch, stop := in.Notify()
		done := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			first := true
			for {
				select {
				case <-done:
					return
				case <-ch:
					if first {
						first = false
						fmt.Fprintln(c.err, interruptingMsg)
						cancel()
						continue
					}
					in.Kill()
					in.Exit(ExitInterrupted)
					return
				}
			}
		}()
		defer func() {
			stop()
			close(done)
			wg.Wait()
		}()
	}
	err := fn()
	if ctx.Err() != nil {
		return abort(ExitInterrupted, interruptedMsg)
	}
	return err
}
