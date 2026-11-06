package harnesscmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aisdlc/internal/harness"
	"aisdlc/internal/harness/harnessapi"
	"aisdlc/internal/harness/harnesstest"
	"aisdlc/internal/lang"
	"aisdlc/internal/lang/golang"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
)

// demo1Cases là các case của tính năng DEMO-1 trong dịch vụ mẫu.
var demo1Cases = []string{"C1", "C2", "C3", "C9"}

// stuckProcess là dịch vụ không bao giờ healthy và không tự thoát.
type stuckProcess struct {
	once sync.Once
	done chan struct{}
}

func (p *stuckProcess) Done() <-chan struct{} { return p.done }
func (p *stuckProcess) ExitErr() error        { return nil }
func (p *stuckProcess) Kill()                 { p.Stop(0) }
func (p *stuckProcess) Stop(time.Duration) bool {
	p.once.Do(func() { close(p.done) })
	return true
}

// fakeRuntime: Build panic nếu panicMsg khác "", Start trả dịch vụ không bao giờ healthy.
type fakeRuntime struct {
	panicMsg string
	starts   int
}

func (f *fakeRuntime) Build(context.Context, string, model.ServiceConfig, string) (lang.BuildResult, error) {
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
	return lang.BuildResult{OK: true, Binary: "/khong-co/svc"}, nil
}

func (f *fakeRuntime) UnitTests(context.Context, string, model.UnitTestConfig, string) (lang.UnitResult, error) {
	return lang.UnitResult{}, nil
}

func (f *fakeRuntime) Start(context.Context, string, map[string]string, string, string) (lang.Process, error) {
	f.starts++
	return &stuckProcess{done: make(chan struct{})}, nil
}

func (f *fakeRuntime) Coverage(context.Context, string, string, string, string) (lang.Coverage, error) {
	return lang.Coverage{NA: "fakeRuntime"}, nil
}

// fakeInfra: Postgres/Kafka trả err.
type fakeInfra struct{ err error }

func (f fakeInfra) Postgres(context.Context, string, string) (string, func(context.Context) error, error) {
	return "", nil, f.err
}

func (f fakeInfra) Kafka(context.Context, string, []string, string) (string, func(context.Context) error, error) {
	return "", nil, f.err
}

// sample dựng harness trên bản chép dịch vụ mẫu với runtime và hạ tầng giả.
func sample(t *testing.T, rt lang.Runtime, infra harness.Infra, edit func(*model.RepoConfig)) *harness.Harness {
	t.Helper()
	root := harnesstest.CopySample(t)
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(&cfg)
	}
	return &harness.Harness{Root: root, Config: cfg, Adapter: golang.Adapter{}, Runtime: rt, Infra: infra, Now: time.Now}
}

func allError(t *testing.T, cases []run.CaseResult, msg string) {
	t.Helper()
	if len(cases) != len(demo1Cases) {
		t.Fatalf("cases = %+v", cases)
	}
	for i, c := range cases {
		if c.ID != demo1Cases[i] || c.Outcome != run.CaseOutcomeError || c.Message != msg {
			t.Errorf("case %d = %+v, muốn %s ERROR %q", i, c, demo1Cases[i], msg)
		}
	}
}

func opts(h *harness.Harness) harnessapi.Options {
	return harnessapi.Options{Feature: "DEMO-1", Mode: harnessapi.ModeCases, Out: filepath.Join(h.Root, "out")}
}

// Port MavenRunnerTest.timeoutKillsMavenAndMarksCasesError: dịch vụ treo, quá timeoutMinutes thì mọi case ERROR
// với thông điệp timeout và ExitCode ExitTimeout; Verify cũng báo quá thời gian.
func TestRunTimeoutMarksCasesError(t *testing.T) {
	rt := &fakeRuntime{}
	h := sample(t, rt, fakeInfra{}, func(c *model.RepoConfig) {
		c.TimeoutMinutes = 0.005 // 300 ms
		c.Service.Env.DatabaseURL = ""
	})
	start := time.Now()
	r := Run(context.Background(), h, opts(h))
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("chạy %v", d)
	}
	if r.Run == nil || r.Run.ExitCode != harnessapi.ExitTimeout {
		t.Fatalf("r = %+v", r)
	}
	allError(t, r.Run.Cases, harnessapi.TimeoutMessage(0.005))
	if rt.starts != 1 {
		t.Errorf("starts = %d", rt.starts)
	}
	b, _ := os.ReadFile(filepath.Join(h.Root, "out", harnessapi.HarnessLog))
	if !strings.Contains(string(b), "quá thời gian") {
		t.Errorf("log = %s", b)
	}
	v := Verify(context.Background(), h, "DEMO-1", filepath.Join(h.Root, "out"))
	if v.Verify == nil || !v.Verify.TimedOut || v.Verify.Run.ExitCode != harnessapi.ExitTimeout {
		t.Errorf("verify = %+v", v)
	}
}

