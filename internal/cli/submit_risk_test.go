package cli

import (
	"os"
	"path/filepath"
	"regexp"
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

func replaceImpactLine(s, impact string) string {
	return regexp.MustCompile(`(?m)^impact:.*$`).ReplaceAllLiteralString(s, impact)
}

func TestSubmitBrdPrintsKeywordPreviewWithoutWriting(t *testing.T) {
	r := bankingRepo(t)
	r.ok(noConsole(), "init", "PAY-1")
	r.write("aisdlc/features/PAY-1/brd.md", strings.Replace(validBRD, "Giới hạn chuyển tiền theo ngày.",
		"Miễn phí chuyển tiền theo ngày.", 1))
	r.ok(noConsole(), "submit", "brd")
	contains(t, r.out, "Rủi ro tạm (từ khóa): high", "R2")
	eq(t, r.exists(riskFile), false)
	r.ok(noConsole(), "review", "brd", "--json")
	contains(t, r.out, `"riskPreview"`, `"R2"`)
}

func TestSubmitBrdBrokenPolicyOnlyWarns(t *testing.T) {
	r := bankingRepo(t)
	r.ok(noConsole(), "init", "PAY-1")
	r.write(policyFile, "levels: [low]\n")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	contains(t, r.err, "Thang mức")
	r.ok(noConsole(), "review", "brd", "--json")
	notContains(t, r.out, "riskPreview")
}

func TestSubmitDesignWritesRiskYaml(t *testing.T) {
	r := bankingRepo(t)
	r.brdApproved()
	if code := r.run("submit", "design"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	f := r.risk()
	eq(t, f.Level, "high")
	hasItem(t, ruleIDs(f.Matched), "R4")
	var on []string
	for _, m := range f.Matched {
		if m.Rule == "R4" {
			on = append(on, m.On)
		}
	}
	eqList(t, on, "bảng ledger_entry")
	if f.Agent == nil {
		t.Fatal("agent phải có")
	}
	hasItem(t, reqIDs(f.Requirements), "idempotent")
	contains(t, r.out, "Rủi ro: high", "R4")
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
	r.write(policyFile, strings.Replace(r.read(policyFile), `account_balance"], level: high`,
		`account_balance"], level: extreme`, 1))
	eq(t, r.run("submit", "design"), 2)
	contains(t, r.err, "R4", "extreme")
	eq(t, r.exists(riskFile), false)
}

func TestSubmitCasesMissingIdempotentBlocked(t *testing.T) {
	r := bankingRepo(t)
	r.brdApproved()
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
	r.write("aisdlc/features/PAY-1/cases.yaml", replaceImpactLine(r.read("aisdlc/features/PAY-1/cases.yaml"),
		`impact: { level: low, labels: [money-movement], reason: "Chuyển tiền" }`))
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomeFailed)}
	}
	r.run("submit", "cases")
	f := r.risk()
	eq(t, f.Level, "high")
	eqList(t, ruleIDs(f.Matched), "G1")
	contains(t, r.out, "RISK_REQ")
}

func TestSubmitCasesLiteUnknownImpactLevelBlocked(t *testing.T) {
	r := bankingRepo(t)
	r.ok(noConsole(), "init", "PAY-1", "--lite")
	r.write("aisdlc/features/PAY-1/cases.yaml", replaceImpactLine(r.read("aisdlc/features/PAY-1/cases.yaml"),
		`impact: { level: urgent, labels: [], reason: "x" }`))
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomeFailed)}
	}
	eq(t, r.run("submit", "cases"), 1)
	contains(t, r.out, "IMPACT_LEVEL", "urgent")
}

func TestSubmitCasesLiteInvariantsInCasesYamlSatisfyRequirement(t *testing.T) {
	r := bankingRepo(t)
	r.ok(noConsole(), "init", "PAY-1", "--lite")
	r.write("aisdlc/features/PAY-1/cases.yaml", replaceImpactLine(r.read("aisdlc/features/PAY-1/cases.yaml"),
		`impact: { level: low, labels: [money-movement], reason: "Chuyển tiền" }`))
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
	// như tính năng qua cổng Thiết kế trước khi có chính sách rủi ro: design.yaml không có impact, không risk.yaml
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
	must(t, os.Remove(filepath.Join(r.root, riskFile))) // design.yaml có impact: không phải tính năng cũ
	r.runner.cases = failingCases
	eq(t, r.run("submit", "cases"), 1)
	eq(t, r.risk().Level, "high")
	contains(t, r.out, "RISK_REQ", "gửi trùng")
}

func TestSubmitCasesPolicyChangedKeepsOverrideAndWarns(t *testing.T) {
	r := bankingRepo(t)
	r.brdApproved()
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "design")
	r.ok(typed("PAY-1"), "risk", "set", "low", "--reason", "Chỉ đọc sổ cái")
	r.write(policyFile, strings.Replace(r.read(policyFile), "min: 90", "min: 95", 1))
	r.runner.cases = failingCases
	if code := r.run("submit", "cases"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	eq(t, r.risk().Level, "low")
	if r.risk().Override == nil {
		t.Fatal("override phải còn")
	}
	contains(t, r.err, "aisdlc risk reclassify")
}
