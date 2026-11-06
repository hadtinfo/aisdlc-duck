package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"aisdlc/internal/harness/harnessapi"
	"aisdlc/internal/harness/harnesstest"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
)

// demo1Cases là các case của tính năng DEMO-1 trong dịch vụ mẫu.
var demo1Cases = []string{"C1", "C2", "C3", "C9"}

var harnessBuild struct {
	once sync.Once
	dir  string
	path string
	err  error
}

// harnessBinary build ./cmd/aisdlc-harness một lần cho cả gói test (thư mục được xóa trong TestMain).
func harnessBinary(t *testing.T) string {
	t.Helper()
	harnessBuild.once.Do(func() {
		harnessBuild.dir, harnessBuild.err = os.MkdirTemp("", "aisdlc-harness-bin")
		if harnessBuild.err != nil {
			return
		}
		harnessBuild.path = filepath.Join(harnessBuild.dir, "aisdlc-harness")
		out, err := exec.Command("go", "build", "-o", harnessBuild.path, "aisdlc/cmd/aisdlc-harness").CombinedOutput()
		if err != nil {
			harnessBuild.err = fmt.Errorf("go build aisdlc-harness: %v\n%s", err, out)
		}
	})
	if harnessBuild.err != nil {
		t.Fatal(harnessBuild.err)
	}
	return harnessBuild.path
}

func TestMain(m *testing.M) {
	code := m.Run()
	if harnessBuild.dir != "" {
		_ = os.RemoveAll(harnessBuild.dir)
	}
	os.Exit(code)
}

// sampleRunner dựng harnessCaseRunner trên bản chép dịch vụ mẫu, chạy binary bin.
func sampleRunner(t *testing.T, std context.Context, bin string) *harnessCaseRunner {
	t.Helper()
	root := harnesstest.CopySample(t)
	return &harnessCaseRunner{std: std, root: root, find: func() (string, error) { return bin, nil }}
}

func allError(t *testing.T, cases []run.CaseResult, msg string) {
	t.Helper()
	if len(cases) != len(demo1Cases) {
		t.Fatalf("cases = %+v", cases)
	}
	for i, c := range cases {
		if c.ID != demo1Cases[i] || c.Outcome != run.CaseOutcomeError || !strings.Contains(c.Message, msg) {
			t.Errorf("case %d = %+v, muốn %s ERROR %q", i, c, demo1Cases[i], msg)
		}
	}
}

// fakeHarness là script thay aisdlc-harness: body chạy với $OUT là giá trị --out.
func fakeHarness(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "aisdlc-harness")
	script := "#!/bin/sh\nOUT=\nwhile [ $# -gt 0 ]; do [ \"$1\" = --out ] && OUT=$2; shift; done\nmkdir -p \"$OUT\"\n" + body + "\n"
	must(t, os.WriteFile(p, []byte(script), 0o755))
	return p
}

func writeStale(t *testing.T, out string) {
	t.Helper()
	must(t, os.MkdirAll(out, 0o755))
	must(t, harnessapi.WriteResults(out, "DEMO-1", harnessapi.ModeCases, []run.CaseResult{
		{ID: "C1", Step: "S1", Outcome: run.CaseOutcomePassed}}))
	must(t, os.WriteFile(filepath.Join(out, "schema.json"), []byte("{}"), 0o644))
	must(t, harnessapi.WriteResult(out, harnessapi.Result{Run: &harnessapi.RunResult{}}))
}

