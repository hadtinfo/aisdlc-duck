package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/risk"
	"aisdlc/internal/run"
)

// FlowLocalTest: quy trình theo mức rủi ro ở chế độ local (spec risk-flow §5); mẫu generic có flow.low toàn auto.

const (
	designFile = "aisdlc/features/PAY-1/design.yaml"
	policyFile = "aisdlc/risk-policy.yaml"
	riskFile   = "aisdlc/features/PAY-1/risk.yaml"
)

func failingCases([]string) []run.CaseResult {
	return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomeFailed), caseResult("C2", "S2", run.CaseOutcomeFailed)}
}

func flowRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.env["HOME"] = t.TempDir()
	r.presetFlow = true
	r.ok(noConsole(), "setup") // generic: flow.low toàn auto
	r.ok(noConsole(), "init", "PAY-1")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.runner.cases = failingCases
	return r
}

// approval là bản duyệt đang lưu trong gates.json của PAY-1.
func (r *testRepo) approval(g gate.Gate) (gate.Approval, bool) {
	r.t.Helper()
	a, ok, err := gate.NewLocalGateStore(model.FeaturePaths{RepoRoot: r.root, Key: "PAY-1"}).Approval(g)
	must(r.t, err)
	return a, ok
}

type statusJSON struct {
	Gates map[string]string `json:"gates"`
	Steps map[string]string `json:"steps"`
}

func (r *testRepo) status() statusJSON {
	r.t.Helper()
	r.ok(noConsole(), "status", "--json")
	var s statusJSON
	must(r.t, json.Unmarshal([]byte(r.out), &s))
	return s
}

func (r *testRepo) state(g gate.Gate) string {
	r.t.Helper()
	return r.status().Gates[string(g)]
}

func (r *testRepo) assertAuto(g gate.Gate, level string) {
	r.t.Helper()
	a, ok := r.approval(g)
	if !ok {
		r.t.Fatalf("chưa duyệt %s", g)
	}
	eq(r.t, a.Approver, "auto")
	eq(r.t, a.Rule, "flow:"+level)
	eq(r.t, a.Superseded, false)
}

func (r *testRepo) superseded(g gate.Gate) bool {
	r.t.Helper()
	a, ok := r.approval(g)
	if !ok {
		r.t.Fatalf("chưa duyệt %s", g)
	}
	return a.Superseded
}

func (r *testRepo) noApproval(g gate.Gate) {
	r.t.Helper()
	if a, ok := r.approval(g); ok {
		r.t.Fatalf("%s không được có bản duyệt: %+v", g, a)
	}
}

// lowThroughCases: BRD, Thiết kế, Case đều tự duyệt ở mức low.
func (r *testRepo) lowThroughCases() {
	r.t.Helper()
	r.ok(noConsole(), "submit", "brd")
	contains(r.t, r.out, "Đã nộp cổng BRD, không có phát hiện: tự duyệt (mức low).")
	r.ok(noConsole(), "submit", "design")
	contains(r.t, r.out, "tự duyệt (mức low)")
	r.ok(noConsole(), "submit", "cases")
	contains(r.t, r.out, "tự duyệt (mức low)")
}

func (r *testRepo) setDesignImpact(impact string) {
	r.t.Helper()
	y := regexp.MustCompile(`(?m)^impact:.*$`).ReplaceAllLiteralString(r.read(designFile), "impact: "+impact)
	r.write(designFile, y)
}

func matches(t *testing.T, s, pattern string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(s) {
		t.Fatalf("không khớp %q:\n%s", pattern, s)
	}
}

// Phần bước (step start/finish tự accept, Review vẫn chờ dev) ở task 6.
func TestLowFeatureBrdDesignCasesAutoApprovedReviewManual(t *testing.T) {
	r := flowRepo(t)
	r.lowThroughCases()
	r.assertAuto(gate.BRD, "low")
	r.assertAuto(gate.DESIGN, "low")
	r.assertAuto(gate.CASES, "low")
	s := r.status()
	eq(t, s.Gates["BRD"], "APPROVED")
	eq(t, s.Gates["DESIGN"], "APPROVED")
	eq(t, s.Gates["CASES"], "APPROVED")
	eq(t, s.Gates["REVIEW"], "NOT_SUBMITTED")

	r.ok(noConsole(), "status")
	matches(t, r.out, `BRD +APPROVED +tự duyệt \(mức low\)`)
	contains(t, r.out, "Cổng kế tiếp cần xử lý: REVIEW")
	r.ok(noConsole(), "review", "design")
	contains(t, r.out, "Duyệt: tự duyệt (mức low)")
	eq(t, r.runC(typed("PAY-1"), "approve", "cases"), 2)
	contains(t, r.err, "APPROVED (tự duyệt (mức low))")
	r.noApproval(gate.REVIEW)
}

