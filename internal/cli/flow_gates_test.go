package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"aisdlc/internal/eval"
	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/run"
	"aisdlc/internal/step"
)

// CliFlowTest (phần submit/approve/reject/reopen) và vài test thêm cho reject, reopen, submit review.

// designApproved: setup + init PAY-1 + BRD/thiết kế đã duyệt (TestRepo.designApproved).
func (r *testRepo) designApproved() {
	r.t.Helper()
	r.setupInit()
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	r.ok(typed("PAY-1"), "approve", "brd")
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "design")
}

func endsWith(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) < len(want) {
		t.Fatalf("got %v, phải kết thúc bằng %v", got, want)
	}
	eqList(t, got[len(got)-len(want):], want...)
}

// Phần step/status/diff ở TestFullFlowStepAndDiff.
func TestFullFlowHappyPathWithoutHarness(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	eq(t, r.run("submit", "brd"), 1) // template chưa điền
	contains(t, r.out, "BRD_EMPTY_SECTION")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	r.ok(typed("PAY-1"), "approve", "brd")

	r.ok(noConsole(), "submit", "design")
	eqList(t, r.runner.calls, "migration-check:")
	contains(t, r.out, "Đang chạy migration nháp qua harness (migration-check)...")
	contains(t, r.read("aisdlc/features/PAY-1/schema-expected.json"), "transfer_limit_usage")
	r.ok(typed("PAY-1"), "approve", "design")

	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomeFailed), caseResult("C2", "S2", run.CaseOutcomePassed)}
	}
	r.ok(noConsole(), "submit", "cases")
	contains(t, r.out, "C2") // case pass trước khi code: chỉ là thông tin
	contains(t, r.out, "Đang chạy bộ case qua harness (mong đợi case fail vì chưa có code)...")
	// trước khi code: chạy trên schema đã thiết kế (migration nháp), không phải schema hiện tại
	endsWith(t, r.runner.calls, "cases-with-drafts:")
	r.ok(typed("PAY-1"), "approve", "cases")
	contains(t, r.out, "Đã duyệt cổng CASES bởi Dev Một (hash ")
	eq(t, r.state(gate.CASES), "APPROVED")
}

func TestSubmitCasesRequiresAtLeastOneFailing(t *testing.T) {
	r := newTestRepo(t)
	r.designApproved()
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomePassed), caseResult("C2", "S2", run.CaseOutcomePassed)}
	}
	eq(t, r.run("submit", "cases"), 1)
	contains(t, r.out, "CASES_NONE_FAILING")
}

func TestSubmitCasesHarnessErrorIsFinding(t *testing.T) {
	r := newTestRepo(t)
	r.designApproved()
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomeError), caseResult("C2", "S2", run.CaseOutcomeFailed)}
	}
	eq(t, r.run("submit", "cases"), 1)
	contains(t, r.out, "CASES_HARNESS_ERROR", "C1")
}

// designApprovedWith: setup + init PAY-1, design.yaml ("" = template) và migration nháp, BRD và thiết kế đã duyệt.
func (r *testRepo) designApprovedWith(designYaml, draft string) {
	r.t.Helper()
	r.setupInit()
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	r.ok(typed("PAY-1"), "approve", "brd")
	if designYaml != "" {
		r.write(designFile, designYaml)
	}
	r.write("aisdlc/features/PAY-1/db/"+draft, "CREATE TABLE transfer_limit_usage (used_amount BIGINT);\n")
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "design")
}

var setupError = run.CaseResult{ID: "C1", Step: "S1", Outcome: run.CaseOutcomeError,
	Message: `given.sql: ERROR: relation "transfer_limit_usage" does not exist`}

func laterTableCases(steps []string) []run.CaseResult {
	if len(steps) == 0 {
		return failingCases(nil)
	}
	return []run.CaseResult{setupError}
}

func TestSubmitCasesFlagsFirstStepCaseNeedingLaterStepTable(t *testing.T) {
	r := newTestRepo(t)
	r.designApprovedWith("", "00100_create_usage.sql") // template: migration không thuộc scope S1
	r.runner.cases = laterTableCases
	eq(t, r.run("submit", "cases"), 1)
	endsWith(t, r.runner.calls, "cases-with-drafts:", "cases:S1")
	contains(t, r.out, "CASES_SETUP_NEEDS_LATER_TABLE", "C1",
		"Đang chạy case của S1 trên schema hiện tại (chưa có migration nháp)...")
}

