package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"aisdlc/internal/harness/harnesstest"
	"aisdlc/internal/lang"
	"aisdlc/internal/lang/golang"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
)

// recInfra bọc ContainerInfra, ghi lại runID và số lần stop để test kiểm container đã được dọn.
type recInfra struct {
	mu    sync.Mutex
	ids   []string
	stops atomic.Int32
}

func (r *recInfra) record(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !slices.Contains(r.ids, id) {
		r.ids = append(r.ids, id)
	}
}

func (r *recInfra) counted(stop func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		r.stops.Add(1)
		return stop(ctx)
	}
}

func (r *recInfra) Postgres(ctx context.Context, image, runID string) (string, func(context.Context) error, error) {
	r.record(runID)
	url, stop, err := ContainerInfra{}.Postgres(ctx, image, runID)
	if err != nil {
		return "", nil, err
	}
	return url, r.counted(stop), nil
}

func (r *recInfra) Kafka(ctx context.Context, image string, topics []string, runID string) (string, func(context.Context) error, error) {
	r.record(runID)
	b, stop, err := ContainerInfra{}.Kafka(ctx, image, topics, runID)
	if err != nil {
		return "", nil, err
	}
	return b, r.counted(stop), nil
}

// requireCleaned kiểm mọi container của các lần chạy đã bị xóa.
func (r *recInfra) requireCleaned(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	ids := append([]string{}, r.ids...)
	r.mu.Unlock()
	if len(ids) == 0 {
		t.Fatal("harness không khởi động container nào")
	}
	for _, id := range ids {
		harnesstest.RequireNoContainers(t, id)
	}
}

// failInfra luôn lỗi, như khi Docker không chạy.
type failInfra struct{ err error }

func (f failInfra) Postgres(context.Context, string, string) (string, func(context.Context) error, error) {
	return "", nil, f.err
}

func (f failInfra) Kafka(context.Context, string, []string, string) (string, func(context.Context) error, error) {
	return "", nil, f.err
}

// fakeRuntime đếm lời gọi; Build trả build, các hàm khác lỗi.
type fakeRuntime struct {
	builds, units, starts atomic.Int32
	build                 lang.BuildResult
}

func (f *fakeRuntime) Build(context.Context, string, model.ServiceConfig, string) (lang.BuildResult, error) {
	f.builds.Add(1)
	return f.build, nil
}

func (f *fakeRuntime) UnitTests(context.Context, string, model.UnitTestConfig, string) (lang.UnitResult, error) {
	f.units.Add(1)
	return lang.UnitResult{}, errors.New("fakeRuntime: không chạy unit test")
}

func (f *fakeRuntime) Start(context.Context, string, map[string]string, string, string) (lang.Process, error) {
	f.starts.Add(1)
	return nil, errors.New("fakeRuntime: không khởi động dịch vụ")
}

func (f *fakeRuntime) Coverage(context.Context, string, string, string, string) (lang.Coverage, error) {
	return lang.Coverage{NA: "fakeRuntime"}, nil
}

// newSampleHarness chép dịch vụ mẫu và dựng Harness dùng runtime Go thật và container thật (có ghi lại).
func newSampleHarness(t *testing.T, edit func(*model.RepoConfig)) (*Harness, *recInfra) {
	t.Helper()
	root := harnesstest.CopySample(t)
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(&cfg)
	}
	infra := &recInfra{}
	return &Harness{Root: root, Config: cfg, Adapter: golang.Adapter{}, Runtime: golang.Runtime{}, Infra: infra, Now: time.Now}, infra
}

func editFile(t *testing.T, path, old, new string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(old)) {
		t.Fatalf("%s không có %q", path, old)
	}
	if err := os.WriteFile(path, bytes.Replace(b, []byte(old), []byte(new), 1), 0o644); err != nil {
		t.Fatal(err)
	}
}

func outDir(t *testing.T) string { return filepath.Join(t.TempDir(), "eval") }

func rows(cs []run.CaseResult) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.ID+"/"+c.Step+"/"+string(c.Outcome))
	}
	return out
}

type resultsFile struct {
	Feature string           `json:"feature"`
	Mode    string           `json:"mode"`
	Cases   []run.CaseResult `json:"cases"`
}