func TestHighFlagNoAuto(t *testing.T) {
	r := flowRepo(t)
	r.ok(noConsole(), "submit", "brd")
	r.assertAuto(gate.BRD, "low")
	// S2 chồng phạm vi với S1: review card có cờ SCOPE_OVERLAP mức HIGH
	d := r.read(designFile)
	r.write(designFile, regexp.MustCompile(regexp.QuoteMeta(`scope: ["src/main/java/**/limit/service/**"`)).
		ReplaceAllLiteralString(d, `scope: ["src/main/java/**/limit/web/**", "src/main/java/**/limit/service/**"`))
	r.ok(noConsole(), "review", "design", "--json")
	contains(t, r.out, `"SCOPE_OVERLAP"`, `"HIGH"`)

	r.ok(noConsole(), "submit", "design")
	contains(t, r.out, "Không tự duyệt cổng DESIGN: Review card có cờ đỏ mức HIGH.",
		"Dev duyệt bằng `aisdlc approve design`")
	r.noApproval(gate.DESIGN)
	eq(t, r.state(gate.DESIGN), "AWAITING_APPROVAL")
}

func TestNoPolicyFileFallbackHasNoFlow(t *testing.T) {
	r := flowRepo(t)
	must(t, os.Remove(filepath.Join(r.root, policyFile)))
	ctx := r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{})
	l, err := LoadPolicy(ctx, r.root, false)
	must(t, err)
	eq(t, len(l.Policy.Flow), 0)
	r.ok(noConsole(), "submit", "brd")
	notContains(t, r.out, "tự duyệt")
	contains(t, r.out, "Dev duyệt bằng `aisdlc approve brd`")
	r.noApproval(gate.BRD)
	r.ok(typed("PAY-1"), "approve", "brd")
	r.ok(noConsole(), "submit", "design")
	notContains(t, r.out, "tự duyệt")
	rf, ok, err := risk.ReadRiskFile(filepath.Join(r.root, riskFile))
	must(t, err)
	eq(t, ok, true)
	eq(t, rf.Level, "low")
	eq(t, len(rf.Flow), 0)
	r.noApproval(gate.DESIGN)
}

// submitDesignRaiseFlow: BRD tự duyệt ở low, rồi `submit design` nâng lên high: bản tự duyệt BRD bị thay thế.
func (r *testRepo) submitDesignRaiseFlow() {
	r.t.Helper()
	r.ok(noConsole(), "submit", "brd")
	r.assertAuto(gate.BRD, "low")
	r.setDesignImpact(`{ level: high, labels: [money-movement], reason: "Chuyển tiền" }`)
	r.ok(noConsole(), "submit", "design")
	contains(r.t, r.out, "Mức tăng low → high: cổng BRD cần dev duyệt lại.")
	eq(r.t, r.superseded(gate.BRD), true)
}

func TestSubmitDesignRaiseSupersedesBrd(t *testing.T) {
	r := flowRepo(t)
	r.ok(noConsole(), "submit", "brd")
	r.assertAuto(gate.BRD, "low")
	r.setDesignImpact(`{ level: high, labels: [money-movement], reason: "Chuyển tiền" }`)
	r.ok(noConsole(), "submit", "design")
	contains(t, r.out, "Rủi ro: high", "Mức tăng low → high: cổng BRD cần dev duyệt lại.")
	notContains(t, r.out, "Đã nộp cổng DESIGN, không có phát hiện: tự duyệt")
	eq(t, r.superseded(gate.BRD), true)
	r.noApproval(gate.DESIGN)
	eq(t, r.state(gate.BRD), "AWAITING_APPROVAL")
	eq(t, r.state(gate.DESIGN), "LOCKED_BY_PREVIOUS")
	r.ok(typed("PAY-1"), "approve", "brd")
	eq(t, r.state(gate.DESIGN), "AWAITING_APPROVAL") // bản nộp thiết kế vẫn còn
}

