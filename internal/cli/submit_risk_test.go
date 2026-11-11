package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/risk"
	"aisdlc/internal/run"
)

// SubmitRiskTest.

const ledgerSQL = "CREATE TABLE ledger_entry (id BIGINT, amount BIGINT);\n"

func bankingRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup", "--preset", "banking")
	return r
}

// brdApproved: tính năng FULL PAY-1, BRD đã duyệt, migration nháp chạm bảng sổ cái.
func (r *testRepo) brdApproved() {
	r.t.Helper()
	r.ok(noConsole(), "init", "PAY-1")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	r.ok(typed("PAY-1"), "approve", "brd")
	r.write("aisdlc/features/PAY-1/db/00100_ledger.sql", ledgerSQL)
}

func (r *testRepo) risk() risk.RiskFile {
	r.t.Helper()
	f, ok, err := risk.ReadRiskFile(filepath.Join(r.root, riskFile))
	must(r.t, err)
	if !ok {
		r.t.Fatal("chưa có risk.yaml")
	}
	return f
}

func ruleIDs(ms []risk.Match) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Rule)
	}
	return out
}

func reqIDs(rs []risk.Requirement) []string {
	var out []string
	for _, q := range rs {
		out = append(out, q.ID)
	}
	return out
}

func hasItem(t *testing.T, list []string, want string) {
	t.Helper()
	if !slices.Contains(list, want) {
		t.Fatalf("thiếu %q trong %v", want, list)
	}
}

// `submit brd` không còn đọc chính sách (không có mức tạm): chính sách hỏng không ảnh hưởng.
func TestSubmitBrdIgnoresBrokenPolicy(t *testing.T) {
	r := bankingRepo(t)
	r.ok(noConsole(), "init", "PAY-1")
	r.write(policyFile, "levels: [low]\n")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	notContains(t, r.out+r.err, "Thang mức", "Rủi ro")
}

func TestSubmitDesignMissingImpactBlocked(t *testing.T) {
	r := bankingRepo(t)
	r.brdApproved()
	y := r.read(designFile)
	r.write(designFile, y[:strings.Index(y, "impact:")])
	eq(t, r.run("submit", "design"), 1)
	contains(t, r.out, "IMPACT_MISSING")
	eq(t, r.exists(riskFile), false)
}

func TestSubmitDesignBrokenPolicyExit2WithRuleID(t *testing.T) {
	r := bankingRepo(t)
	r.brdApproved()
	r.write(policyFile, strings.Replace(r.read(policyFile), `account_balance)", level: high`,
		`account_balance)", level: extreme`, 1))
	eq(t, r.run("submit", "design"), 2)
	contains(t, r.err, "R4", "extreme")
	eq(t, r.exists(riskFile), false)
}

func TestSubmitCasesMissingIdempotentBlocked(t *testing.T) {
	r := bankingRepo(t)
	r.brdApproved()
	r.llmLevel = "high"
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "design")
	r.runner.cases = failingCases
	eq(t, r.run("submit", "cases"), 1)
	contains(t, r.out, "RISK_REQ", "gửi trùng")
	for _, c := range r.runner.calls {
		if strings.HasPrefix(c, "cases-with-drafts") {
			t.Fatalf("không được chạy case: %v", r.runner.calls)
		}
	}
}

func TestSubmitCasesLiteClassifies(t *testing.T) {
	r := bankingRepo(t)
	r.ok(noConsole(), "init", "PAY-1", "--lite")
	r.llmLevel = "high" // subagent: G1 (dòng tiền)
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomeFailed)}
	}
	r.run("submit", "cases")
	f := r.risk()
	eq(t, f.Level, "high")
	eqList(t, ruleIDs(f.Matched), "G1")
	contains(t, r.out, "RISK_REQ")
}

func TestSubmitCasesLiteInvariantsInCasesYamlSatisfyRequirement(t *testing.T) {
	r := bankingRepo(t)
	r.ok(noConsole(), "init", "PAY-1", "--lite")
	r.llmLevel = "high"
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomeFailed)}
	}
	r.run("submit", "cases")
	hasItem(t, reqIDs(r.risk().Requirements), "invariant")
	contains(t, r.out, "cases.yaml có 0/1 invariants")
	r.write("aisdlc/features/PAY-1/cases.yaml", strings.Replace(r.read("aisdlc/features/PAY-1/cases.yaml"),
		"invariants: []", "invariants:\n  - { name: a, sql: \"SELECT 1 WHERE 1=0\" }", 1))
	r.run("submit", "cases")
	notContains(t, r.out, "CASES_YAML_INVALID", "0/1 invariants", "Bất biến DB")
}

func TestNoRiskFileLegacyFeatureCasesUnchanged(t *testing.T) {
	r := bankingRepo(t)
	r.brdApproved()
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "design")
	// như tính năng qua cổng Thiết kế trước khi có chính sách rủi ro: không có dấu riskSchema, design.yaml không có
	// impact, không risk.yaml
	meta := "aisdlc/features/PAY-1/feature.yaml"
	r.write(meta, strings.Replace(r.read(meta), "riskSchema: 2\n", "", 1))
	y := r.read(designFile)
	r.write(designFile, y[:strings.Index(y, "impact:")])
	must(t, os.Remove(filepath.Join(r.root, riskFile)))
	cfg, err := model.LoadRepoConfig(r.root)
	must(t, err)
	paths := model.FeaturePaths{RepoRoot: r.root, Key: "PAY-1"}
	gates := gate.NewService(paths, model.FlowFull, cfg, gate.NewLocalGateStore(paths), func() time.Time { return testNow })
	_, err = gates.Submit(gate.DESIGN, nil) // duyệt lại bản design.yaml cũ (không impact)
	must(t, err)
	_, err = gates.Approve(gate.DESIGN, "Dev Một", "")
	must(t, err)
	r.runner.cases = failingCases
	if code := r.run("submit", "cases"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	eq(t, r.exists(riskFile), false)
	notContains(t, r.out, "RISK_REQ")
}

func TestSubmitCasesDesignHasImpactButNoRiskFileClassifiesAndChecks(t *testing.T) {
	r := bankingRepo(t)
	r.brdApproved()
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "design")
	must(t, os.Remove(filepath.Join(r.root, riskFile))) // chưa có risk.yaml: xếp loại từ risk-agent.json
	r.llmLevel = "high"
	r.runner.cases = failingCases
	eq(t, r.run("submit", "cases"), 1)
	eq(t, r.risk().Level, "high")
	contains(t, r.out, "RISK_REQ", "gửi trùng")
}

// Chính sách đổi sau khi dev đổi mức tay: kết quả xếp loại đã cũ, cổng Case chặn (RISK_STALE); mức đổi tay giữ nguyên
// cho tới khi dev chạy `aisdlc risk reclassify`.
func TestSubmitCasesPolicyChangedAfterOverrideIsStale(t *testing.T) {
	r := bankingRepo(t)
	r.brdApproved()
	r.llmLevel = "high"
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "design")
	r.ok(typed("PAY-1"), "risk", "set", "low", "--reason", "Chỉ đọc sổ cái")
	r.write(policyFile, strings.Replace(r.read(policyFile), "min: 90", "min: 95", 1))
	r.runner.cases = failingCases
	eq(t, r.run("submit", "cases"), 1)
	contains(t, r.out, "RISK_STALE", "aisdlc risk reclassify")
	eq(t, r.risk().Level, "low")
	if r.risk().Override == nil {
		t.Fatal("override phải còn")
	}
}
