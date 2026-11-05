package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aisdlc/internal/gitx"
)

// DevOnlyCommandTest: lệnh chỉ dev cần console thật và gõ lại feature key (không có phiếu cho phép của Claude Code;
// đường phiếu qua hook ở task 7).

func brdSubmitted(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.run("setup")
	r.run("init", "PAY-1")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	eq(t, r.run("submit", "brd"), 0)
	return r
}

func (r *testRepo) bytes(rel string) []byte {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.root, rel))
	must(r.t, err)
	return b
}

const gatesJSON = "aisdlc/features/PAY-1/gates.json"

func TestApproveWithoutConsoleExits3(t *testing.T) {
	r := brdSubmitted(t)
	before := r.bytes(gatesJSON)
	eq(t, r.run("approve", "brd"), 3)
	contains(t, r.err, "terminal")
	eq(t, bytes.Equal(r.bytes(gatesJSON), before), true)
}

func TestApproveWithWrongKeyExits3(t *testing.T) {
	r := brdSubmitted(t)
	before := r.bytes(gatesJSON)
	eq(t, r.runC(typed("PAY-2"), "approve", "brd"), 3)
	eq(t, r.runC(typed(""), "approve", "brd"), 3)
	eq(t, bytes.Equal(r.bytes(gatesJSON), before), true)
}

func TestApproveWithRightKeyRecordsGitUserAndPrintsSummary(t *testing.T) {
	r := brdSubmitted(t)
	if c := r.runC(typed(" PAY-1 "), "approve", "brd"); c != 0 {
		t.Fatal(r.err)
	}
	contains(t, r.out, "aisdlc/features/PAY-1/brd.md", "hash")
	contains(t, r.read(gatesJSON), `"approver": "Dev Một"`)
	eq(t, r.run("status", "--json"), 0)
	contains(t, r.out, `"BRD":"APPROVED"`)
}

func TestApproveNotSubmittedGateExits2(t *testing.T) {
	r := brdSubmitted(t)
	eq(t, r.runC(typed("PAY-1"), "approve", "design"), 2)
	eq(t, r.runC(typed("PAY-1"), "approve", "nope"), 2)
}

func TestRejectAndReopenWithoutConsoleExit3(t *testing.T) {
	r := brdSubmitted(t)
	before := r.bytes(gatesJSON)
	eq(t, r.run("reject", "brd", "-m", "thiếu số liệu"), 3)
	eq(t, r.run("reopen", "brd"), 3)
	eq(t, bytes.Equal(r.bytes(gatesJSON), before), true)
	eq(t, r.exists("aisdlc/features/PAY-1/feedback.md"), false)
}

func TestRejectWritesFeedback(t *testing.T) {
	r := brdSubmitted(t)
	eq(t, r.runC(typed("PAY-1"), "reject", "brd", "-m", "thiếu số liệu"), 0)
	contains(t, r.read("aisdlc/features/PAY-1/feedback.md"), "thiếu số liệu", "Dev Một")
	eq(t, r.run("status", "--json"), 0)
	contains(t, r.out, `"BRD":"NOT_SUBMITTED"`)
}

func TestStepAcceptWithoutConsoleOrWrongKeyExits3(t *testing.T) {
	r := newTestRepo(t)
	r.s1AwaitingDev()
	before := r.bytes("aisdlc/features/PAY-1/steps.json")
	eq(t, r.run("step", "accept", "S1"), 3)
	eq(t, r.runC(typed("PAY-2"), "step", "accept", "S1"), 3)
	eq(t, bytes.Equal(r.bytes("aisdlc/features/PAY-1/steps.json"), before), true)

	if c := r.runC(typed("PAY-1"), "step", "accept", "S1"); c != 0 {
		t.Fatal(r.err)
	}
	eq(t, r.run("status", "--json"), 0)
	contains(t, r.out, `"S1":"ACCEPTED"`)
}

func TestScopeAllowWithoutConsoleOrWrongKeyExits3(t *testing.T) {
	r := newTestRepo(t)
	r.s1AwaitingDev()
	eq(t, r.run("scope", "allow", "go.mod"), 3)
	eq(t, r.runC(typed("PAY-2"), "scope", "allow", "go.mod"), 3)
	eq(t, r.exists("aisdlc/features/PAY-1/scope-extra.json"), false)

	if c := r.runC(typed("PAY-1"), "scope", "allow", filepath.Join(r.root, "go.mod")); c != 0 {
		t.Fatal(r.err)
	}
	contains(t, r.out, "Tính năng PAY-1 — cho phép sửa ngoài phạm vi: go.mod", "Đã cho phép go.mod.")
	contains(t, r.read("aisdlc/features/PAY-1/scope-extra.json"), `"path": "go.mod"`)
}

// consoleSpy ghi lại stdout đã in lúc lời nhắc "Gõ lại feature key" hiện ra.
type consoleSpy struct {
	r           *testRepo
	outAtPrompt string
}

func (c *consoleSpy) ReadLine(string) (string, bool) {
	c.outAtPrompt = c.r.currentOut()
	return "PAY-1", true
}

func TestApprovePrintsCardBeforeConfirm(t *testing.T) {
	r := brdSubmitted(t)
	spy := &consoleSpy{r: r}
	if c := r.runC(spy, "approve", "brd"); c != 0 {
		t.Fatal(r.err)
	}
	card := strings.Index(r.out, "Review card —")
	if card < 0 {
		t.Fatalf("thiếu review card:\n%s", r.out)
	}
	contains(t, spy.outAtPrompt, "Review card —", "Cờ đỏ (")
	if strings.Index(r.out, "Đã duyệt cổng BRD") <= card {
		t.Fatalf("phải duyệt sau khi in thẻ:\n%s", r.out)
	}
}

func TestApproveStillWorksWhenCardFails(t *testing.T) {
	r := brdSubmitted(t)
	// chỉ hỏng khi review card dựng (git dùng cho việc khác vẫn chạy)
	r.git = func(root string) *gitx.Repo {
		if calledFrom("cli.CardFor") {
			panic("git hỏng")
		}
		return gitx.New(root)
	}
	if c := r.runC(typed("PAY-1"), "approve", "brd"); c != 0 {
		t.Fatal(r.err)
	}
	contains(t, r.out, "Không dựng được review card: git hỏng", "Đã duyệt cổng BRD")
	notContains(t, r.out+r.err, "goroutine", "panic")
}

func TestApproveWithBrokenDesignYamlHasNoStackTrace(t *testing.T) {
	r := brdSubmitted(t)
	eq(t, r.runC(typed("PAY-1"), "approve", "brd"), 0)
	eq(t, r.run("submit", "design"), 0)
	r.write(designFile, "steps: [oops\n")
	if r.runC(typed("PAY-1"), "approve", "design") == 0 {
		t.Fatal("phải lỗi")
	}
	notContains(t, r.out+r.err, "goroutine", "panic", ".go:")
}