func TestResubmitBrdAfterRaiseStaysManual(t *testing.T) {
	r := flowRepo(t)
	r.submitDesignRaiseFlow()
	r.ok(noConsole(), "submit", "brd") // agent nộp lại BRD không đổi
	notContains(t, r.out, "tự duyệt (mức")
	contains(t, r.out, "Dev duyệt bằng `aisdlc approve brd`")
	eq(t, r.superseded(gate.BRD), true)
	eq(t, r.state(gate.BRD), "AWAITING_APPROVAL")
}

// raisingPolicy: chính sách thêm rule từ khóa khớp BRD, tính lại thành medium (flow duyệt tay, case mẫu đủ yêu cầu).
func raisingPolicy(p string) string {
	return regexp.MustCompile(`(?m)^rules:\n`).ReplaceAllLiteralString(p,
		"rules:\n  - { id: X1, kind: keyword, match: \"hạn mức\", level: medium, note: Hạn mức }\n")
}

func TestCasesRaiseThenHarnessThrowsNextSubmitSupersedes(t *testing.T) {
	r := flowRepo(t)
	r.ok(noConsole(), "submit", "brd")
	r.ok(noConsole(), "submit", "design")
	r.assertAuto(gate.DESIGN, "low")
	r.write(policyFile, raisingPolicy(r.read(policyFile)))
	r.runner.err = errors.New("harness chết giữa chừng")
	if r.run("submit", "cases") == 0 {
		t.Fatal("phải lỗi")
	}
	// risk.yaml đã ghi mức mới trước khi chạy case
	contains(t, r.err, "harness chết giữa chừng")
	rf, _, err := risk.ReadRiskFile(filepath.Join(r.root, riskFile))
	must(t, err)
	eq(t, rf.Level, "medium")
	eq(t, r.superseded(gate.BRD), false) // chưa thay thế

	r.runner.err = nil
	r.runner.cases = failingCases
	calls := len(r.runner.calls)
	eq(t, r.run("submit", "cases"), 2)
	contains(t, r.out, "Mức tăng low → medium: cổng BRD, Thiết kế cần dev duyệt lại.")
	contains(t, r.err, "Cổng CASES bị khóa: cổng trước chưa được duyệt.")
	eq(t, len(r.runner.calls), calls) // dừng trước khi chạy harness
	eq(t, r.superseded(gate.BRD), true)
	eq(t, r.superseded(gate.DESIGN), true)
	eq(t, r.state(gate.BRD), "AWAITING_APPROVAL")

	// dev duyệt tay lại; agent nộp Case: mức medium duyệt tay, không in lại thông điệp
	r.ok(typed("PAY-1"), "approve", "brd")
	r.ok(typed("PAY-1"), "approve", "design")
	r.ok(noConsole(), "submit", "cases")
	notContains(t, r.out, "cần dev duyệt lại", "tự duyệt (mức")
	contains(t, r.out, "Dev duyệt bằng `aisdlc approve cases`")
	r.noApproval(gate.CASES)
}

func TestApproverNamedAutoRejected(t *testing.T) {
	r := flowRepo(t)
	r.write(policyFile, regexp.MustCompile(regexp.QuoteMeta("low:    { brd: auto,")).
		ReplaceAllLiteralString(r.read(policyFile), "low:    { brd: manual,"))
	r.ok(noConsole(), "submit", "brd")
	r.noApproval(gate.BRD)
	for _, name := range []string{"auto", "AUTO", " Auto "} {
		r.gitCmd("config", "user.name", name)
		eq(t, r.runC(typed("PAY-1"), "approve", "brd"), 2)
		contains(t, r.err, "dành cho bản tự duyệt")
		r.noApproval(gate.BRD)
	}
	r.gitCmd("config", "user.name", "Dev Một")
	r.ok(typed("PAY-1"), "approve", "brd")
	a, _ := r.approval(gate.BRD)
	eq(t, a.Approver, "Dev Một")
}

// passing: các case ids đều pass.
func passing(ids ...string) func([]string) []run.CaseResult {
	return func([]string) []run.CaseResult {
		var out []run.CaseResult
		for _, id := range ids {
			out = append(out, caseResult(id, "S"+id[1:], run.CaseOutcomePassed)) // C1 thuộc S1, C2 thuộc S2
		}
		return out
	}
}