func TestSubmitCasesSkipsLaterTableCheckWhenFirstStepOwnsMigrations(t *testing.T) {
	r := newTestRepo(t)
	r.designApprovedWith(`steps:
  - id: S1
    title: Bảng và API
    scope: ["internal/**/limit/**", "db/migrations/**"]
    budget: { files: 3, lines: 120 }
  - id: S2
    title: Tính hạn mức
    scope: ["internal/**/limit/service/**"]
    budget: { files: 2, lines: 80 }
impact: { level: low, labels: [], reason: "Thêm bảng hạn mức" }
`, "00100_create_usage.sql")
	r.runner.cases = laterTableCases
	if code := r.run("submit", "cases"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	endsWith(t, r.runner.calls, "cases-with-drafts:")
}

func TestSubmitDesignMigrationFailureReportsHarnessTail(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.run("submit", "brd")
	r.runC(typed("PAY-1"), "approve", "brd")
	r.runner.migrationExit = 1
	r.runner.writeSchema = false
	var log strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&log, "log %d\n", i)
	}
	r.runner.log = log.String()

	eq(t, r.run("submit", "design"), 1)
	contains(t, r.out, "DESIGN_MIGRATION_INVALID", "log 30", "log 11", "20 dòng cuối output harness:",
		"(exit 1, không có schema.json)")
	notContains(t, r.out, "log 10\n")
	eq(t, r.exists("aisdlc/features/PAY-1/schema-expected.json"), false)
}

func TestSubmitDesignWithBrokenYamlIsFindingNotCrash(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.run("submit", "brd")
	r.runC(typed("PAY-1"), "approve", "brd")
	r.write(designFile, "steps: [ {id: S1\n")
	eq(t, r.run("submit", "design"), 1)
	contains(t, r.out, "DESIGN_YAML_INVALID")
	must(t, os.Remove(filepath.Join(r.root, designFile)))
	eq(t, r.run("submit", "design"), 1)
	contains(t, r.out, "DESIGN_YAML_INVALID")
}

func TestSubmitReviewIsNotAvailableYet(t *testing.T) {
	r := newTestRepo(t)
	r.designApproved()
	eq(t, r.run("submit", "review"), 2)
	contains(t, r.err, "Cổng REVIEW bị khóa: cổng trước chưa được duyệt.")
}

func TestSubmitWithoutSubcommandPrintsUsageExit2(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	eq(t, r.run("submit"), 2)
	contains(t, r.err, "brd", "design", "cases", "review")
	eq(t, r.run("submit", "nope"), 2)
}

func TestApproveNeedsSubmittedGate(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	eq(t, r.runC(typed("PAY-1"), "approve", "brd"), 2)
	contains(t, r.out, "Tính năng PAY-1 — cổng BRD (NOT_SUBMITTED)", "Chưa nộp.")
	contains(t, r.err, "Cổng BRD đang ở trạng thái NOT_SUBMITTED; chỉ duyệt được khi AWAITING_APPROVAL")
	eq(t, r.runC(typed("PAY-1"), "approve", "nope"), 2)
	contains(t, r.err, "Cổng không hợp lệ: nope")
}

func TestApproveWithoutConsoleExit3(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	eq(t, r.run("approve", "brd"), 3)
	contains(t, r.err, "Lệnh này chỉ dev được chạy")
	r.noApproval(gate.BRD)
	// gõ sai key: hủy
	eq(t, r.runC(typed("PAY-2"), "approve", "brd"), 3)
	contains(t, r.err, "Key không khớp")
	// thẻ review in trước lời nhắc
	c := &cardPeek{r: r}
	r.ok(c, "approve", "brd")
	contains(t, c.seen, "Review card — PAY-1 · cổng BRD", "Hash hiện tại: ")
	contains(t, r.out, "Đã duyệt cổng BRD bởi Dev Một (hash ")
}

// cardPeek là console gõ PAY-1, ghi lại stdout lúc được hỏi.
type cardPeek struct {
	r    *testRepo
	seen string
}

func (c *cardPeek) ReadLine(string) (string, bool) {
	c.seen = c.r.currentOut()
	return "PAY-1", true
}

