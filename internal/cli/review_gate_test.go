package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"aisdlc/internal/eval"
	"aisdlc/internal/run"
)

// EvalCommandTest, phần cổng REVIEW không cần chạy eval thật: thay `aisdlc eval` bằng eval/report.json và report.md
// viết tay (handEval). Các test chạy `aisdlc eval` thật (evalWritesReportAndExitsByResult, ci*,
// localEvalNeedsActiveFeature) ở eval_cmd_test.go.

// evalReviewCriteria như CRITERIA của Java: 4 ĐẠT, security CẦN_SỬA.
const evalReviewCriteria = `[{"id":"sequence","label":"DAT","reason":"đúng"},` +
	`{"id":"error-handling","label":"DAT","reason":"đủ"},` +
	`{"id":"over-engineering","label":"DAT","reason":"gọn"},` +
	`{"id":"naming","label":"DAT","reason":"rõ"},` +
	`{"id":"security","label":"CAN_SUA","reason":"thiếu kiểm tra quyền"}]`

const evalValidReview = `{"summary":"ổn","criteria":` + evalReviewCriteria + `,"issues":[]}`

// evalFx như setUp của EvalCommandTest: S1 chờ dev chấp nhận.
func evalFx(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.s1AwaitingDev()
	return r
}

func (r *testRepo) allCasesPass() {
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomePassed),
			caseResult("C2", "S2", run.CaseOutcomePassed)}
	}
}

// allStepsAccepted: S1 được chấp nhận, S2 làm xong và được chấp nhận.
func (r *testRepo) allStepsAccepted() {
	r.t.Helper()
	r.ok(typed("PAY-1"), "step", "accept", "S1")
	r.ok(noConsole(), "step", "start", "S2")
	r.write("src/main/java/com/example/limit/service/LimitService.java", "class LimitService {}\n")
	r.allCasesPass()
	r.ok(noConsole(), "step", "finish", "S2")
	r.ok(typed("PAY-1"), "step", "accept", "S2")
}

// handEval thay `aisdlc eval`: ghi eval/report.json và report.md cho working tree hiện tại (trừ thư mục tính năng),
// với CASES_ALL_PASS đạt hoặc không và AGENT_REVIEW theo review-agent.json lúc này.
func (r *testRepo) handEval(passed bool) {
	r.t.Helper()
	f := r.feature()
	tree, err := f.Git().SnapshotTreeExcluding(f.ctx.Std(), f.Paths.Rel(f.Paths.Dir()))
	must(r.t, err)
	detail := "2/2 case đạt"
	if !passed {
		detail = "1/2 case đạt"
	}
	rep := eval.NewEvalReport("PAY-1", testNow, "local", []eval.HardCheck{{ID: "BUILD", Passed: true, Detail: "ok"},
		{ID: "CASES_ALL_PASS", Passed: passed, Detail: detail}},
		[]eval.SoftCheck{eval.AgentReviewCheck(f.Paths.ReviewAgent())}, tree, nil)
	must(r.t, rep.Write(filepath.Join(f.Paths.EvalDir(), "report.json")))
	r.write("aisdlc/features/PAY-1/eval/report.md", eval.RenderReport(rep, nil))
}

func TestSubmitReviewNeedsPassingEvalAndValidAgentReview(t *testing.T) {
	r := evalFx(t)
	r.allStepsAccepted()
	eq(t, r.run("submit", "review"), 1)
	contains(t, r.out, "REVIEW_NO_EVAL", "REVIEW_AGENT_MISSING")

	r.handEval(false)
	r.write("aisdlc/features/PAY-1/review-agent.json", evalValidReview)
	eq(t, r.run("submit", "review"), 1)
	contains(t, r.out, "REVIEW_EVAL_FAILED", "CASES_ALL_PASS")
	notContains(t, r.out, "REVIEW_AGENT")

	r.handEval(true)
	r.write("aisdlc/features/PAY-1/review-agent.json", strings.Replace(evalValidReview, `"CAN_SUA"`, `"TOT"`, 1))
	eq(t, r.run("submit", "review"), 1)
	contains(t, r.out, "REVIEW_AGENT_INVALID", "TOT")
	notContains(t, r.out, "REVIEW_EVAL")

	r.write("aisdlc/features/PAY-1/review-agent.json", evalValidReview)
	if c := r.run("submit", "review"); c != 0 {
		t.Fatalf("exit %d: %s%s", c, r.out, r.err)
	}
	eq(t, r.run("status", "--json"), 0)
	contains(t, r.out, `"REVIEW":"AWAITING_APPROVAL"`)
}

