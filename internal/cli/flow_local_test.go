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
