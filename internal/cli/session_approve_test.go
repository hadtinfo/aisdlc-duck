package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/policy"
)

// SessionApproveTest (14/14): duyệt trong Claude Code — hook hỏi Yes/No và ghi phiếu một lần, CLI không console
// tiêu thụ phiếu (spec §3). Thêm: TestGrantNotUsableFromOtherSession, TestReadToolClearsGrant và vài test vượt rào.

// approveRepo: repo với AISDLC_SESSION=s1 (lệnh CLI chạy trong session s1).
func approveRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.env["AISDLC_SESSION"] = "s1"
	return r
}

func (r *testRepo) brdSubmittedS1() {
	r.t.Helper()
	r.ok(noConsole(), "setup")
	r.ok(noConsole(), "init", "PAY-1")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
}

// enableOnBase bật sessionApprove trong config đã commit ở nhánh base (baseRef), như sau khi MR cấu hình đã merge.
func (r *testRepo) enableOnBase() {
	r.t.Helper()
	c := r.read("aisdlc/config.yaml")
	contains(r.t, c, "baseRef: origin/main", "# sessionApprove: true")
	r.write("aisdlc/config.yaml", strings.Replace(strings.Replace(c, "baseRef: origin/main", "baseRef: base", 1),
		"# sessionApprove: true", "sessionApprove: true", 1))
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "bật sessionApprove")
	r.gitCmd("branch", "-f", "base")
}

func approveHookIn(root, session, mode, tool, field, value string) string {
	base := map[string]any{"session_id": session, "cwd": root, "hook_event_name": "PreToolUse"}
	if mode != "" {
		base["permission_mode"] = mode
	}
	return hookJSON(base, tool, field, value)
}

// bash chạy hook PreToolUse (session s1) cho một lệnh Bash; trả hookSpecificOutput (rỗng nếu hook im lặng).
func (r *testRepo) bash(mode, command string) map[string]any {
	r.t.Helper()
	return r.bashAs("s1", mode, command)
}

func (r *testRepo) bashAs(session, mode, command string) map[string]any {
	r.t.Helper()
	r.hook("pre-tool-use", approveHookIn(r.root, session, mode, "Bash", "command", command))
	return hookSpecific(r.t, r.out)
}

func (r *testRepo) grant() string { return policy.GrantFile(r.root, "s1") }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestHookAsksAndIssuesGrant(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	o := r.bash("default", "aisdlc approve brd")
	eq(t, str(o, "permissionDecision"), "ask")
	eq(t, str(o, "hookEventName"), "PreToolUse")
	reason := str(o, "permissionDecisionReason")
	if !strings.HasPrefix(reason, "[PAY-1] Duyệt cổng BRD @ ") ||
		!strings.HasSuffix(reason, "Bấm Yes nếu chính bạn muốn duyệt.") {
		t.Fatalf("got %q", reason)
	}
	contains(t, reason, " — HIGH ", ", WARN ")
	// hash rút gọn 8 ký tự đúng hash hiện tại của cổng
	hash, err := r.feature().Gates.CurrentHash(gate.BRD)
	must(t, err)
	contains(t, reason, " @ "+hash[:8]+" — ")
	if !strings.HasPrefix(r.out, `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask",`+
		`"permissionDecisionReason":`) {
		t.Fatalf("got %q", r.out)
	}
	eq(t, fileExists(r.grant()), true)
	for _, mode := range []string{"acceptEdits", "plan"} {
		eq(t, str(r.bash(mode, "aisdlc approve brd"), "permissionDecision"), "ask")
	}
}

func TestBypassDenies(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	for _, mode := range []string{"bypassPermissions", "dontAsk", "auto", "weird"} {
		o := r.bash(mode, "aisdlc approve brd")
		eq(t, str(o, "permissionDecision"), "deny")
		contains(t, str(o, "permissionDecisionReason"), "(permission_mode: "+mode+")", "terminal riêng hoặc web")
		eq(t, fileExists(r.grant()), false)
	}
}

func TestMissingModeDenies(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	o := r.bash("", "aisdlc approve brd")
	eq(t, str(o, "permissionDecision"), "deny")
	contains(t, str(o, "permissionDecisionReason"), "(permission_mode: không rõ)", "terminal riêng hoặc web")
	eq(t, fileExists(r.grant()), false)
}

func TestIndirectFormDenied(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	for _, cmd := range []string{`bash -c "aisdlc approve brd"`, "aisdlc approve brd && echo ok",
		"X=1 aisdlc approve brd", "$(echo aisdlc) approve brd; aisdlc approve brd", "./aisdlc approve brd"} {
		o := r.bash("default", cmd)
		if str(o, "permissionDecision") != "deny" {
			t.Fatalf("%s: %s", cmd, r.out)
		}
		contains(t, str(o, "permissionDecisionReason"), "gọi thẳng `aisdlc <lệnh>`")
		eq(t, fileExists(r.grant()), false)
	}
}

