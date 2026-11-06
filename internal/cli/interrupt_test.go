package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/run"
)

// fakeSignals là nguồn tín hiệu giả: kênh do test gửi, ghi lại Kill/Exit theo thứ tự.
type fakeSignals struct {
	ch      chan os.Signal
	events  chan string // "stop", "kill", "exit:<code>"
	stopped bool
}

func newFakeSignals() *fakeSignals {
	return &fakeSignals{ch: make(chan os.Signal, 2), events: make(chan string, 8)}
}

func (f *fakeSignals) interrupts() *Interrupts {
	return &Interrupts{
		Notify: func() (<-chan os.Signal, func()) { return f.ch, func() { f.events <- "stop" } },
		Kill:   func() { f.events <- "kill" },
		Exit: func(code int) {
			f.events <- "exit:" + strconv.Itoa(code)
		},
	}
}

func (f *fakeSignals) next(t *testing.T) string {
	t.Helper()
	select {
	case e := <-f.events:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("hết giờ chờ sự kiện tín hiệu")
		return ""
	}
}

func bareContext(t *testing.T) (*Context, *bytes.Buffer) {
	e := &bytes.Buffer{}
	return NewContext(strings.NewReader(""), &bytes.Buffer{}, e, noConsole(), t.TempDir()), e
}

// Tín hiệu đầu: in thông báo ra stderr và hủy ctx; tín hiệu thứ hai: diệt dịch vụ rồi thoát 130. Sau đó lệnh
// nhận Abort 130 và ctx của Context trở lại như cũ.
func TestInterruptibleFirstSignalCancelsSecondKills(t *testing.T) {
	ctx, stderr := bareContext(t)
	sig := newFakeSignals()
	ctx.WithInterrupts(sig.interrupts())
	outer := ctx.Std()

	err := ctx.interruptible(func() error {
		std := ctx.Std()
		sig.ch <- os.Interrupt
		select {
		case <-std.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("tín hiệu đầu không hủy ctx")
		}
		contains(t, stderr.String(), "Đang dừng harness và dọn container (tối đa 30 giây)…")
		sig.ch <- syscall.SIGTERM
		eq(t, sig.next(t), "kill") // diệt dịch vụ trước khi thoát
		eq(t, sig.next(t), "exit:130")
		return nil
	})

	var a *Abort
	if !asAbort(err, &a) || a.Code != ExitInterrupted || a.Msg != interruptedMsg {
		t.Fatalf("err = %v", err)
	}
	eq(t, sig.next(t), "stop") // thôi bắt tín hiệu khi harness xong
	if ctx.Std() != outer || outer.Err() != nil {
		t.Error("ctx của Context không được trả lại như trước")
	}
}

// Không có tín hiệu: kết quả và lỗi của fn đi qua nguyên vẹn, chỉ bắt tín hiệu trong lúc fn chạy.
func TestInterruptiblePassesThroughWithoutSignal(t *testing.T) {
	ctx, stderr := bareContext(t)
	sig := newFakeSignals()
	ctx.WithInterrupts(sig.interrupts())
	boom := errors.New("boom")
	if err := ctx.interruptible(func() error { return boom }); err != boom {
		t.Fatalf("err = %v", err)
	}
	eq(t, sig.next(t), "stop")
	eq(t, stderr.String(), "")

	// không có nguồn tín hiệu (test, hook): vẫn chạy fn, ctx của lệnh bị hủy thì vẫn là Abort 130
	ctx2, _ := bareContext(t)
	if err := ctx2.interruptible(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	std, cancel := context.WithCancel(context.Background())
	cancel()
	ctx2.WithStd(std)
	var a *Abort
	if err := ctx2.interruptible(func() error { return nil }); !asAbort(err, &a) || a.Code != ExitInterrupted {
		t.Fatalf("err = %v", err)
	}
}

// Ngoài lúc harness chạy không bắt tín hiệu: lệnh không gọi harness (status, approve) không đụng Notify.
func TestNoSignalTrapOutsideHarness(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	sig := newFakeSignals()
	r.interrupts = sig.interrupts()
	r.ok(noConsole(), "status")
	select {
	case e := <-sig.events:
		t.Fatalf("status không được bắt tín hiệu: %s", e)
	default:
	}
}

func (r *testRepo) snapshot(rel ...string) map[string]string {
	m := map[string]string{}
	for _, p := range rel {
		b, _ := os.ReadFile(filepath.Join(r.root, p))
		m[p] = string(b)
	}
	return m
}

const (
	gatesRel = "aisdlc/features/PAY-1/gates.json"
	stepsRel = "aisdlc/features/PAY-1/steps.json"
)

// Ctrl-C lúc `step finish` đang chạy case: thoát 130, steps.json và gates.json không đổi (bước vẫn IN_PROGRESS).
func TestStepFinishInterruptedLeavesStateUnchanged(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.write(s1File, "class LimitController {}\n")
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomePassed)}
	}
	sig := newFakeSignals()
	r.interrupts = sig.interrupts()
	r.runner.onRun = func() {
		sig.ch <- os.Interrupt
		<-r.runnerStd.Done() // harness thật thấy ctx bị hủy
	}
	before := r.snapshot(gatesRel, stepsRel)

	eq(t, r.run("step", "finish", "S1"), ExitInterrupted)

	contains(t, r.err, "Đang dừng harness và dọn container", interruptedMsg)
	notContains(t, r.out, "Đã chấp nhận bước")
	eq(t, r.snapshot(gatesRel, stepsRel)[stepsRel], before[stepsRel])
	eq(t, r.snapshot(gatesRel, stepsRel)[gatesRel], before[gatesRel])
	r.interrupts, r.runner.onRun = nil, nil
	eq(t, r.status().Steps["S1"], "IN_PROGRESS")
}