// Phần bước của lowFeature_brdDesignCasesAutoApproved_reviewManual: bước sạch tự accept; Review vẫn chờ dev.
func TestLowFeatureStepsAutoAcceptedReviewManual(t *testing.T) {
	r := flowRepo(t)
	r.lowThroughCases()
	for _, id := range []string{"S1", "S2"} {
		r.ok(noConsole(), "step", "start", id)
		file := s1File
		if id == "S2" {
			file = "src/main/java/com/example/limit/service/LimitService.java"
		}
		r.write(file, "class X"+id+" {}\n")
		r.runner.cases = passing("C1", "C2")
		r.ok(noConsole(), "step", "finish", id)
	}
	s := r.status()
	eq(t, s.Steps["S1"], "ACCEPTED")
	eq(t, s.Steps["S2"], "ACCEPTED")
	eq(t, s.Gates["REVIEW"], "NOT_SUBMITTED")
	r.noApproval(gate.REVIEW)
}

func TestStepAutoAcceptWhenClean(t *testing.T) {
	r := flowRepo(t)
	r.lowThroughCases()
	r.ok(noConsole(), "step", "start", "S1")
	r.write(s1File, "class LimitController {}\n")
	r.runner.cases = passing("C1")
	r.ok(noConsole(), "step", "finish", "S1")
	contains(t, r.out, "Đã chấp nhận bước S1: tự duyệt (mức low).", "Bước kế: `aisdlc step start S2`.")
	notContains(t, r.out, "Dừng lại.")
	var recs []struct {
		Status     string `json:"status"`
		AcceptedBy string `json:"acceptedBy"`
	}
	must(t, json.Unmarshal([]byte(r.read("aisdlc/features/PAY-1/steps.json")), &recs))
	eq(t, recs[0].Status, "ACCEPTED")
	eq(t, recs[0].AcceptedBy, "auto")
	// đã tự duyệt: step accept chỉ báo lại
	r.ok(noConsole(), "step", "accept", "S1")
	contains(t, r.out, "Bước S1 đã được tự duyệt theo mức rủi ro.")
}

func TestStepWithAllowScopeNeedsDev(t *testing.T) {
	r := flowRepo(t)
	r.lowThroughCases()
	r.ok(noConsole(), "step", "start", "S1")
	r.write(s1File, "class LimitController {}\n")
	r.write("src/main/java/com/example/Shared.java", "class Shared {}\n") // ngoài phạm vi S1
	r.runner.cases = passing("C1")
	eq(t, r.run("step", "finish", "S1"), 1)
	r.ok(typed("PAY-1"), "scope", "allow", "src/main/java/com/example/Shared.java")
	r.ok(noConsole(), "step", "start", "S1")
	r.ok(noConsole(), "step", "finish", "S1")
	contains(t, r.out, "Dừng lại. Dev xem rồi chạy `aisdlc step accept S1`")
	notContains(t, r.out, "tự duyệt")
	eq(t, r.status().Steps["S1"], "AWAITING_DEV")
}

func TestStepCasesFailNoAutoAccept(t *testing.T) {
	r := flowRepo(t)
	r.lowThroughCases()
	eq(t, r.risk().Flow["step"], "auto")
	r.ok(noConsole(), "step", "start", "S1")
	r.write(s1File, "class LimitController {}\n")
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomeFailed)}
	}
	eq(t, r.run("step", "finish", "S1"), 1) // case chưa qua: không tự accept
	contains(t, r.out, "  - C1 FAILED: mong đợi 200, thực tế 404")
	eq(t, r.status().Steps["S1"], "AWAITING_DEV")
}