func TestApproveWithGrantNoConsole(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	eq(t, str(r.bash("default", "aisdlc approve brd"), "permissionDecision"), "ask")
	if c := r.run("approve", "brd"); c != 0 {
		t.Fatalf("exit %d: %s", c, r.err)
	}
	contains(t, r.read(gatesJSON), `"channel": "claude-code"`)
	eq(t, fileExists(r.grant()), false)
	eq(t, r.run("status"), 0)
	contains(t, r.out, "duyệt trong Claude Code")
	eq(t, r.run("review", "brd"), 0)
	contains(t, r.out, "duyệt trong Claude Code")

	// phiếu dùng một lần: mở lại có phiếu → được; mở lại lần nữa không hỏi lại → exit 3
	eq(t, str(r.bash("default", "aisdlc reopen brd"), "permissionDecision"), "ask")
	if c := r.run("reopen", "brd"); c != 0 {
		t.Fatalf("exit %d: %s", c, r.err)
	}
	eq(t, r.run("reopen", "brd"), 3)
	contains(t, r.err, "một mình")
}

func TestConsoleApprovalHasNoChannel(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	r.ok(typed("PAY-1"), "approve", "brd")
	notContains(t, r.read(gatesJSON), "channel")
}

func TestGrantBoundToSessionCommandAndFeature(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	before := r.bytes(gatesJSON)
	r.bash("default", "aisdlc approve brd")
	r.env["AISDLC_SESSION"] = "s2"
	eq(t, r.run("approve", "brd"), 3)
	delete(r.env, "AISDLC_SESSION")
	eq(t, r.run("approve", "brd"), 3)
	r.env["AISDLC_SESSION"] = "s1"
	// phiếu cho approve không dùng được cho reject (và lần thử sai đốt phiếu)
	eq(t, r.run("reject", "brd", "-m", "x"), 3)
	eq(t, r.run("approve", "brd"), 3)
	// cùng lệnh nhưng thêm --feature: chuỗi lệnh khác → không khớp
	r.bash("default", "aisdlc approve brd")
	eq(t, r.run("approve", "brd", "--feature", "PAY-1"), 3)
	eq(t, bytes.Equal(r.bytes(gatesJSON), before), true)
	eq(t, r.exists("aisdlc/features/PAY-1/feedback.md"), false)
}

func TestRejectWithQuotedMessageAndExplicitFeature(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	o := r.bash("default", `aisdlc reject brd -m "thiếu số liệu" --feature PAY-1`)
	eq(t, str(o, "permissionDecision"), "ask")
	reason := str(o, "permissionDecisionReason")
	if !strings.HasPrefix(reason, "[PAY-1] Trả lại cổng BRD @ ") {
		t.Fatalf("got %q", reason)
	}
	contains(t, reason, " — góp ý: thiếu số liệu. Bấm Yes nếu chính bạn muốn trả lại.")
	if c := r.run("reject", "brd", "-m", "thiếu số liệu", "--feature", "PAY-1"); c != 0 {
		t.Fatalf("exit %d: %s", c, r.err)
	}
	contains(t, r.read("aisdlc/features/PAY-1/feedback.md"), "thiếu số liệu")
}

func TestOtherToolClearsGrant(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	for _, tool := range []string{"Read", "Write", "Bash"} {
		r.bash("default", "aisdlc approve brd")
		eq(t, fileExists(r.grant()), true)
		field, value := "file_path", filepath.Join(r.root, "README.md")
		if tool == "Bash" {
			field, value = "command", "ls"
		}
		r.hook("pre-tool-use", approveHookIn(r.root, "s1", "default", tool, field, value))
		if fileExists(r.grant()) {
			t.Fatalf("%s không xóa phiếu", tool)
		}
		eq(t, r.run("approve", "brd"), 3)
		contains(t, r.err, "một mình")
	}
	// PreToolUse của session khác không xóa phiếu của s1
	r.bash("default", "aisdlc approve brd")
	r.hook("pre-tool-use", approveHookIn(r.root, "s2", "default", "Bash", "command", "ls"))
	eq(t, fileExists(r.grant()), true)
}

func TestHashChangedAfterAsk(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	r.bash("default", "aisdlc approve brd")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD+"\n- thêm dòng\n")
	r.ok(noConsole(), "submit", "brd") // cổng lại AWAITING_APPROVAL, nhưng hash khác lúc hỏi
	eq(t, r.run("approve", "brd"), 3)
	notContains(t, r.read(gatesJSON), `"approver"`)
}