func TestSubmitReviewAfterCodeChangeIsStale(t *testing.T) {
	r := evalFx(t)
	r.allCasesPass()
	r.write("aisdlc/features/PAY-1/review-agent.json", evalValidReview)
	r.handEval(true)
	r.write(s1File, "class LimitController { int x; }\n")

	eq(t, r.run("submit", "review"), 1)
	contains(t, r.out, "REVIEW_EVAL_STALE", "code đã đổi sau lần eval — chạy lại aisdlc eval")
}

func TestApproveReviewRefusesWhenCodeChangedAfterSubmit(t *testing.T) {
	r := evalFx(t)
	r.allStepsAccepted()
	original := r.read(s1File)
	r.write("aisdlc/features/PAY-1/review-agent.json", evalValidReview)
	r.handEval(true)
	r.ok(noConsole(), "submit", "review")
	gatesBefore := r.read("aisdlc/features/PAY-1/gates.json")

	r.write(s1File, "class LimitController { int changedAfterSubmit; }\n")
	eq(t, r.runC(typed("PAY-1"), "approve", "review"), 1)
	contains(t, r.out+r.err, "REVIEW_EVAL_STALE", "code đã đổi sau lần eval — chạy lại aisdlc eval")
	eq(t, r.read("aisdlc/features/PAY-1/gates.json"), gatesBefore)

	r.write(s1File, original)
	r.ok(typed("PAY-1"), "approve", "review")
	contains(t, r.read("aisdlc/features/PAY-1/gates.json"), `"REVIEW"`)
}

func TestSubmitReviewRequiresEveryStepAccepted(t *testing.T) {
	r := evalFx(t)
	r.allCasesPass()
	r.write("aisdlc/features/PAY-1/review-agent.json", evalValidReview)
	r.handEval(true)
	eq(t, r.run("submit", "review"), 1)
	contains(t, r.out, "REVIEW_STEPS_NOT_ACCEPTED", "S1 (AWAITING_DEV)", "S2 (NOT_STARTED)")

	r.allStepsAccepted()
	r.handEval(true)
	r.ok(noConsole(), "submit", "review")
}

func TestApproveReviewRequiresEveryStepAccepted(t *testing.T) {
	r := evalFx(t)
	r.allStepsAccepted()
	r.write("aisdlc/features/PAY-1/review-agent.json", evalValidReview)
	r.handEval(true)
	r.ok(noConsole(), "submit", "review")
	// bước S2 bị mở lại sau khi nộp (steps.json không thuộc hash của REVIEW)
	var recs []map[string]any
	must(t, json.Unmarshal([]byte(r.read("aisdlc/features/PAY-1/steps.json")), &recs))
	for _, rec := range recs {
		if rec["id"] == "S2" {
			rec["status"] = "IN_PROGRESS"
		}
	}
	b, err := json.Marshal(recs)
	must(t, err)
	r.write("aisdlc/features/PAY-1/steps.json", string(b))
	gatesBefore := r.read("aisdlc/features/PAY-1/gates.json")

	eq(t, r.runC(typed("PAY-1"), "approve", "review"), 1)
	contains(t, r.out+r.err, "REVIEW_STEPS_NOT_ACCEPTED", "S2 (IN_PROGRESS)")
	eq(t, r.read("aisdlc/features/PAY-1/gates.json"), gatesBefore)
}

func TestSubmitReviewPutsAgentReviewIntoReportAndApproveShowsIt(t *testing.T) {
	r := evalFx(t)
	r.allStepsAccepted()
	r.handEval(true)
	contains(t, r.read("aisdlc/features/PAY-1/eval/report.md"), "| AGENT_REVIEW | NA |")
	calls := len(r.runner.calls)
	issue := `{"step":"S2","file":"src/main/java/com/example/limit/service/LimitService.java",` +
		`"line":1,"severity":"high","category":"security","message":"lộ số tài khoản"}`
	r.write("aisdlc/features/PAY-1/review-agent.json",
		strings.Replace(evalValidReview, `"issues":[]`, `"issues":[`+issue+`]`, 1))

	r.ok(noConsole(), "submit", "review")
	eq(t, len(r.runner.calls), calls)
	contains(t, r.read("aisdlc/features/PAY-1/eval/report.md"),
		"| AGENT_REVIEW | WARN | 4/5 ĐẠT, 1 CẦN_SỬA (security); 1 high, 0 medium |")
	contains(t, r.read("aisdlc/features/PAY-1/eval/report.json"), "1 CẦN_SỬA (security)")

	r.ok(typed("PAY-1"), "approve", "review")
	contains(t, r.out, "Agent review: 4/5 ĐẠT, 1 CẦN_SỬA (security); 1 high, 0 medium",
		"high S2 src/main/java/com/example/limit/service/LimitService.java:1: lộ số tài khoản")
}
