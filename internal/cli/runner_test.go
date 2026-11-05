package cli

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
func (p *stuckProcess) Stop(time.Duration) bool {
	p.once.Do(func() { close(p.done) })
	return true
}

// runnerRuntime: Build theo build (panic nếu panicMsg khác ""), Start trả dịch vụ không bao giờ healthy.
type runnerRuntime struct {
	panicMsg string
	starts   int
}

func (f *runnerRuntime) Build(context.Context, string, model.ServiceConfig, string) (lang.BuildResult, error) {
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
	return lang.BuildResult{OK: true, Binary: "/khong-co/svc"}, nil
}

func (f *runnerRuntime) UnitTests(context.Context, string, model.UnitTestConfig, string) (lang.UnitResult, error) {
	return lang.UnitResult{}, nil
}

func (f *runnerRuntime) Start(context.Context, string, map[string]string, string, string) (lang.Process, error) {
	f.starts++
	return &stuckProcess{done: make(chan struct{})}, nil
}

func (f *runnerRuntime) Coverage(context.Context, string, string, string, string) (lang.Coverage, error) {
	return lang.Coverage{NA: "runnerRuntime"}, nil
}

// runnerInfra: Postgres/Kafka trả err (nil thì chưa bao giờ được gọi tới trong test dùng nó).
type runnerInfra struct{ err error }

func (f runnerInfra) Postgres(context.Context, string, string) (string, func(context.Context) error, error) {
	return "", nil, f.err
}

func (f runnerInfra) Kafka(context.Context, string, []string, string) (string, func(context.Context) error, error) {
	return "", nil, f.err
}

// sampleRunner dựng harnessCaseRunner trên bản chép dịch vụ mẫu với runtime và hạ tầng giả.
func sampleRunner(t *testing.T, std context.Context, rt lang.Runtime, infra harness.Infra,
	edit func(*model.RepoConfig)) *harnessCaseRunner {
	t.Helper()
	root := harnesstest.CopySample(t)
	cfg, err := model.LoadRepoConfig(root)
	must(t, err)
	if edit != nil {
		edit(&cfg)
	}
	h := &harness.Harness{Root: root, Config: cfg, Adapter: golang.Adapter{}, Runtime: rt, Infra: infra, Now: time.Now}
	return &harnessCaseRunner{std: std, root: root, h: h}
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

// Port MavenRunnerTest.timeoutKillsMavenAndMarksCasesError: dịch vụ treo, quá timeoutMinutes thì mọi case ERROR
// với thông điệp timeout và ExitCode ExitTimeout; Verify cũng báo quá thời gian.
func TestRunnerTimeoutMarksCasesError(t *testing.T) {
	rt := &runnerRuntime{}
	r := sampleRunner(t, context.Background(), rt, runnerInfra{}, func(c *model.RepoConfig) {
		c.TimeoutMinutes = 0.005 // 300 ms
		c.Service.Env.DatabaseURL = ""
	})
	start := time.Now()
	res, err := r.RunCases("DEMO-1", nil, "cases")
	must(t, err)
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("chạy %v", d)
	}
	eq(t, res.ExitCode, harness.ExitTimeout)
	allError(t, res.Cases, harness.TimeoutMessage(0.005))
	eq(t, rt.starts, 1)
	contains(t, r.OutputTail("DEMO-1", 50), "quá thời gian")
	eq(t, r.OutDir("DEMO-1"), filepath.Join(r.root, ".aisdlc", "run", "DEMO-1"))

	vr, err := harnessVerifier{h: r.h}.Verify(context.Background(), "DEMO-1", filepath.Join(r.root, "out"))
	must(t, err)
	if !vr.TimedOut || vr.Run.ExitCode != harness.ExitTimeout {
		t.Errorf("verify = %+v", vr)
	}
}

// Ctrl-C: ctx của lệnh đã bị hủy thì harness dừng như quá thời gian.
func TestRunnerCancelledContextMarksCasesError(t *testing.T) {
	std, cancel := context.WithCancel(context.Background())
	cancel()
	r := sampleRunner(t, std, &runnerRuntime{}, runnerInfra{}, func(c *model.RepoConfig) {
		c.Service.Env.DatabaseURL = ""
	})
	res, err := r.RunCases("DEMO-1", nil, "cases")
	must(t, err)
	eq(t, res.ExitCode, harness.ExitTimeout)
	allError(t, res.Cases, harness.TimeoutMessage(r.h.Config.TimeoutMinutes))
}