func TestDisabledKeepsOldBehaviour(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	// chỉ bật ở working tree (chưa merge vào baseRef): không có hiệu lực
	r.write("aisdlc/config.yaml", strings.Replace(r.read("aisdlc/config.yaml"), "# sessionApprove: true",
		"sessionApprove: true", 1))
	o := r.bash("default", "aisdlc approve brd")
	eq(t, str(o, "permissionDecision"), "deny")
	contains(t, str(o, "permissionDecisionReason"), "agent không được chạy qua Bash")
	eq(t, fileExists(r.grant()), false)
	eq(t, r.run("approve", "brd"), 3)
	contains(t, r.err, "terminal")
}

func TestPrePlacedGrantIgnoredWhenDisabledOnBase(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	// baseRef đọc được nhưng sessionApprove tắt ở đó
	r.write("aisdlc/config.yaml", strings.Replace(r.read("aisdlc/config.yaml"), "baseRef: origin/main",
		"baseRef: base", 1))
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "baseRef")
	r.gitCmd("branch", "-f", "base")
	hash, err := r.feature().Gates.CurrentHash(gate.BRD)
	must(t, err)
	cmd, ok := policy.ParseDevCommand("aisdlc approve brd")
	eq(t, ok, true)
	must(t, policy.IssueGrant(r.root, policy.Grant{SessionID: "s1", Feature: "PAY-1", Command: cmd.GrantKey(),
		Hash: hash, ExpiresAt: testNow.Add(policy.GrantTTL)}))

	eq(t, r.run("approve", "brd"), 3)
	contains(t, r.err, "trong terminal thật")
	notContains(t, r.err, "Không có xác nhận")
	eq(t, fileExists(r.grant()), true)
	notContains(t, r.read(gatesJSON), `"approver"`)

	// đối chứng: cùng phiếu đó hợp lệ khi bật ở baseRef
	r.write("aisdlc/config.yaml", strings.Replace(r.read("aisdlc/config.yaml"), "# sessionApprove: true",
		"sessionApprove: true", 1))
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "bật sessionApprove")
	r.gitCmd("branch", "-f", "base")
	if c := r.run("approve", "brd"); c != 0 {
		t.Fatalf("exit %d: %s", c, r.err)
	}
	eq(t, fileExists(r.grant()), false)
}

func TestReclassifyWithoutConsoleSaysConsoleEvenWhenEnabled(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	eq(t, r.run("risk", "reclassify"), 2)
	contains(t, r.err, "không tìm thấy console")
	notContains(t, r.err, "Không có xác nhận")
}

func TestStepAcceptAndScopeAllowWithGrant(t *testing.T) {
	r := approveRepo(t)
	r.s1AwaitingDev()
	r.enableOnBase()
	o := r.bash("default", "aisdlc scope allow go.mod")
	eq(t, str(o, "permissionDecision"), "ask")
	eq(t, str(o, "permissionDecisionReason"),
		"[PAY-1] Cho phép sửa ngoài phạm vi: go.mod. Bấm Yes nếu chính bạn muốn cho phép.")
	if c := r.run("scope", "allow", "go.mod"); c != 0 {
		t.Fatalf("exit %d: %s", c, r.err)
	}
	contains(t, r.read("aisdlc/features/PAY-1/scope-extra.json"), `"path": "go.mod"`)

	o = r.bash("default", "aisdlc step accept S1")
	eq(t, str(o, "permissionDecision"), "ask")
	eq(t, str(o, "permissionDecisionReason"), "[PAY-1] Chấp nhận bước S1. Bấm Yes nếu chính bạn muốn chấp nhận.")
	if c := r.run("step", "accept", "S1"); c != 0 {
		t.Fatalf("exit %d: %s", c, r.err)
	}
	eq(t, r.run("status", "--json"), 0)
	contains(t, r.out, `"S1":"ACCEPTED"`)
}

// ---- thêm theo brief và vượt rào ----

// Phiếu của session s1 không dùng được ở session khác, kể cả khi session kia cũng được hook hỏi cho lệnh khác.
func TestGrantNotUsableFromOtherSession(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	r.bash("default", "aisdlc approve brd")
	eq(t, fileExists(r.grant()), true)
	// session s2 dùng lệnh CLI: không có phiếu của s2 → exit 3, phiếu của s1 không bị đụng
	r.env["AISDLC_SESSION"] = "s2"
	eq(t, r.run("approve", "brd"), 3)
	contains(t, r.err, "một mình")
	eq(t, fileExists(r.grant()), true)
	notContains(t, r.read(gatesJSON), `"approver"`)
	// s2 được hỏi cho reopen: phiếu đó không duyệt được brd
	eq(t, str(r.bashAs("s2", "default", "aisdlc reopen brd"), "permissionDecision"), "ask")
	eq(t, r.run("approve", "brd"), 3)
	notContains(t, r.read(gatesJSON), `"approver"`)
	// chép phiếu s1 sang tên file của s2: sessionId trong phiếu vẫn là s1 → không khớp
	r.bash("default", "aisdlc approve brd")
	b, err := os.ReadFile(r.grant())
	must(t, err)
	must(t, os.WriteFile(policy.GrantFile(r.root, "s2"), b, 0o644))
	eq(t, r.run("approve", "brd"), 3)
	notContains(t, r.read(gatesJSON), `"approver"`)
}