// Port MavenRunnerTest.verifyClearsStaleResultsAndReadsThemLikeRunCases: results.json/schema.json cũ trong OutDir,
// harness hỏng (panic) trước khi ghi: kết quả là NoResults, không bao giờ dùng file cũ; panic thành HarnessErr.
func TestRunnerHarnessPanicNeverUsesStaleResults(t *testing.T) {
	bin := fakeHarness(t, `echo 'harness lỗi: boom' >> "$OUT/harness.log"; printf '{"panic":"boom"}' > "$OUT/harness-result.json"; exit 2`)
	r := sampleRunner(t, context.Background(), bin)
	out := r.OutDir("DEMO-1")
	writeStale(t, out)
	res, err := r.RunCases("DEMO-1", nil, "cases")
	must(t, err)
	eq(t, res.ExitCode, 2)
	allError(t, res.Cases, harnessapi.NoResults)
	if _, err := os.Stat(filepath.Join(out, "schema.json")); err == nil {
		t.Error("schema.json cũ vẫn còn")
	}
	contains(t, r.OutputTail("DEMO-1", 20), "boom")

	writeStale(t, out)
	vr, err := harnessVerifier{root: r.root, find: r.find}.Verify(context.Background(), "DEMO-1", out)
	must(t, err)
	eq(t, vr.HarnessErr, "boom")
	eq(t, vr.Coverage.NA, "harness lỗi: boom")
	// panic không phải build hỏng: eval chỉ báo lỗi harness, không báo "go build lỗi"
	if !vr.Build.OK || vr.Run.ExitCode != 2 {
		t.Errorf("vr = %+v", vr)
	}
	for _, n := range []string{"results.json", "schema.json"} {
		if _, err := os.Stat(filepath.Join(out, n)); err == nil {
			t.Errorf("%s cũ vẫn còn sau Verify", n)
		}
	}
}

// aisdlc-harness chết không ghi kết quả: NoResults, exit 2; lý do và stderr của nó vào harness.log.
func TestRunnerHarnessDiesWithoutResult(t *testing.T) {
	bin := fakeHarness(t, `echo "fatal error: out of memory" >&2; exit 7`)
	r := sampleRunner(t, context.Background(), bin)
	writeStale(t, r.OutDir("DEMO-1"))
	res, err := r.RunCases("DEMO-1", []string{"S1"}, "cases")
	must(t, err)
	eq(t, res.ExitCode, 2)
	allError(t, res.Cases, harnessapi.NoResults)
	contains(t, r.OutputTail("DEMO-1", 20), "aisdlc-harness không ghi kết quả (exit status 7)", "fatal error: out of memory")

	vr, err := harnessVerifier{root: r.root, find: r.find}.Verify(context.Background(), "DEMO-1", r.OutDir("DEMO-1"))
	must(t, err)
	contains(t, vr.HarnessErr, "aisdlc-harness không ghi kết quả", "out of memory")
	if !vr.Build.OK || vr.Run.ExitCode != 2 {
		t.Errorf("vr = %+v", vr)
	}
}

// Result.Error (không ghi được out…) là error của RunCases/Verify như khi harness chạy trong tiến trình.
func TestRunnerHarnessErrorIsError(t *testing.T) {
	bin := fakeHarness(t, `printf '{"error":"Không ghi được out"}' > "$OUT/harness-result.json"; exit 3`)
	r := sampleRunner(t, context.Background(), bin)
	if _, err := r.RunCases("DEMO-1", nil, "cases"); err == nil || err.Error() != "Không ghi được out" {
		t.Errorf("err = %v", err)
	}
	if _, err := (harnessVerifier{root: r.root, find: r.find}).Verify(context.Background(), "DEMO-1", r.OutDir("DEMO-1")); err == nil {
		t.Error("Verify không trả lỗi")
	}
}

// Không tìm thấy aisdlc-harness: fail closed — mọi case ERROR với lý do, eval báo lỗi harness; không đọc kết quả cũ.
func TestRunnerMissingHarnessFailsClosed(t *testing.T) {
	missing := func() (string, error) {
		return locateHarness(func() (string, error) { return t.TempDir() + "/aisdlc", nil },
			func(string) (string, error) { return "", exec.ErrNotFound })
	}
	_, ferr := missing()
	if ferr == nil {
		t.Fatal("findHarness không lỗi")
	}
	contains(t, ferr.Error(), "không tìm thấy aisdlc-harness: ")

	r := sampleRunner(t, context.Background(), "")
	r.find = missing
	writeStale(t, r.OutDir("DEMO-1"))
	res, err := r.RunCases("DEMO-1", nil, "cases")
	must(t, err)
	eq(t, res.ExitCode, 2)
	allError(t, res.Cases, "không tìm thấy aisdlc-harness: ")
	contains(t, r.OutputTail("DEMO-1", 5), "không tìm thấy aisdlc-harness")

	vr, err := harnessVerifier{root: r.root, find: missing}.Verify(context.Background(), "DEMO-1", r.OutDir("DEMO-1"))
	must(t, err)
	contains(t, vr.HarnessErr, "không tìm thấy aisdlc-harness: ")
	if vr.Run.ExitCode != 2 || vr.Coverage.NA == "" {
		t.Errorf("vr = %+v", vr)
	}
	if _, err := os.Stat(filepath.Join(r.OutDir("DEMO-1"), "results.json")); err == nil {
		t.Error("results.json cũ vẫn còn")
	}
}