// Ctrl-C: ctx đã bị hủy thì harness dừng như quá thời gian nhưng với thông điệp hủy riêng.
func TestRunCanceledContextMarksCasesError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := sample(t, &fakeRuntime{}, fakeInfra{}, func(c *model.RepoConfig) { c.Service.Env.DatabaseURL = "" })
	r := Run(ctx, h, opts(h))
	if r.Run == nil || r.Run.ExitCode != harnessapi.ExitTimeout {
		t.Fatalf("r = %+v", r)
	}
	allError(t, r.Run.Cases, harnessapi.CanceledMessage)
}

// Harness panic: Result.Panic, stack và "harness lỗi: boom" ở cuối harness.log, không có results.json.
func TestPanicBecomesResultWithStack(t *testing.T) {
	h := sample(t, &fakeRuntime{panicMsg: "boom"}, fakeInfra{}, nil)
	for _, r := range []harnessapi.Result{Run(context.Background(), h, opts(h)),
		Verify(context.Background(), h, "DEMO-1", filepath.Join(h.Root, "out"))} {
		if r.Panic != "boom" || r.Run != nil || r.Verify != nil {
			t.Errorf("r = %+v", r)
		}
		b, _ := os.ReadFile(filepath.Join(h.Root, "out", harnessapi.HarnessLog))
		for _, w := range []string{"harness lỗi: boom", "goroutine", "runtime/debug.Stack"} {
			if !strings.Contains(string(b), w) {
				t.Errorf("log thiếu %q", w)
			}
		}
		if _, err := os.Stat(filepath.Join(h.Root, "out", "results.json")); err == nil {
			t.Error("có results.json sau panic")
		}
	}
}

// Review Focus 5: Docker không chạy thì case ERROR với thông điệp tiếng Việt, không phải Error.
func TestInfraFailureIsResultNotError(t *testing.T) {
	h := sample(t, &fakeRuntime{}, fakeInfra{errors.New("Cannot connect to the Docker daemon")}, nil)
	r := Run(context.Background(), h, opts(h))
	if r.Run == nil || r.Run.ExitCode != 2 || r.Error != "" {
		t.Fatalf("r = %+v", r)
	}
	allError(t, r.Run.Cases, "không khởi động được Postgres: Cannot connect to the Docker daemon")
}

// Tham số sai: mã ExitError; khi biết out thì ghi Result.Error để CLI báo đúng lý do.
func TestMainBadArgs(t *testing.T) {
	var e strings.Builder
	if c := Main(nil, &e, nil); c != harnessapi.ExitError || !strings.Contains(e.String(), "cách dùng") {
		t.Errorf("code %d, %q", c, e.String())
	}
	if c := Main([]string{"run", "--root", "x"}, &e, nil); c != harnessapi.ExitError {
		t.Errorf("code %d", c)
	}
	root := harnesstest.CopySample(t)
	out := filepath.Join(t.TempDir(), "out")
	if c := Main([]string{"run", "--root", root, "--feature", "DEMO-1", "--out", out, "--mode", "lạ"}, &e, nil); c != harnessapi.ExitError {
		t.Errorf("code %d", c)
	}
	r, err := harnessapi.ReadResult(out)
	if err != nil || !strings.Contains(r.Error, "mode không hợp lệ") {
		t.Errorf("r = %+v, %v", r, err)
	}
	if err := os.WriteFile(filepath.Join(root, "aisdlc", "config.yaml"), []byte("language: cobol\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := Main([]string{"verify", "--root", root, "--feature", "DEMO-1", "--out", out}, &e, nil); c != harnessapi.ExitError {
		t.Errorf("code %d", c)
	}
	if r, err := harnessapi.ReadResult(out); err != nil || r.Error == "" {
		t.Errorf("r = %+v, %v", r, err)
	}
}

// Tín hiệu đầu hủy ctx; tín hiệu thứ hai diệt dịch vụ rồi thoát ExitInterrupted.
func TestTrapFirstCancelsSecondKills(t *testing.T) {
	ch := make(chan os.Signal, 2)
	killed := make(chan struct{})
	exited := make(chan int, 1)
	sig := &Signals{
		Notify: func() (<-chan os.Signal, func()) { return ch, func() {} },
		Kill:   func() { close(killed) },
		Exit:   func(c int) { exited <- c },
	}
	ctx, cancel := context.WithCancel(context.Background())
	var e strings.Builder
	stop := trap(sig, cancel, &e)
	ch <- os.Interrupt
	<-ctx.Done()
	ch <- os.Interrupt
	<-killed
	if c := <-exited; c != harnessapi.ExitInterrupted {
		t.Errorf("exit %d", c)
	}
	stop()
}