// Tool đọc (Read, Grep, Glob; cả tên cũ pre-tool-use-read) cũng xóa phiếu chưa dùng của session (luật clear_grant
// của plugin/bin/aisdlc), kể cả tool lạ và input hỏng mà vẫn đọc được session_id.
func TestReadToolClearsGrant(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	cases := []struct{ event, tool, field, value string }{
		{"pre-tool-use", "Read", "file_path", filepath.Join(r.root, "go.mod")},
		{"pre-tool-use", "Grep", "pattern", "approve"},
		{"pre-tool-use", "Glob", "pattern", "**/*.go"},
		{"pre-tool-use-read", "Read", "file_path", filepath.Join(r.root, "go.mod")},
		{"pre-tool-use", "WebFetch", "url", "https://example.com"},
	}
	for _, c := range cases {
		r.bash("default", "aisdlc approve brd")
		eq(t, fileExists(r.grant()), true)
		r.hook(c.event, approveHookIn(r.root, "s1", "default", c.tool, c.field, c.value))
		eq(t, r.out, "")
		if fileExists(r.grant()) {
			t.Fatalf("%s %s không xóa phiếu", c.event, c.tool)
		}
		eq(t, r.run("approve", "brd"), 3)
	}
	// input hỏng nhưng phần đầu có session_id và cwd: vẫn xóa (fail closed về phía phiếu)
	r.bash("default", "aisdlc approve brd")
	r.hook("pre-tool-use", `{"session_id":"s1","cwd":"`+r.root+`","tool_name":"Read","tool_input":{`)
	contains(t, r.out, `"permissionDecision":"deny"`)
	eq(t, fileExists(r.grant()), false)
}

// Hỏi lại lệnh chỉ dev ngay sau khi được cấp phiếu: phiếu mới thay phiếu cũ (không cộng dồn); phiếu hết hạn không
// dùng được.
func TestExpiredGrantRejected(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	r.bash("default", "aisdlc approve brd")
	r.now = testNow.Add(policy.GrantTTL)
	eq(t, r.run("approve", "brd"), 3)
	notContains(t, r.read(gatesJSON), `"approver"`)
	eq(t, fileExists(r.grant()), false)
}

// Lệnh chỉ dev qua hook: tính năng phải xác định được và cổng phải thuộc luồng; không thì không cấp phiếu.
func TestDevOnlyAskNeedsFeatureAndGate(t *testing.T) {
	r := approveRepo(t)
	r.brdSubmittedS1()
	r.enableOnBase()
	// session lạ, hai tính năng đang mở (PAY-1 đã có ở base nên coi như đóng): không đoán
	r.env["AISDLC_SESSION"] = "s9"
	r.ok(noConsole(), "init", "PAY-2")
	r.ok(noConsole(), "init", "PAY-3")
	r.env["AISDLC_SESSION"] = "s1"
	o := r.bashAs("zz", "default", "aisdlc approve brd")
	eq(t, str(o, "permissionDecision"), "deny")
	contains(t, str(o, "permissionDecisionReason"), "thêm `--feature <KEY>` vào lệnh")
	eq(t, fileExists(policy.GrantFile(r.root, "zz")), false)
	// cổng lạ
	o = r.bash("default", "aisdlc approve nope")
	eq(t, str(o, "permissionDecision"), "deny")
	contains(t, str(o, "permissionDecisionReason"), "Không đọc được tên cổng trong lệnh `aisdlc approve nope`.")
	eq(t, fileExists(r.grant()), false)
	// không có session_id
	r.hook("pre-tool-use", hookJSON(map[string]any{"cwd": r.root, "permission_mode": "default"}, "Bash", "command",
		"aisdlc approve brd"))
	contains(t, str(hookSpecific(t, r.out), "permissionDecisionReason"), "Không có session_id trong input hook")
	// --feature trỏ tính năng khác: phiếu ghi cho tính năng đó, approve không có --feature không dùng được
	o = r.bash("default", "aisdlc approve brd --feature PAY-2")
	eq(t, str(o, "permissionDecision"), "ask")
	contains(t, str(o, "permissionDecisionReason"), "[PAY-2] Duyệt cổng BRD")
	eq(t, r.run("approve", "brd"), 3)
	notContains(t, r.read(gatesJSON), `"approver"`)
}
