// Package harnesscmd là lõi của binary aisdlc-harness: chạy harness (testcontainers) trong tiến trình riêng để binary
// hook aisdlc không phải trả giá khởi động của testcontainers. CLI aisdlc gọi nó như tiến trình con và đọc
// out/harness-result.json (harnessapi.ResultFile).
//
// Cách dùng:
//
//	aisdlc-harness run    --root <repo> --feature <KEY> --out <dir> [--steps S1,S2] [--mode cases]
//	aisdlc-harness verify --root <repo> --feature <KEY> --out <dir>
//
// Mã thoát: xem harnessapi (ExitPanic, ExitError, ExitInterrupted); `run` thoát bằng RunResult.ExitCode.
package harnesscmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"aisdlc/internal/buildinfo"
	"aisdlc/internal/harness"
	"aisdlc/internal/harness/harnessapi"
	"aisdlc/internal/lang/langs"
	"aisdlc/internal/model"
)

const usage = "cách dùng: aisdlc-harness run|verify --root <repo> --feature <KEY> --out <dir> [--steps S1,S2] [--mode cases]"

// Signals là nguồn tín hiệu của tiến trình; test tiêm kênh riêng. exit thoát ngay (tín hiệu thứ hai).
type Signals struct {
	Notify func() (<-chan os.Signal, func())
	Kill   func()
	Exit   func(code int)
}

// OSSignals là Ctrl-C/SIGTERM thật: lần đầu hủy ctx (harness dừng dịch vụ, dọn container), lần hai SIGKILL dịch vụ
// rồi thoát ExitInterrupted.
func OSSignals() *Signals {
	return &Signals{
		Notify: func() (<-chan os.Signal, func()) {
			ch := make(chan os.Signal, 2)
			signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
			return ch, func() { signal.Stop(ch) }
		},
		Kill: harness.KillServices,
		Exit: os.Exit,
	}
}

// Main chạy lệnh với args (không gồm tên chương trình) và trả mã thoát.
func Main(args []string, stderr io.Writer, sig *Signals) int {
	if len(args) == 0 || (args[0] != "run" && args[0] != "verify") {
		fmt.Fprintln(stderr, usage)
		return harnessapi.ExitError
	}
	cmd := args[0]
	fs := flag.NewFlagSet("aisdlc-harness "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "thư mục gốc repo đích")
	feature := fs.String("feature", "", "mã tính năng")
	out := fs.String("out", "", "thư mục kết quả (bị xóa rồi tạo lại)")
	steps := fs.String("steps", "", "các bước, phân cách bằng dấu phẩy (run; rỗng là tất cả)")
	mode := fs.String("mode", harnessapi.ModeCases, "cases, cases-with-drafts hoặc migration-check (run)")
	if err := fs.Parse(args[1:]); err != nil {
		return harnessapi.ExitError
	}
	if *root == "" || *feature == "" || *out == "" || fs.NArg() > 0 {
		fmt.Fprintln(stderr, usage)
		return harnessapi.ExitError
	}
	outDir, err := filepath.Abs(*out)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return harnessapi.ExitError
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, err)
		_ = harnessapi.WriteResult(outDir, harnessapi.Result{Error: err.Error(), Version: buildinfo.Version()})
		return harnessapi.ExitError
	}
	key, err := model.ParseFeatureKey(*feature)
	if err != nil {
		return fail(err)
	}
	m, err := harnessapi.ParseMode(*mode)
	if err != nil {
		return fail(err)
	}
	h, err := New(*root)
	if err != nil {
		return fail(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if sig != nil {
		defer trap(sig, cancel, stderr)()
	}
	var r harnessapi.Result
	if cmd == "run" {
		r = Run(ctx, h, harnessapi.Options{Feature: key, Steps: harnessapi.SplitSteps(*steps), Mode: m, Out: outDir})
	} else {
		r = Verify(ctx, h, key, outDir)
	}
	r.Version = buildinfo.Version()
	if err := harnessapi.WriteResult(outDir, r); err != nil {
		fmt.Fprintln(stderr, err)
		return harnessapi.ExitError
	}
	switch {
	case r.Panic != "":
		fmt.Fprintln(stderr, harnessapi.PanicPrefix+r.Panic)
		return harnessapi.ExitPanic
	case r.Error != "":
		fmt.Fprintln(stderr, r.Error)
		return harnessapi.ExitError
	case r.Run != nil:
		return r.Run.ExitCode
	}
	return 0
}

// trap: tín hiệu đầu hủy ctx; tín hiệu thứ hai SIGKILL ngay mọi dịch vụ rồi thoát ExitInterrupted. Hàm trả về thôi
// nhận tín hiệu.
func trap(sig *Signals, cancel context.CancelFunc, stderr io.Writer) func() {
	ch, stop := sig.Notify()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		first := true
		for {
			select {
			case <-done:
				return
			case <-ch:
				if first {
					first = false
					fmt.Fprintln(stderr, "aisdlc-harness: đang dừng dịch vụ và dọn container")
					cancel()
					continue
				}
				sig.Kill()
				sig.Exit(harnessapi.ExitInterrupted)
				return
			}
		}
	}()
	return func() {
		stop()
		close(done)
		<-finished
	}
}

// New dựng harness.Harness như CLI trước đây: config của repo, adapter và runtime theo language, hạ tầng
// testcontainers.
func New(root string) (*harness.Harness, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cfg, err := model.LoadRepoConfig(abs)
	if err != nil {
		return nil, err
	}
	a, err := langs.For(cfg.Language)
	if err != nil {
		return nil, err
	}
	rt, err := langs.RuntimeFor(cfg.Language)
	if err != nil {
		return nil, err
	}
	return &harness.Harness{Root: abs, Config: cfg, Adapter: a, Runtime: rt,
		Now: func() time.Time { return time.Now().UTC() }}, nil
}

// Run chạy h.Run; panic giữa chừng thành Result.Panic, stack ghi vào cuối harness.log.
func Run(ctx context.Context, h *harness.Harness, o harnessapi.Options) (r harnessapi.Result) {
	defer recoverInto(o.Out, &r)
	rr, err := h.Run(ctx, o)
	if err != nil {
		return harnessapi.Result{Error: err.Error()}
	}
	return harnessapi.Result{Run: &rr}
}

// Verify chạy h.Verify; panic giữa chừng thành Result.Panic, stack ghi vào cuối harness.log.
func Verify(ctx context.Context, h *harness.Harness, key model.FeatureKey, out string) (r harnessapi.Result) {
	defer recoverInto(out, &r)
	vr, err := h.Verify(ctx, key, out)
	if err != nil {
		return harnessapi.Result{Error: err.Error()}
	}
	return harnessapi.Result{Verify: &vr}
}

func recoverInto(out string, r *harnessapi.Result) {
	if p := recover(); p != nil {
		if err := os.MkdirAll(out, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return
		}
		harnessapi.AppendLog(out, harnessapi.PanicLog(p, debug.Stack()))
		*r = harnessapi.Result{Panic: fmt.Sprint(p)}
	}
}