func TestRunnerUnknownModeAborts(t *testing.T) {
	r := sampleRunner(t, context.Background(), "/khong-chay")
	_, err := r.RunCases("DEMO-1", nil, "lạ")
	var a *Abort
	if !asAbort(err, &a) || a.Code != 2 {
		t.Fatalf("err = %v", err)
	}
	_, perr := harnessapi.ParseMode("lạ")
	eq(t, a.Msg, perr.Error())
}

// ctx của lệnh đã hủy trước khi chạy: không khởi động harness, mọi case ERROR CanceledMessage.
func TestRunnerCanceledBeforeStart(t *testing.T) {
	std, cancel := context.WithCancel(context.Background())
	cancel()
	r := sampleRunner(t, std, fakeHarness(t, "exit 99"))
	res, err := r.RunCases("DEMO-1", nil, "cases")
	must(t, err)
	eq(t, res.ExitCode, harnessapi.ExitTimeout)
	allError(t, res.Cases, harnessapi.CanceledMessage)
}

// Bản thật mặc định của Context là binary aisdlc-harness theo language của config.
func TestDefaultRunnerAndVerifierUseHarness(t *testing.T) {
	ctx := NewContext(strings.NewReader(""), &strings.Builder{}, &strings.Builder{}, noConsole(), t.TempDir())
	cfg := model.RepoConfig{Language: "go"}
	if _, ok := ctx.Runner("/repo", cfg).(*harnessCaseRunner); !ok {
		t.Errorf("runner = %T", ctx.Runner("/repo", cfg))
	}
	if _, ok := ctx.Verifier("/repo", cfg).(harnessVerifier); !ok {
		t.Errorf("verifier = %T", ctx.Verifier("/repo", cfg))
	}
	if _, err := ctx.Runner("/repo", model.RepoConfig{Language: "cobol"}).RunCases("DEMO-1", nil, "cases"); err == nil {
		t.Error("language lạ không lỗi")
	}
}

func rowsOf(cs []run.CaseResult) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID+"/"+string(c.Outcome))
	}
	return out
}

// Chạy thật qua tiến trình con (Docker): run và verify trên dịch vụ mẫu cho đúng kết quả của harness.
func TestRunnerSubprocessEndToEnd(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	r := sampleRunner(t, context.Background(), harnessBinary(t))
	res, err := r.RunCases("DEMO-1", nil, "cases")
	must(t, err)
	want := []string{"C1/PASSED", "C2/PASSED", "C3/PASSED", "C9/FAILED"}
	if !slices.Equal(rowsOf(res.Cases), want) || res.ExitCode != 1 {
		t.Fatalf("res = %+v\nlog:\n%s", res, r.OutputTail("DEMO-1", 80))
	}
	eq(t, res.Cases[3].Message, "status: mong đợi 201, nhận 200")
	if _, err := os.Stat(filepath.Join(r.OutDir("DEMO-1"), "schema.json")); err != nil {
		t.Error("không có schema.json")
	}

	out := filepath.Join(r.root, "eval-out")
	vr, err := harnessVerifier{root: r.root, find: r.find}.Verify(context.Background(), "DEMO-1", out)
	must(t, err)
	if !vr.Build.OK || vr.HarnessErr != "" || vr.TimedOut || vr.Coverage.NA != "" || len(vr.Coverage.Lines) == 0 {
		t.Fatalf("vr = %+v\nlog:\n%s", vr, tailFile(filepath.Join(out, HarnessLog), 80))
	}
	if !slices.Equal(rowsOf(vr.Run.Cases), want) {
		t.Errorf("cases = %v", rowsOf(vr.Run.Cases))
	}
	requireNoProcesses(t, r.root)
}