func TestRejectWritesFeedbackAndClears(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	eq(t, r.runC(typed("PAY-1"), "reject", "brd"), 2) // thiếu -m
	r.ok(typed("PAY-1"), "reject", "brd", "-m", "Thiếu quy tắc làm tròn")
	contains(t, r.out, "Đã trả lại cổng BRD; góp ý ghi vào aisdlc/features/PAY-1/feedback.md.")
	contains(t, r.read("aisdlc/features/PAY-1/feedback.md"), "Thiếu quy tắc làm tròn")
	eq(t, r.state(gate.BRD), "NOT_SUBMITTED")
	eq(t, r.run("reject", "brd", "-m", "x"), 3)
}

func TestReopenClearsApproval(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	r.ok(typed("PAY-1"), "approve", "brd")
	r.ok(typed("PAY-1"), "reopen", "brd")
	contains(t, r.out, "Đã mở lại cổng BRD; sửa artifact rồi `aisdlc submit brd`.")
	r.noApproval(gate.BRD)
	eq(t, r.state(gate.BRD), "NOT_SUBMITTED")
}

func TestLiteFlowGateOutsideFlowExit2(t *testing.T) {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	r.ok(noConsole(), "init", "PAY-1", "--lite")
	eq(t, r.run("submit", "brd"), 2)
	contains(t, r.err, "Cổng BRD không thuộc luồng LITE.")
	eq(t, r.runC(typed("PAY-1"), "approve", "design"), 2)
	contains(t, r.err, "Cổng DESIGN không thuộc luồng LITE.")
}

// casesApproved: designApproved + cases đã duyệt.
func (r *testRepo) casesApproved() {
	r.t.Helper()
	r.designApproved()
	r.runner.cases = failingCases
	r.ok(noConsole(), "submit", "cases")
	r.ok(typed("PAY-1"), "approve", "cases")
}

func TestSubmitReviewFindings(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	eq(t, r.run("submit", "review"), 1)
	contains(t, r.out, "REVIEW_STEPS_NOT_ACCEPTED", "S1 (NOT_STARTED)", "REVIEW_NO_EVAL", "REVIEW_AGENT_MISSING",
		"Sửa rồi chạy lại `aisdlc submit review`.")
	eq(t, r.state(gate.REVIEW), "SUBMITTED_WITH_FINDINGS")
}

func TestSubmitReviewCleanThenApprove(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.acceptAllSteps()
	f := r.feature()
	tree, err := f.Git().SnapshotTreeExcluding(f.ctx.Std(), f.Paths.Rel(f.Paths.Dir()))
	must(t, err)
	rep := eval.NewEvalReport("PAY-1", testNow, "local", []eval.HardCheck{{ID: "BUILD", Passed: true, Detail: "ok"}},
		nil, tree, nil)
	must(t, rep.Write(filepath.Join(f.Paths.EvalDir(), "report.json")))
	r.write("aisdlc/features/PAY-1/review-agent.json", validAgentReview)
	r.ok(noConsole(), "submit", "review")
	contains(t, r.out, "Đã nộp cổng REVIEW, không có phát hiện. Dev duyệt bằng `aisdlc approve review`")

	// code đổi sau eval: không duyệt được
	r.write("internal/x.go", "package x\n")
	eq(t, r.runC(typed("PAY-1"), "approve", "review"), 1)
	contains(t, r.err, "Không duyệt được cổng REVIEW: REVIEW_EVAL_STALE")
	must(t, os.Remove(filepath.Join(r.root, "internal/x.go")))
	r.ok(typed("PAY-1"), "approve", "review")
	contains(t, r.out, "Agent review: ", "Đã duyệt cổng REVIEW bởi Dev Một")
}

const validAgentReview = `{
  "summary": "Ổn",
  "criteria": [
    {"id": "sequence", "label": "DAT", "reason": "ok"},
    {"id": "error-handling", "label": "DAT", "reason": "ok"},
    {"id": "over-engineering", "label": "DAT", "reason": "ok"},
    {"id": "naming", "label": "DAT", "reason": "ok"},
    {"id": "security", "label": "DAT", "reason": "ok"}
  ],
  "issues": []
}
`