func readResultsFile(t *testing.T, out string) resultsFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, "results.json"))
	if err != nil {
		t.Fatalf("không có results.json: %v", err)
	}
	var r resultsFile
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func readLog(t *testing.T, out string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, "harness.log"))
	if err != nil {
		t.Fatalf("không có harness.log: %v", err)
	}
	return string(b)
}

func readSchema(t *testing.T, out string) run.SchemaSnapshot {
	t.Helper()
	s, err := run.ReadSchemaSnapshot(filepath.Join(out, "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func allErrorWith(t *testing.T, cs []run.CaseResult, ids []string, msg string) {
	t.Helper()
	got := []string{}
	for _, c := range cs {
		got = append(got, c.ID)
		if c.Outcome != run.CaseOutcomeError || c.Message != msg {
			t.Errorf("%s: %s %q, mong đợi ERROR %q", c.ID, c.Outcome, c.Message, msg)
		}
	}
	if !slices.Equal(got, ids) {
		t.Errorf("case = %v, mong đợi %v", got, ids)
	}
}

var demo1 = []string{"C1", "C2", "C3", "C9"}

// Port SampleCasesTest/SuiteRun và ResultsFileTest.casesModeWritesResultsAndFailsOnlyC9.
func TestRunDemo1Cases(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	h, infra := newSampleHarness(t, nil)
	out := outDir(t)
	res, err := h.Run(context.Background(), Options{Feature: "DEMO-1", Mode: ModeCases, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"C1/S1/PASSED", "C2/S1/PASSED", "C3/S1/PASSED", "C9/S1/FAILED"}
	if !slices.Equal(rows(res.Cases), want) {
		t.Fatalf("cases = %v\n%+v\nlog:\n%s", rows(res.Cases), res.Cases, readLog(t, out))
	}
	if res.Cases[0].Message != "" {
		t.Errorf("C1 message = %q", res.Cases[0].Message)
	}
	if res.Cases[3].Message != "status: mong đợi 201, nhận 200" {
		t.Errorf("C9 message = %q", res.Cases[3].Message)
	}
	if res.ExitCode != 1 {
		t.Errorf("exit = %d", res.ExitCode)
	}
	f := readResultsFile(t, out)
	if f.Feature != "DEMO-1" || f.Mode != ModeCases || !slices.Equal(rows(f.Cases), want) {
		t.Errorf("results.json = %+v", f)
	}
	cols := readSchema(t, out).Tables["transfer"]
	if !slices.Contains(cols, "amount:BIGINT:NOT NULL") {
		t.Errorf("schema transfer = %v", cols)
	}
	for _, c := range cols {
		if strings.HasPrefix(c, "note:") {
			t.Errorf("mode cases không được chạy nháp: %v", cols)
		}
	}
	infra.requireCleaned(t)
}

func TestRunStepsFilter(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	h, infra := newSampleHarness(t, nil)
	editFile(t, filepath.Join(h.Root, "aisdlc/features/DEMO-1/cases.yaml"), "id: C2\n    step: S1", "id: C2\n    step: S2")
	out := outDir(t)
	res, err := h.Run(context.Background(), Options{Feature: "DEMO-1", Steps: []string{"S2"}, Mode: ModeCases, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if got := rows(res.Cases); !slices.Equal(got, []string{"C2/S2/PASSED"}) {
		t.Fatalf("cases = %v %+v\nlog:\n%s", got, res.Cases, readLog(t, out))
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d", res.ExitCode)
	}
	infra.requireCleaned(t)
}

// Port 3 test của DraftsModeTest (DEMO-3: D1 dùng cột note của migration nháp).
func TestRunCasesWithDrafts(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	t.Run("nháp được chạy", func(t *testing.T) {
		t.Parallel()
		h, infra := newSampleHarness(t, nil)
		out := outDir(t)
		res, err := h.Run(context.Background(), Options{Feature: "DEMO-3", Mode: ModeCasesWithDrafts, Out: out})
		if err != nil {
			t.Fatal(err)
		}
		if got := rows(res.Cases); !slices.Equal(got, []string{"D1/S1/PASSED"}) {
			t.Fatalf("cases = %+v\nlog:\n%s", res.Cases, readLog(t, out))
		}
		if f := readResultsFile(t, out); f.Mode != ModeCasesWithDrafts {
			t.Errorf("mode = %q", f.Mode)
		}
		if cols := readSchema(t, out).Tables["transfer"]; !slices.Contains(cols, "note:CHARACTER VARYING:NULL") {
			t.Errorf("schema transfer = %v", cols)
		}
		infra.requireCleaned(t)
	})
	t.Run("nháp trùng version với migration chính bị bỏ", func(t *testing.T) {
		t.Parallel()
		h, infra := newSampleHarness(t, nil)
		init, err := os.ReadFile(filepath.Join(h.Root, "db/migrations/00001_init.sql"))
		if err != nil {
			t.Fatal(err)
		}
		// tên khác, cùng version 1: không bỏ thì CREATE TABLE transfer chạy hai lần
		if err := os.WriteFile(filepath.Join(h.Root, "aisdlc/features/DEMO-3/db/00001_init_draft.sql"), init, 0o644); err != nil {
			t.Fatal(err)
		}
		out := outDir(t)
		res, err := h.Run(context.Background(), Options{Feature: "DEMO-3", Mode: ModeCasesWithDrafts, Out: out})
		if err != nil {
			t.Fatal(err)
		}
		if got := rows(res.Cases); !slices.Equal(got, []string{"D1/S1/PASSED"}) {
			t.Fatalf("cases = %+v\nlog:\n%s", res.Cases, readLog(t, out))
		}
		infra.requireCleaned(t)
	})
	t.Run("mode cases không chạy nháp", func(t *testing.T) {
		t.Parallel()
		h, infra := newSampleHarness(t, nil)
		out := outDir(t)
		res, err := h.Run(context.Background(), Options{Feature: "DEMO-3", Mode: ModeCases, Out: out})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Cases) != 1 || res.Cases[0].Outcome != run.CaseOutcomeError || !strings.Contains(res.Cases[0].Message, "given.sql") {
			t.Fatalf("cases = %+v", res.Cases)
		}
		infra.requireCleaned(t)
	})
}

// Port SchemaDumperTest (migrationCheckRunsOnlySchemaDump, migrationCheckIncludesDrafts): chỉ goose + dump, không build.
func TestRunMigrationCheck(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	h, infra := newSampleHarness(t, nil)
	rt := &fakeRuntime{}
	h.Runtime = rt
	out := outDir(t)
	res, err := h.Run(context.Background(), Options{Feature: "DEMO-1", Mode: ModeMigrationCheck, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cases) != 0 || res.ExitCode != 0 {
		t.Fatalf("res = %+v\nlog:\n%s", res, readLog(t, out))
	}
	if n := rt.builds.Load() + rt.starts.Load(); n != 0 {
		t.Errorf("migration-check không được build/khởi động dịch vụ (%d lời gọi)", n)
	}
	f := readResultsFile(t, out)
	if f.Mode != ModeMigrationCheck || f.Cases == nil || len(f.Cases) != 0 {
		t.Errorf("results.json = %+v", f)
	}
	s := readSchema(t, out)
	if _, ok := s.Tables["goose_db_version"]; ok {
		t.Error("schema có goose_db_version")
	}
	if cols := s.Tables["transfer"]; !slices.Contains(cols, "note:CHARACTER VARYING:NULL") || !slices.Contains(cols, "id:BIGINT:NOT NULL") {
		t.Errorf("schema transfer = %v", cols)
	}
	infra.requireCleaned(t)
}

// Port KafkaCasesTest.messageExpectationsAreVerifiedAgainstKafka.
func TestRunKafkaCase(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	h, infra := newSampleHarness(t, func(c *model.RepoConfig) {
		c.Service.Env.KafkaBrokers = "KAFKA_BROKERS"
		c.Service.Kafka.Topics = []string{"transfer.created"}
	})
	out := outDir(t)
	res, err := h.Run(context.Background(), Options{Feature: "DEMO-2", Mode: ModeCases, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"K1/S1/PASSED", "K2/S1/FAILED", "K3/S1/PASSED"}
	if got := rows(res.Cases); !slices.Equal(got, want) {
		t.Fatalf("cases = %+v\nlog:\n%s", res.Cases, readLog(t, out))
	}
	if !strings.Contains(res.Cases[1].Message, "transfer.created") {
		t.Errorf("K2 message = %q", res.Cases[1].Message)
	}
	infra.requireCleaned(t)
}

// Port ResultsFileTest.appStartupFailureStillReportsEverySelectedCase (Review Focus 1).
func TestRunServiceNeverHealthy(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	h, infra := newSampleHarness(t, func(c *model.RepoConfig) {
		c.Service.ExtraEnv = map[string]string{"SAMPLE_EXIT_AT_START": "1"}
	})
	out := outDir(t)
	res, err := h.Run(context.Background(), Options{Feature: "DEMO-1", Steps: []string{"S1"}, Mode: ModeCases, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	allErrorWith(t, res.Cases, demo1, NotRunText())
	allErrorWith(t, readResultsFile(t, out).Cases, demo1, NotRunText())
	if res.ExitCode != 2 {
		t.Errorf("exit = %d", res.ExitCode)
	}
	if log := readLog(t, out); !strings.Contains(log, "boom") {
		t.Errorf("harness.log thiếu log dịch vụ:\n%s", log)
	}
	infra.requireCleaned(t)
}

// preInfra trả Postgres đã khởi động sẵn (để Run tới được bước chạy case trong hạn ngắn), stop thật.
type preInfra struct {
	url   string
	stop  func(context.Context) error
	stops atomic.Int32
}

func (p *preInfra) Postgres(context.Context, string, string) (string, func(context.Context) error, error) {
	return p.url, func(ctx context.Context) error { p.stops.Add(1); return p.stop(ctx) }, nil
}

func (p *preInfra) Kafka(context.Context, string, []string, string) (string, func(context.Context) error, error) {
	return "", nil, errors.New("không dùng Kafka")
}

// prebuiltRuntime trả bản build làm sẵn (build rỗng: build thật) và ghi lại tiến trình đã khởi động.
type prebuiltRuntime struct {
	golang.Runtime
	build   lang.BuildResult
	onStart func()
	mu      sync.Mutex
	procs   []lang.Process
}

func (p *prebuiltRuntime) Build(ctx context.Context, root string, svc model.ServiceConfig, out string) (lang.BuildResult, error) {
	if p.build.Binary == "" {
		return p.Runtime.Build(ctx, root, svc, out)
	}
	return p.build, nil
}

func (p *prebuiltRuntime) Start(ctx context.Context, binary string, env map[string]string, coverDir, logPath string) (lang.Process, error) {
	pr, err := p.Runtime.Start(ctx, binary, env, coverDir, logPath)
	if p.onStart != nil {
		p.onStart()
	}
	if err == nil {
		p.mu.Lock()
		p.procs = append(p.procs, pr)
		p.mu.Unlock()
	}
	return pr, err
}

// processesRunning liệt kê pid có argv[0] là binary (đọc /proc).
func processesRunning(t *testing.T, binary string) []string {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Skip("không có /proc")
	}
	var pids []string
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if arg0, _, _ := bytes.Cut(b, []byte{0}); string(arg0) == binary {
			pids = append(pids, e.Name())
		}
	}
	return pids
}

// runStuckCase chạy DEMO-1 với stub chậm 60s; build và Postgres làm sẵn ngoài Run để hạn (hoặc lệnh hủy) rơi vào
// lúc case đang chờ stub. cancelAfter > 0 thì hủy ctx sau khoảng đó kể từ khi dịch vụ khởi động (như Ctrl-C).
// Kiểm mọi case ERROR TimeoutMessage (hủy: Canceled()), ExitTimeout, không còn tiến trình hay container.
func runStuckCase(t *testing.T, minutes float64, cancelAfter time.Duration) {
	h, _ := newSampleHarness(t, func(c *model.RepoConfig) {
		c.TimeoutMinutes = minutes
		c.Service.ShutdownTimeoutSeconds = 2
	})
	editFile(t, filepath.Join(h.Root, "aisdlc/features/DEMO-1/cases.yaml"),
		"response: { status: 200, jsonBody: { balance: 1000 } }",
		"response: { status: 200, fixedDelayMilliseconds: 60000, jsonBody: { balance: 1000 } }")

	b, err := golang.Runtime{}.Build(context.Background(), h.Root, h.Config.Service, t.TempDir())
	if err != nil || !b.OK {
		t.Fatalf("build: %v %s", err, b.Output)
	}
	runID := "harness-stuck-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	pg, stop, err := StartPostgres(context.Background(), h.Config.Service.Postgres.Image, runID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stop(context.Background()) }) // Run lỗi sớm vẫn không để sót container
	infra := &preInfra{url: pg.URL, stop: stop}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := &prebuiltRuntime{build: b}
	if cancelAfter > 0 {
		rt.onStart = func() { time.AfterFunc(cancelAfter, cancel) }
	}
	h.Infra, h.Runtime = infra, rt

	out := outDir(t)
	start := time.Now()
	res, err := h.Run(ctx, Options{Feature: "DEMO-1", Mode: ModeCases, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("Run mất %s: hạn/hủy không dừng được case đang chờ stub 60s", elapsed)
	}
	if len(rt.procs) != 1 {
		t.Fatalf("dịch vụ phải được khởi động một lần (%d)\nlog:\n%s", len(rt.procs), readLog(t, out))
	}
	msg := TimeoutMessage(minutes)
	if cancelAfter > 0 {
		msg = Canceled()
	}
	allErrorWith(t, res.Cases, demo1, msg)
	allErrorWith(t, readResultsFile(t, out).Cases, demo1, msg)
	if _, err := os.Stat(filepath.Join(out, "schema.json")); err == nil {
		t.Error("dừng giữa chừng không được ghi schema.json")
	}
	if res.ExitCode != ExitTimeout {
		t.Errorf("exit = %d", res.ExitCode)
	}
	select {
	case <-rt.procs[0].Done():
	default:
		t.Error("tiến trình dịch vụ còn chạy")
	}
	if pids := processesRunning(t, b.Binary); len(pids) > 0 {
		t.Errorf("còn tiến trình dịch vụ: %v", pids)
	}
	if infra.stops.Load() != 1 {
		t.Errorf("Postgres stop = %d lần", infra.stops.Load())
	}
	harnesstest.RequireNoContainers(t, runID)
}

// Review Focus 2: quá timeoutMinutes giữa lúc chạy case. Hạn 9s (tính từ đầu Run) đủ rộng để dịch vụ đã build sẵn
// khởi động xong trên máy chậm, mà vẫn rơi vào lúc case chờ stub 60s.
func TestRunTimeoutKillsServiceAndContainers(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	runStuckCase(t, 0.15, 0)
}

// Review Focus 2: ctx bị hủy (Ctrl-C) giữa lúc chạy case.
func TestRunCancelKillsServiceAndContainers(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	runStuckCase(t, 30, 1500*time.Millisecond)
}

// Hủy (ctx của lệnh bị hủy, Ctrl-C) có thông điệp riêng, khác quá thời gian; ExitCode vẫn là ExitTimeout. Không cần
// Docker.
func TestRunCanceledUsesCanceledMessage(t *testing.T) {
	t.Parallel()
	root := harnesstest.CopySample(t)
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Service.Env.DatabaseURL = ""
	h := &Harness{Root: root, Config: cfg, Adapter: golang.Adapter{}, Runtime: &fakeRuntime{build: lang.BuildResult{OK: true}},
		Infra: failInfra{}, Now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := outDir(t)
	res, err := h.Run(ctx, Options{Feature: "DEMO-1", Mode: ModeCases, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	allErrorWith(t, res.Cases, demo1, Canceled())
	allErrorWith(t, readResultsFile(t, out).Cases, demo1, Canceled())
	if res.ExitCode != ExitTimeout {
		t.Errorf("exit = %d", res.ExitCode)
	}
	if log := readLog(t, out); !strings.Contains(log, Canceled()) || strings.Contains(log, "quá thời gian") {
		t.Errorf("harness.log:\n%s", log)
	}
	vr, err := h.Verify(ctx, "DEMO-1", outDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if !vr.TimedOut || vr.Coverage.NA != Canceled() {
		t.Errorf("verify = %+v", vr)
	}
}

// httpRuntime là dịch vụ giả trong tiến trình: trả 200 rỗng ở mọi đường dẫn nên healthy ngay; không cần Docker
// hay go build.
type httpRuntime struct {
	mu    sync.Mutex
	procs []*httpProcess
}

func (f *httpRuntime) Build(context.Context, string, model.ServiceConfig, string) (lang.BuildResult, error) {
	return lang.BuildResult{OK: true, Binary: "/khong-co/svc"}, nil
}

func (f *httpRuntime) UnitTests(context.Context, string, model.UnitTestConfig, string) (lang.UnitResult, error) {
	return lang.UnitResult{}, nil
}

func (f *httpRuntime) Start(_ context.Context, _ string, env map[string]string, _, _ string) (lang.Process, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+env["PORT"])
	if err != nil {
		return nil, err
	}
	p := &httpProcess{srv: &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})},
		done: make(chan struct{})}
	go func() { _ = p.srv.Serve(ln) }()
	f.mu.Lock()
	f.procs = append(f.procs, p)
	f.mu.Unlock()
	return p, nil
}

func (f *httpRuntime) Coverage(context.Context, string, string, string, string) (lang.Coverage, error) {
	return lang.Coverage{NA: "httpRuntime"}, nil
}

type httpProcess struct {
	srv        *http.Server
	once       sync.Once
	done       chan struct{}
	liveAtStop bool // còn trong danh sách dịch vụ đang chạy (KillServices) lúc Stop
	kills      atomic.Int32
}

func (p *httpProcess) Done() <-chan struct{} { return p.done }
func (p *httpProcess) ExitErr() error        { return nil }
func (p *httpProcess) Kill()                 { p.kills.Add(1); p.Stop(0) }
func (p *httpProcess) Stop(time.Duration) bool {
	p.once.Do(func() {
		live.Lock()
		_, p.liveAtStop = live.procs[p]
		live.Unlock()
		_ = p.srv.Close()
		close(p.done)
	})
	return true
}

// Dịch vụ không khai databaseUrl: không có Postgres, schema.json là schema rỗng (như migration-check khi không có
// migration), để SCHEMA_MATCH của eval so được. Không cần Docker.
func TestRunNoDatabaseWritesEmptySchema(t *testing.T) {
	t.Parallel()
	root := harnesstest.CopySample(t)
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Service.Env.DatabaseURL = ""
	rt := &httpRuntime{}
	h := &Harness{Root: root, Config: cfg, Adapter: golang.Adapter{}, Runtime: rt, Infra: failInfra{}, Now: time.Now}

	out := outDir(t)
	res, err := h.Run(context.Background(), Options{Feature: "DEMO-1", Mode: ModeCases, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 1 || len(res.Cases) != len(demo1) {
		t.Fatalf("res = %+v\nlog:\n%s", res, readLog(t, out))
	}
	if s := readSchema(t, out); len(s.Tables) != 0 {
		t.Errorf("schema = %+v", s)
	}
	if len(rt.procs) != 1 || !rt.procs[0].liveAtStop {
		t.Errorf("dịch vụ phải được ghi nhận đang chạy cho tới khi dừng: %d", len(rt.procs))
	}

	out = outDir(t)
	vr, err := h.Verify(context.Background(), "DEMO-1", out)
	if err != nil {
		t.Fatal(err)
	}
	if vr.HarnessErr != "" || vr.TimedOut {
		t.Errorf("verify = %+v", vr)
	}
	if s := readSchema(t, out); len(s.Tables) != 0 {
		t.Errorf("schema = %+v", s)
	}
	// Chỉ đếm dịch vụ của test này: live là danh sách chung, test song song khác có thể đang có dịch vụ chạy.
	n := 0
	live.Lock()
	rt.mu.Lock()
	for _, p := range rt.procs {
		if _, ok := live.procs[p]; ok {
			n++
		}
	}
	rt.mu.Unlock()
	live.Unlock()
	if n != 0 {
		t.Errorf("còn %d dịch vụ trong danh sách đang chạy sau Run", n)
	}
}

// KillServices diệt ngay mọi dịch vụ đã track mà chưa bỏ track.
func TestKillServices(t *testing.T) {
	a := &httpProcess{srv: &http.Server{}, done: make(chan struct{})}
	b := &httpProcess{srv: &http.Server{}, done: make(chan struct{})}
	untrackA := track(a)
	untrackB := track(b)
	untrackB()
	KillServices()
	untrackA()
	if a.kills.Load() != 1 || b.kills.Load() != 0 {
		t.Errorf("kills a=%d b=%d", a.kills.Load(), b.kills.Load())
	}
}

func TestTimeoutMessage(t *testing.T) {
	for m, want := range map[float64]string{
		30:   "Harness chạy quá thời gian (30 phút), đã dừng",
		0.05: "Harness chạy quá thời gian (0.05 phút), đã dừng",
		1.5:  "Harness chạy quá thời gian (1.5 phút), đã dừng",
	} {
		if got := TimeoutMessage(m); got != want {
			t.Errorf("TimeoutMessage(%v) = %q", m, got)
		}
	}
}

// Review Focus 5: Docker không chạy. Không cần Docker.
func TestRunInfraFailureMarksCasesError(t *testing.T) {
	t.Parallel()
	root := harnesstest.CopySample(t)
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	rt := &fakeRuntime{build: lang.BuildResult{OK: true, Binary: "/khong-co/svc"}}
	h := &Harness{Root: root, Config: cfg, Adapter: golang.Adapter{}, Runtime: rt,
		Infra: failInfra{errors.New("Cannot connect to the Docker daemon")}, Now: time.Now}
	out := outDir(t)
	res, err := h.Run(context.Background(), Options{Feature: "DEMO-1", Mode: ModeCases, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	msg := "không khởi động được Postgres: Cannot connect to the Docker daemon"
	allErrorWith(t, res.Cases, demo1, msg)
	allErrorWith(t, readResultsFile(t, out).Cases, demo1, msg)
	if res.ExitCode != 2 {
		t.Errorf("exit = %d", res.ExitCode)
	}
	if rt.starts.Load() != 0 {
		t.Error("không được khởi động dịch vụ khi hạ tầng lỗi")
	}

	vr, err := h.Verify(context.Background(), "DEMO-1", outDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(vr.HarnessErr, msg) {
		t.Errorf("HarnessErr = %q", vr.HarnessErr)
	}
	allErrorWith(t, vr.Run.Cases, demo1, msg)
}

// Port AisdlcCaseSuite.writeResults: cases.yaml không đọc được thì một dòng HARNESS. Không cần Docker.
func TestRunUnreadableCasesYaml(t *testing.T) {
	t.Parallel()
	root := harnesstest.CopySample(t)
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "aisdlc/features/DEMO-1/cases.yaml"), []byte("cases: [ { id: \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := &fakeRuntime{}
	h := &Harness{Root: root, Config: cfg, Adapter: golang.Adapter{}, Runtime: rt, Infra: failInfra{errors.New("x")}, Now: time.Now}
	out := outDir(t)
	res, err := h.Run(context.Background(), Options{Feature: "DEMO-1", Mode: ModeCases, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cases) != 1 || res.Cases[0].ID != "HARNESS" || res.Cases[0].Outcome != run.CaseOutcomeError ||
		!strings.HasPrefix(res.Cases[0].Message, "không đọc được cases.yaml: ") {
		t.Fatalf("cases = %+v", res.Cases)
	}
	if res.ExitCode != 2 || rt.builds.Load() != 0 {
		t.Errorf("exit = %d, builds = %d", res.ExitCode, rt.builds.Load())
	}
	if f := readResultsFile(t, out); len(f.Cases) != 1 || f.Cases[0].ID != "HARNESS" {
		t.Errorf("results.json = %+v", f)
	}
}

// Không cần Docker: build lỗi thì không chạy unit test, không khởi động hạ tầng, mọi case ERROR "build lỗi".
func TestVerifyBuildFailure(t *testing.T) {
	t.Parallel()
	root := harnesstest.CopySample(t)
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	rt := &fakeRuntime{build: lang.BuildResult{Output: "main.go:1: syntax error"}}
	h := &Harness{Root: root, Config: cfg, Adapter: golang.Adapter{}, Runtime: rt, Infra: failInfra{errors.New("không được gọi")}, Now: time.Now}
	out := outDir(t)
	vr, err := h.Verify(context.Background(), "DEMO-1", out)
	if err != nil {
		t.Fatal(err)
	}
	if vr.Build.OK || rt.units.Load() != 0 || rt.starts.Load() != 0 {
		t.Errorf("vr = %+v, units = %d", vr, rt.units.Load())
	}
	allErrorWith(t, vr.Run.Cases, demo1, "build lỗi")
	allErrorWith(t, readResultsFile(t, out).Cases, demo1, "build lỗi")
	if vr.HarnessErr != "" || vr.Run.ExitCode != 2 {
		t.Errorf("build hỏng không phải lỗi harness: HarnessErr=%q exit=%d", vr.HarnessErr, vr.Run.ExitCode)
	}
	if !strings.Contains(readLog(t, out), "syntax error") {
		t.Error("harness.log thiếu output build")
	}
}

// insertLine là số dòng có câu INSERT trong transfer.go của bản chép.
func insertLine(t *testing.T, root string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "internal/transfer/transfer.go"))
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "INSERT INTO transfer") {
			return i + 1
		}
	}
	t.Fatal("không thấy INSERT")
	return 0
}

const transferGo = "internal/transfer/transfer.go"

func TestVerifyAllGreen(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	h, infra := newSampleHarness(t, nil)
	out := outDir(t)
	vr, err := h.Verify(context.Background(), "DEMO-1", out)
	if err != nil {
		t.Fatal(err)
	}
	if !vr.Build.OK || vr.Unit.Exit != 0 || vr.HarnessErr != "" || vr.TimedOut {
		t.Fatalf("vr = %+v\nlog:\n%s", vr, readLog(t, out))
	}
	if got := rows(vr.Run.Cases); !slices.Equal(got, []string{"C1/S1/PASSED", "C2/S1/PASSED", "C3/S1/PASSED", "C9/S1/FAILED"}) {
		t.Errorf("cases = %v", got)
	}
	if vr.Coverage.NA != "" {
		t.Fatalf("coverage NA = %q", vr.Coverage.NA)
	}
	if line := insertLine(t, h.Root); !vr.Coverage.Lines[transferGo][line] {
		t.Errorf("dòng INSERT %d chưa được phủ: %v", line, vr.Coverage.Lines[transferGo])
	}
	infra.requireCleaned(t)
}

// Review Focus 3: dịch vụ phớt lờ SIGTERM. Theo spec §3, coverage là N/A kể cả khi có unit profile; case vẫn giữ.
func TestVerifyServiceIgnoresSigterm(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	h, infra := newSampleHarness(t, func(c *model.RepoConfig) {
		c.Service.ExtraEnv = map[string]string{"SAMPLE_IGNORE_SIGTERM": "1"}
		c.Service.ShutdownTimeoutSeconds = 1
	})
	rt := &prebuiltRuntime{} // build thật, ghi lại tiến trình
	h.Runtime = rt
	out := outDir(t)
	vr, err := h.Verify(context.Background(), "DEMO-1", out)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows(vr.Run.Cases); !slices.Equal(got, []string{"C1/S1/PASSED", "C2/S1/PASSED", "C3/S1/PASSED", "C9/S1/FAILED"}) {
		t.Errorf("cases = %v\nlog:\n%s", got, readLog(t, out))
	}
	if vr.Unit.Profile == "" {
		t.Fatal("cần unit profile để kiểm coverage không lấy số chỉ từ unit test")
	}
	if !strings.Contains(vr.Coverage.NA, "không tắt êm") || vr.Coverage.Lines != nil {
		t.Errorf("coverage = %+v", vr.Coverage)
	}
	if log := readLog(t, out); !strings.Contains(log, "không tắt êm") {
		t.Errorf("harness.log thiếu thông điệp kill:\n%s", log)
	}
	if len(rt.procs) != 1 {
		t.Fatalf("dịch vụ phải được khởi động một lần (%d)", len(rt.procs))
	}
	select {
	case <-rt.procs[0].Done():
	default:
		t.Error("tiến trình dịch vụ còn chạy")
	}
	if pids := processesRunning(t, vr.Build.Binary); len(pids) > 0 {
		t.Errorf("còn tiến trình dịch vụ: %v", pids)
	}
	infra.requireCleaned(t)
}