var runIDRe = regexp.MustCompile(`run (aisdlc-[0-9a-z]+-[0-9a-f]{8})`)

// stuckRunner: DEMO-1 với stub chậm 60s; chạy RunCases nền, chờ dịch vụ sẵn sàng rồi gọi interrupt.
func stuckRunner(t *testing.T, interrupt func(cancel context.CancelFunc)) (*harnessCaseRunner, run.CaseRunResult, time.Duration) {
	std, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := sampleRunner(t, std, harnessBinary(t))
	p := filepath.Join(r.root, "aisdlc/features/DEMO-1/cases.yaml")
	b, err := os.ReadFile(p)
	must(t, err)
	must(t, os.WriteFile(p, bytes.Replace(b, []byte("response: { status: 200, jsonBody: { balance: 1000 } }"),
		[]byte("response: { status: 200, fixedDelayMilliseconds: 60000, jsonBody: { balance: 1000 } }"), 1), 0o644))

	type ret struct {
		res run.CaseRunResult
		err error
	}
	done := make(chan ret, 1)
	go func() {
		res, err := r.RunCases("DEMO-1", nil, "cases")
		done <- ret{res, err}
	}()
	deadline := time.Now().Add(3 * time.Minute)
	for !strings.Contains(r.OutputTail("DEMO-1", 200), "dịch vụ sẵn sàng") {
		if time.Now().After(deadline) {
			t.Fatalf("dịch vụ chưa sẵn sàng:\n%s", r.OutputTail("DEMO-1", 80))
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond) // case C1 đang chờ stub 60s
	start := time.Now()
	interrupt(cancel)
	select {
	case got := <-done:
		must(t, got.err)
		return r, got.res, time.Since(start)
	case <-time.After(90 * time.Second):
		t.Fatalf("RunCases không dừng sau khi hủy:\n%s", r.OutputTail("DEMO-1", 80))
	}
	return nil, run.CaseRunResult{}, 0
}

// Ctrl-C lần đầu (ctx của lệnh hủy): aisdlc-harness nhận SIGINT, dừng dịch vụ, dọn container rồi ghi kết quả hủy.
func TestRunnerSubprocessCancel(t *testing.T) {
	harnesstest.RequireDocker(t)
	t.Parallel()
	r, res, took := stuckRunner(t, func(cancel context.CancelFunc) { cancel() })
	if took > 40*time.Second {
		t.Errorf("hủy mất %v", took)
	}
	eq(t, res.ExitCode, harnessapi.ExitTimeout)
	allError(t, res.Cases, harnessapi.CanceledMessage)
	log := r.OutputTail("DEMO-1", 400)
	contains(t, log, harnessapi.CanceledMessage)
	m := runIDRe.FindStringSubmatch(log)
	if m == nil {
		t.Fatalf("không thấy run id trong log:\n%s", log)
	}
	harnesstest.RequireNoContainers(t, m[1])
	requireNoProcesses(t, r.root)
}

// Ctrl-C lần hai (KillHarness sau khi hủy): aisdlc-harness SIGKILL dịch vụ và thoát ngay; không còn tiến trình.
// Không song song: KillHarness diệt mọi aisdlc-harness của tiến trình test.
func TestRunnerSubprocessSecondInterruptKills(t *testing.T) {
	harnesstest.RequireDocker(t)
	r, _, took := stuckRunner(t, func(cancel context.CancelFunc) {
		cancel()
		time.Sleep(200 * time.Millisecond)
		KillHarness()
	})
	if took > 10*time.Second {
		t.Errorf("Ctrl-C lần hai mất %v", took)
	}
	requireNoProcesses(t, r.root)
}

// requireNoProcesses: không còn tiến trình nào (aisdlc-harness, dịch vụ) có dòng lệnh chứa root.
func requireNoProcesses(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat("/proc/self/cmdline"); err != nil {
		t.Skip("không có /proc")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var left []string
		entries, _ := os.ReadDir("/proc")
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
			if err == nil && bytes.Contains(b, []byte(root)) {
				left = append(left, e.Name()+": "+strings.ReplaceAll(string(b), "\x00", " "))
			}
		}
		if len(left) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("còn tiến trình: %v", left)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