// feature dựng Feature PAY-1 như một lệnh.
func (r *testRepo) feature() *Feature {
	r.t.Helper()
	ctx := r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{})
	f, err := ctx.FeatureAt(r.root, "PAY-1")
	must(r.t, err)
	return f
}

// acceptAllSteps ghi steps.json với mọi bước ACCEPTED (nhanh hơn chạy step start/finish/accept từng bước).
func (r *testRepo) acceptAllSteps() {
	r.t.Helper()
	f := r.feature()
	var recs []step.StepRecord
	for _, s := range f.Specs() {
		recs = append(recs, step.StepRecord{ID: s.ID, Status: step.StepAccepted, AcceptedBy: "Dev Một"})
	}
	b, err := jsonMarshal(recs)
	must(r.t, err)
	r.write("aisdlc/features/PAY-1/steps.json", string(b))
}

func panicProbe(*Context) *cobra.Command {
	return &cobra.Command{Use: "panic-probe", RunE: func(*cobra.Command, []string) error { panic("hỏng bất ngờ") }}
}

func TestPanicPrintsLoiAndExit2(t *testing.T) {
	r := newTestRepo(t)
	r.extra = append(r.extra, panicProbe)
	eq(t, r.run("panic-probe"), ExitUsage)
	eq(t, r.err, "Lỗi: hỏng bất ngờ\n")
}

// Chế độ web mà máy chưa `aisdlc web login`: lệnh nhắc đăng nhập; nộp/duyệt/trả lại/mở lại dừng exit 2, không ghi
// gates.json, không chạy harness. Lệnh chỉ đọc vẫn chạy.
func TestWebModeWithoutLoginGateCommandsAbortWithoutWriting(t *testing.T) {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup", "--approvals", "web")
	r.ok(noConsole(), "init", "PAY-1")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	eq(t, r.run("submit", "brd"), 2)
	contains(t, r.err, "Chưa `aisdlc web login` trên máy này",
		"Không nộp được lên web: web chưa cấu hình; chạy `aisdlc web login`.")
	eq(t, r.runC(typed("PAY-1"), "approve", "brd"), 2)
	for _, args := range [][]string{{"reject", "brd", "-m", "x"}, {"reopen", "brd"}} {
		eq(t, r.runC(typed("PAY-1"), args...), 2)
		contains(t, r.err, "Chưa có token approve. Duyệt trên web: <url web>/f/")
	}
	eq(t, r.runner.calls == nil, true)
	eq(t, r.exists("aisdlc/features/PAY-1/gates.json"), false)
	r.ok(noConsole(), "review", "brd") // chỉ đọc: chạy được
}

// calledFrom: đang chạy trong hàm có tên kết thúc bằng fn.
func calledFrom(fn string) bool {
	pc := make([]uintptr, 64)
	frames := runtime.CallersFrames(pc[:runtime.Callers(2, pc)])
	for {
		f, more := frames.Next()
		if strings.HasSuffix(f.Function, fn) {
			return true
		}
		if !more {
			return false
		}
	}
}

// Như catch RuntimeException của Java: git hỏng khi kiểm eval cũ là phát hiện REVIEW_NO_EVAL (exit 1), không exit 2.
func TestSubmitReviewStaleEvalGitErrorIsFinding(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.acceptAllSteps()
	f := r.feature()
	rep := eval.NewEvalReport("PAY-1", testNow, "local", []eval.HardCheck{{ID: "BUILD", Passed: true, Detail: "ok"}},
		nil, "abc", nil)
	must(t, rep.Write(filepath.Join(f.Paths.EvalDir(), "report.json")))
	r.write("aisdlc/features/PAY-1/review-agent.json", validAgentReview)
	broken := filepath.Join(t.TempDir(), "không-phải-repo")
	r.git = func(root string) *gitx.Repo {
		if calledFrom("cli.staleEval") {
			return gitx.New(broken)
		}
		return gitx.New(root)
	}
	eq(t, r.run("submit", "review"), 1)
	contains(t, r.out, "REVIEW_NO_EVAL", "Không đọc được aisdlc/features/PAY-1/eval/report.json; chạy lại `aisdlc eval`: ")
	notContains(t, r.err, "Lỗi:")
	eq(t, r.state(gate.REVIEW), "SUBMITTED_WITH_FINDINGS")
}