// ctx của lệnh bị hủy lúc `submit cases` chạy harness: thoát 130, không nộp cổng.
func TestSubmitCasesCanceledLeavesGatesUnchanged(t *testing.T) {
	r := newTestRepo(t)
	r.designApproved()
	r.runner.cases = failingCases
	std, cancel := context.WithCancel(context.Background())
	r.runner.onRun = cancel
	r.std = std
	before := r.snapshot(gatesRel, stepsRel)

	eq(t, r.run("submit", "cases"), ExitInterrupted)

	contains(t, r.err, interruptedMsg)
	notContains(t, r.out, "Đã nộp cổng")
	after := r.snapshot(gatesRel, stepsRel)
	eq(t, after[gatesRel], before[gatesRel])
	eq(t, after[stepsRel], before[stepsRel])
	r.std, r.runner.onRun = nil, nil
	eq(t, r.state(gate.CASES), "NOT_SUBMITTED")
}

// Ctrl-C lúc migration-check của `submit design`: thoát 130, không chép schema-expected.json, không nộp cổng.
func TestSubmitDesignInterruptedLeavesGatesUnchanged(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	r.ok(typed("PAY-1"), "approve", "brd")
	sig := newFakeSignals()
	r.interrupts = sig.interrupts()
	r.runner.onRun = func() {
		sig.ch <- os.Interrupt
		<-r.runnerStd.Done()
	}
	before := r.snapshot(gatesRel)

	eq(t, r.run("submit", "design"), ExitInterrupted)

	contains(t, r.err, interruptedMsg)
	notContains(t, r.out, "Đã ghi ", "Đã nộp cổng")
	eq(t, r.snapshot(gatesRel)[gatesRel], before[gatesRel])
	if r.exists("aisdlc/features/PAY-1/schema-expected.json") {
		t.Error("không được chép schema-expected.json khi bị hủy")
	}
	r.interrupts, r.runner.onRun = nil, nil
	eq(t, r.state(gate.DESIGN), "NOT_SUBMITTED")
}

// Ctrl-C lúc `aisdlc eval`: thoát 130, không ghi report.
func TestEvalCanceledWritesNoReport(t *testing.T) {
	r := evalCmdFx(t)
	std, cancel := context.WithCancel(context.Background())
	cancel()
	r.std = std
	eq(t, r.run("eval"), ExitInterrupted)
	contains(t, r.err, interruptedMsg)
	if r.exists("aisdlc/features/PAY-1/eval/report.json") {
		t.Error("không được ghi report.json khi bị hủy")
	}
}

// Thư mục tính năng không đọc được (quyền): CI eval fail closed thay vì coi như không có tính năng.
func TestChangedFeaturesUnreadableDirFailsClosed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root đọc được mọi thư mục")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "aisdlc", "features")
	must(t, os.MkdirAll(filepath.Join(dir, "PAY-1"), 0o755))
	must(t, os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	changes := []gitx.FileChange{{Path: "aisdlc/features/PAY-1/cases.yaml"}}
	if _, err := changedFeatures(root, changes); err == nil {
		t.Fatal("phải trả lỗi khi không đọc được thư mục tính năng")
	}
	// chưa có thư mục tính năng: không có gì để eval
	keys, err := changedFeatures(t.TempDir(), changes)
	if err != nil || len(keys) != 0 {
		t.Fatalf("keys=%v err=%v", keys, err)
	}
}
