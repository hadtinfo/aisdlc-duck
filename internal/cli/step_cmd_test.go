package cli

import (
	"testing"

	"aisdlc/internal/run"
)

// CliFlowTest (phần step/scope/diff/eval) và các lệnh tạm eval, web, policy pull.

const s1File = "src/main/java/com/example/limit/web/LimitController.java"

// s1AwaitingDev: như casesApproved + S1 đã finish, đang chờ dev.
func (r *testRepo) s1AwaitingDev() {
	r.t.Helper()
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.write(s1File, "class LimitController {}\n")
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomePassed)}
	}
	r.ok(noConsole(), "step", "finish", "S1")
}

// Phần step/status/diff của fullFlowHappyPathWithoutMaven (phần submit/approve ở TestFullFlowHappyPathWithoutHarness).
func TestFullFlowStepAndDiff(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	if c := r.run("step", "start", "S1"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	contains(t, r.out, "Bắt đầu bước S1 — Validate request", "src/main/java/**/limit/web/**", "3 file", "C1",
		"Xong thì chạy `aisdlc step finish S1`.")
	r.write(s1File, "class LimitController {}\n")
	r.write("go.mod", "module example.com/pay\n\ngo 1.26\n// đổi\n")
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomePassed)}
	}
	eq(t, r.run("step", "finish", "S1"), 1) // go.mod ngoài phạm vi
	endsWith(t, r.runner.calls, "cases:S1")
	contains(t, r.out, "Đang chạy case của S1 qua harness...",
		"Case: 1/1 pass · 2 file / 2 dòng (ngân sách 3/120) · Ngoài phạm vi: go.mod",
		"File ngoài phạm vi: dev chấp nhận bằng `aisdlc scope allow <path>`, hoặc agent hoàn tác.",
		"Dừng lại. Dev xem rồi chạy `aisdlc step accept S1` ở terminal của mình.")

	eq(t, r.run("status", "--json"), 0)
	contains(t, r.out, `"S1":"AWAITING_DEV"`, `"CASES":"APPROVED"`)
	eq(t, r.run("status"), 0)
	contains(t, r.out, "PAY-1", "AWAITING_DEV")

	if c := r.run("diff", "--by-step", "--patch"); c != 0 {
		t.Fatal(r.err)
	}
	contains(t, r.out, "## S1 — Validate request", "1\t0\t"+s1File, "+class LimitController {}")
	eq(t, r.run("diff"), 2) // --by-step bắt buộc như picocli
}

func TestStepStartBeforeGatesExits2(t *testing.T) {
	r := newTestRepo(t)
	r.run("setup")
	r.run("init", "PAY-1")
	eq(t, r.run("step", "start", "S1"), 2)
	contains(t, r.err, "Chưa được code")
}

func TestResubmitCasesAfterCodingStartedMayAllPass(t *testing.T) {
	r := newTestRepo(t)
	r.s1AwaitingDev()
	r.ok(typed("PAY-1"), "reopen", "cases")
	// dev sửa kỳ vọng sai của C2; code hiện tại đã đúng nên mọi case pass
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomePassed), caseResult("C2", "S2", run.CaseOutcomePassed)}
	}
	if c := r.run("submit", "cases"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	notContains(t, r.out, "CASES_NONE_FAILING")
	contains(t, r.out, "C1, C2")
	r.ok(typed("PAY-1"), "approve", "cases")
}

func TestStepUnknownAndWrongStatusExit2(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	eq(t, r.run("step", "start", "S9"), 2)
	contains(t, r.err, "Không có bước S9 trong thiết kế.")
	eq(t, r.run("step", "finish", "S1"), 2)
	contains(t, r.err, "Bước S1 đang ở trạng thái NOT_STARTED, cần IN_PROGRESS.")
	eq(t, r.runC(typed("PAY-1"), "step", "accept", "S1"), 2)
	contains(t, r.err, "Bước S1 đang ở trạng thái NOT_STARTED, cần AWAITING_DEV.")
	eq(t, r.run("step"), 2)
	eq(t, r.run("scope"), 2)
}

func TestStepAcceptThenNext(t *testing.T) {
	r := newTestRepo(t)
	r.s1AwaitingDev()
	r.ok(typed("PAY-1"), "step", "accept", "S1")
	contains(t, r.out, "Tính năng PAY-1 — bước S1", "Case: 1/1 pass", "Đã chấp nhận bước S1 (Dev Một).",
		"Bước kế: `aisdlc step start S2`.")
}

func TestScopeAllowOutsideRepoExit2(t *testing.T) {
	r := newTestRepo(t)
	r.s1AwaitingDev()
	eq(t, r.runC(typed("PAY-1"), "scope", "allow", "../x.go"), 2)
	contains(t, r.err, "Path nằm ngoài repo: ../x.go")
	eq(t, r.runC(typed("PAY-1"), "scope", "allow", "."), 2)
	eq(t, r.exists("aisdlc/features/PAY-1/scope-extra.json"), false)
}

func TestWebStubsExit2(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	for _, sub := range []string{"status", "push", "flush", "login", "approve-token", "migrate"} {
		eq(t, r.run("web", sub), 2)
		contains(t, r.err, "Lệnh web chưa có trong bản Go (giai đoạn 4).")
	}
	eq(t, r.run("web"), 2) // không lệnh con: usage
	// đã `aisdlc web login`: policy pull cần web client
	home := t.TempDir()
	r.env["HOME"] = home
	r.env["AISDLC_WEB_URL"] = "http://127.0.0.1:1"
	r.env["AISDLC_WEB_TOKEN"] = "x"
	pinKey(t, home)
	eq(t, r.run("policy", "pull"), 2)
	contains(t, r.err, "Lệnh web chưa có trong bản Go (giai đoạn 4).")
}