func TestReclassifyRaiseSupersedesAllAutoGates(t *testing.T) {
	r := flowRepo(t)
	r.lowThroughCases()
	// chính sách thêm rule từ khóa khớp BRD: tính lại thành high
	r.write(policyFile, regexp.MustCompile(`(?m)^rules:\n`).ReplaceAllLiteralString(r.read(policyFile),
		"rules:\n  - { id: X1, kind: keyword, match: \"hạn mức\", level: high, note: Hạn mức }\n"))
	r.ok(typed("PAY-1"), "risk", "reclassify")
	contains(t, r.out, "Mức tăng low → high: cổng BRD, Thiết kế cần dev duyệt lại.", "Cổng Case đã mở lại")
	eq(t, r.superseded(gate.BRD), true)
	eq(t, r.superseded(gate.DESIGN), true)
	r.noApproval(gate.CASES)
	eq(t, r.state(gate.BRD), "AWAITING_APPROVAL")
	eq(t, r.state(gate.DESIGN), "LOCKED_BY_PREVIOUS")

	r.ok(noConsole(), "status")
	matches(t, r.out, `BRD +AWAITING_APPROVAL +chờ dev duyệt \(bản tự duyệt \(mức low\) đã bị thay thế khi nâng mức\)`)
	// agent không đi tiếp được: chưa được code
	if r.run("step", "start", "S1") == 0 {
		t.Fatal("step start phải lỗi")
	}

	// dev duyệt tay lại, không cần nộp lại
	r.ok(typed("PAY-1"), "approve", "brd")
	a, _ := r.approval(gate.BRD)
	eq(t, a.Approver, "Dev Một")
	eq(t, r.state(gate.DESIGN), "AWAITING_APPROVAL")
	r.ok(typed("PAY-1"), "approve", "design")
	a, _ = r.approval(gate.DESIGN)
	eq(t, a.Approver, "Dev Một")
}

func TestFlowEditSameLevelSaysFlowChangedNotRaise(t *testing.T) {
	r := flowRepo(t)
	r.lowThroughCases()
	// cùng mức low nhưng chính sách đổi BRD sang duyệt tay
	r.write(policyFile, regexp.MustCompile(regexp.QuoteMeta("low:    { brd: auto,")).
		ReplaceAllLiteralString(r.read(policyFile), "low:    { brd: manual,"))
	r.ok(typed("PAY-1"), "risk", "reclassify")
	contains(t, r.out, "Flow của mức low đổi: cổng BRD cần dev duyệt lại.")
	notContains(t, r.out, "Mức tăng")
	eq(t, r.superseded(gate.BRD), true)
	eq(t, r.superseded(gate.DESIGN), false)
}

func TestResubmitBrdAfterRiskSetHighStaysManual(t *testing.T) {
	r := flowRepo(t)
	r.ok(noConsole(), "submit", "brd")
	r.ok(noConsole(), "submit", "design")
	r.assertAuto(gate.DESIGN, "low")
	r.ok(typed("PAY-1"), "risk", "set", "high", "--reason", "Chuyển tiền thật")
	eq(t, r.superseded(gate.BRD), true)
	r.ok(noConsole(), "submit", "brd")
	notContains(t, r.out, "tự duyệt (mức")
	eq(t, r.superseded(gate.BRD), true)
	eq(t, r.state(gate.BRD), "AWAITING_APPROVAL")
}

func TestStepFinishSupersedesStaleAutoGates(t *testing.T) {
	r := flowRepo(t)
	r.lowThroughCases()
	r.ok(noConsole(), "step", "start", "S1")
	// risk.yaml ghi mức high mà chưa thay thế (lệnh trước hỏng giữa chừng)
	p, err := risk.ParsePolicy(r.read(policyFile))
	must(t, err)
	must(t, r.risk().WithOverride(p, "high", "Dev Một", "r", testNow).Write(filepath.Join(r.root, riskFile)))
	r.write(s1File, "class LimitController {}\n")
	r.runner.cases = passing("C1")
	r.ok(noConsole(), "step", "finish", "S1")
	contains(t, r.out, "Mức tăng low → high: cổng BRD, Thiết kế, Case cần dev duyệt lại.")
	notContains(t, r.out, "Đã chấp nhận bước S1")
	eq(t, r.superseded(gate.CASES), true)
}

func TestLowerNeverTurnsManualIntoAuto(t *testing.T) {
	r := flowRepo(t)
	r.ok(noConsole(), "submit", "brd")
	r.setDesignImpact(`{ level: high, labels: [money-movement], reason: "Chuyển tiền" }`)
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "brd")
	r.ok(typed("PAY-1"), "approve", "design")
	brd, _ := r.approval(gate.BRD)
	design, _ := r.approval(gate.DESIGN)
	eq(t, brd.Approver, "Dev Một")

	r.ok(typed("PAY-1"), "risk", "set", "low", "--reason", "Không chuyển tiền thật")
	notContains(t, r.out, "Mức tăng")
	a, _ := r.approval(gate.BRD)
	eq(t, a, brd)
	a, _ = r.approval(gate.DESIGN)
	eq(t, a, design)
	eq(t, r.state(gate.BRD), "APPROVED")
	eq(t, r.state(gate.DESIGN), "APPROVED")
}