// Port MavenRunnerTest.verifyClearsStaleResultsAndReadsThemLikeRunCases: results.json/schema.json cũ trong OutDir,
// harness hỏng trước khi ghi: kết quả là NoResults, không bao giờ dùng file cũ.
func TestRunnerClearsStaleResults(t *testing.T) {
	r := sampleRunner(t, context.Background(), &runnerRuntime{panicMsg: "boom"}, runnerInfra{}, nil)
	out := r.OutDir("DEMO-1")
	stale := func() {
		must(t, os.MkdirAll(out, 0o755))
		must(t, harness.WriteResults(out, "DEMO-1", harness.ModeCases, []run.CaseResult{
			{ID: "C1", Step: "S1", Outcome: run.CaseOutcomePassed}}))
		must(t, os.WriteFile(filepath.Join(out, "schema.json"), []byte("{}"), 0o644))
	}
	stale()

	res, err := r.RunCases("DEMO-1", nil, "cases")
	must(t, err)
	eq(t, res.ExitCode, 2)
	allError(t, res.Cases, harness.NoResults)
	if _, err := os.Stat(filepath.Join(out, "schema.json")); err == nil {
		t.Error("schema.json cũ vẫn còn")
	}
	contains(t, r.OutputTail("DEMO-1", 20), "boom")

	stale()
	vr, err := harnessVerifier{h: r.h}.Verify(context.Background(), "DEMO-1", out)
	must(t, err)
	contains(t, vr.HarnessErr, "boom")
	if vr.Build.OK {
		t.Error("build không được coi là đạt")
	}
	if _, err := os.Stat(filepath.Join(out, "results.json")); err == nil {
		t.Error("results.json cũ vẫn còn sau Verify")
	}
	if _, err := os.Stat(filepath.Join(out, "schema.json")); err == nil {
		t.Error("schema.json cũ vẫn còn sau Verify")
	}
}

// Review Focus 5: Docker không chạy thì case ERROR với thông điệp tiếng Việt, RunCases không trả lỗi.
func TestRunnerInfraFailureIsResultNotError(t *testing.T) {
	r := sampleRunner(t, context.Background(), &runnerRuntime{},
		runnerInfra{errors.New("Cannot connect to the Docker daemon")}, nil)
	res, err := r.RunCases("DEMO-1", nil, "cases")
	must(t, err)
	eq(t, res.ExitCode, 2)
	allError(t, res.Cases, "không khởi động được Postgres: Cannot connect to the Docker daemon")
}

func TestRunnerUnknownModeAborts(t *testing.T) {
	r := sampleRunner(t, context.Background(), &runnerRuntime{}, runnerInfra{}, nil)
	_, err := r.RunCases("DEMO-1", nil, "lạ")
	var a *Abort
	if !asAbort(err, &a) || a.Code != 2 {
		t.Fatalf("err = %v", err)
	}
	_, perr := harness.ParseMode("lạ")
	eq(t, a.Msg, perr.Error())
	if !strings.Contains(a.Msg, "mode không hợp lệ") {
		t.Errorf("msg = %q", a.Msg)
	}
}

// Bản thật mặc định của Context là harness Go theo language của config.
func TestDefaultRunnerAndVerifierUseHarness(t *testing.T) {
	ctx := NewContext(strings.NewReader(""), &strings.Builder{}, &strings.Builder{}, noConsole(), t.TempDir())
	cfg := model.RepoConfig{Language: "go"}
	if _, ok := ctx.Runner("/repo", cfg).(*harnessCaseRunner); !ok {
		t.Errorf("runner = %T", ctx.Runner("/repo", cfg))
	}
	if _, ok := ctx.Verifier("/repo", cfg).(harnessVerifier); !ok {
		t.Errorf("verifier = %T", ctx.Verifier("/repo", cfg))
	}
}
