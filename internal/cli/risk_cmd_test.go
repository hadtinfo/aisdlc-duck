package cli

import (
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/run"
)

// RiskCommandTest.

// highDesignApproved: banking, thiết kế chạm bảng sổ cái → risk.yaml mức high, cổng Thiết kế đã duyệt.
func highDesignApproved(t *testing.T) *testRepo {
	r := bankingRepo(t)
	r.brdApproved()
	r.llmLevel = "high" // subagent xếp high (luật R4: bảng sổ cái)
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "design")
	eq(t, r.risk().Level, "high")
	return r
}

func (r *testRepo) casesState() string { return r.state("CASES") }

func TestLowerWithoutReasonExit2(t *testing.T) {
	r := highDesignApproved(t)
	eq(t, r.runC(typed("PAY-1"), "risk", "set", "medium"), 2)
	contains(t, r.err, "--reason")
	eq(t, r.risk().Level, "high")
	if r.risk().Override != nil {
		t.Fatal("không được có override")
	}
}

// Phần event RISK_CLASSIFIED/RISK_OVERRIDDEN gửi web: TestLowerWithReasonSendsRiskEvents (hook_web_test.go).
func TestLowerWithReasonWritesOverrideAndEvent(t *testing.T) {
	r := highDesignApproved(t)
	if c := r.runC(typed("PAY-1"), "risk", "set", "medium", "--reason", "Chỉ đọc sổ cái"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	contains(t, r.out, "Đổi mức rủi ro PAY-1: high → medium (lý do: Chỉ đọc sổ cái)",
		"Đã đổi mức rủi ro thành medium. Yêu cầu: ")
	f := r.risk()
	eq(t, f.Level, "medium")
	if f.Override == nil {
		t.Fatal("thiếu override")
	}
	eq(t, f.Override.From, "high")
	eq(t, f.Override.To, "medium")
	eq(t, f.Override.By, "Dev Một")
	eq(t, f.Override.Reason, "Chỉ đọc sổ cái")
	eq(t, f.Override.At, testNow)
	for _, id := range reqIDs(f.Requirements) {
		if id == "idempotent" {
			t.Fatal("medium không đòi idempotent")
		}
	}
}

func TestPolicyChangedSinceClassificationExit2(t *testing.T) {
	r := highDesignApproved(t)
	r.write(policyFile, strings.Replace(r.read(policyFile), "Mọi thay đổi chạm sổ cái", "Ghi chú mới", 1))
	eq(t, r.runC(typed("PAY-1"), "risk", "set", "medium", "--reason", "x"), 2)
	contains(t, r.err, "Chính sách đã đổi kể từ lần phân loại; chạy `aisdlc risk reclassify` trước.")
	eq(t, r.risk().Level, "high")
	if r.risk().Override != nil {
		t.Fatal("không được có override")
	}
}

func TestCurrentLevelNotInScaleExit2(t *testing.T) {
	r := highDesignApproved(t)
	r.write(riskFile, regexp.MustCompile(`(?m)^level: .*$`).ReplaceAllLiteralString(r.read(riskFile), "level: urgent"))
	eq(t, r.runC(typed("PAY-1"), "risk", "set", "medium", "--reason", "x"), 2)
	contains(t, r.err, "urgent")
	eq(t, r.risk().Level, "urgent")
}

func TestRaiseWithoutReasonOk(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(typed("PAY-1"), "reopen", "cases")
	r.ok(typed("PAY-1"), "risk", "set", "medium")
	eq(t, r.risk().Level, "medium")
}

func TestAfterCasesApprovedExit2(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	eq(t, r.runC(typed("PAY-1"), "risk", "set", "high"), 2)
	contains(t, r.err, "Cổng Case đã duyệt; mở lại cổng Case trước")
	eq(t, r.risk().Level, "low")
}

func TestAwaitingCasesNeedsResubmit(t *testing.T) {
	r := newTestRepo(t)
	r.designApproved()
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomeFailed), caseResult("C2", "S2", run.CaseOutcomeFailed)}
	}
	r.ok(noConsole(), "submit", "cases")
	eq(t, r.casesState(), "AWAITING_APPROVAL")
	r.ok(typed("PAY-1"), "risk", "set", "medium")
	contains(t, r.out, "aisdlc submit cases")
	eq(t, r.casesState(), "NOT_SUBMITTED")
}

func TestReclassifyReopensCases(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	eq(t, r.casesState(), "APPROVED")
	r.write(policyFile, strings.Replace(r.read(policyFile), "default: low", "default: medium", 1))
	if c := r.runC(typed("PAY-1"), "risk", "reclassify"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	contains(t, r.out, "low → medium", "Cổng Case đã duyệt: lệnh này sẽ mở lại cổng Case.",
		"Đã ghi aisdlc/features/PAY-1/risk.yaml mức medium. Cổng Case đã mở lại; nộp lại `aisdlc submit cases`.")
	eq(t, r.risk().Level, "medium")
	if r.risk().Override != nil {
		t.Fatal("reclassify bỏ mức đổi tay")
	}
	if r.casesState() == "APPROVED" {
		t.Fatal("cổng Case phải mở lại")
	}
}

func TestNonConsoleExit2(t *testing.T) {
	r := highDesignApproved(t)
	eq(t, r.run("risk", "set", "medium", "--reason", "x"), 2)
	contains(t, r.err, "console")
	eq(t, r.run("risk", "reclassify"), 2)
	contains(t, r.err, "console")
	eq(t, r.risk().Level, "high")
	if r.risk().Override != nil {
		t.Fatal("không được có override")
	}
}
