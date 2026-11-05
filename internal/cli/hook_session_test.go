package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
)

// HookSessionTest (8/8) và phần chỉ đọc cache của HookControlTest.

func sessionIn(root, session, tool, field, value string) string {
	return hookJSON(map[string]any{"session_id": session, "cwd": root}, tool, field, value)
}

// twoFeaturesS1Open: PAY-1 đã duyệt tới CASES và S1 đang chạy; PAY-2 mới; session a → PAY-1, b → PAY-2.
func (r *testRepo) twoFeaturesS1Open() {
	r.t.Helper()
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.ok(noConsole(), "init", "PAY-2")
	must(r.t, model.WriteSession(r.root, "a", "PAY-1"))
	must(r.t, model.WriteSession(r.root, "b", "PAY-2"))
}

func TestSessionStartAppendsEnvFile(t *testing.T) {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	envFile := filepath.Join(r.root, "env")
	r.env["CLAUDE_ENV_FILE"] = envFile
	for i := 0; i < 2; i++ {
		r.hook("session-start", sessionIn(r.root, "s1", "", "", ""))
	}
	eq(t, r.read("env"), "export AISDLC_SESSION=s1\n")
}

func TestSessionStartOutsideRepoStillExports(t *testing.T) {
	r := newTestRepo(t)
	envFile := filepath.Join(r.root, "env")
	r.env["CLAUDE_ENV_FILE"] = envFile
	r.hook("session-start", sessionIn(t.TempDir(), "s9", "", "", ""))
	eq(t, r.out, "")
	eq(t, r.read("env"), "export AISDLC_SESSION=s9\n")
	// id có ký tự ngoài [A-Za-z0-9_-]: không ghi (không trích dẫn an toàn được cho mọi shell)
	r.hook("session-start", sessionIn(t.TempDir(), "x; rm -rf ~", "", "", ""))
	eq(t, r.read("env"), "export AISDLC_SESSION=s9\n")
}

func TestAmbiguousDenyMentionsFeatureOption(t *testing.T) {
	r := newTestRepo(t)
	r.twoFeaturesS1Open()
	r.hook("pre-tool-use", sessionIn(r.root, "z", "Write", "file_path", filepath.Join(r.root, codePath)))
	contains(t, str(hookSpecific(t, r.out), "permissionDecisionReason"), "aisdlc use", "--feature <KEY>")
}

func TestNoEnvFileIsSilent(t *testing.T) {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	r.hook("session-start", sessionIn(r.root, "s1", "", "", ""))
	eq(t, r.err, "")
}

func TestScopeCheckedAgainstSessionFeature(t *testing.T) {
	r := newTestRepo(t)
	r.twoFeaturesS1Open()
	file := filepath.Join(r.root, "src/main/java/com/example/other/Nope.java")
	r.hook("pre-tool-use", sessionIn(r.root, "a", "Write", "file_path", file))
	o := hookSpecific(t, r.out)
	eq(t, str(o, "permissionDecision"), "deny")
	contains(t, str(o, "permissionDecisionReason"), "Ngoài phạm vi")
	// session b (PAY-2, chưa duyệt cổng nào) bị chặn vì lý do khác, không phải phạm vi của PAY-1
	r.hook("pre-tool-use", sessionIn(r.root, "b", "Write", "file_path", file))
	contains(t, str(hookSpecific(t, r.out), "permissionDecisionReason"), "Chưa được viết code")
}

func TestSessionFeatureInScopeAllowed(t *testing.T) {
	r := newTestRepo(t)
	r.twoFeaturesS1Open()
	r.hook("pre-tool-use", sessionIn(r.root, "a", "Write", "file_path", filepath.Join(r.root, codePath)))
	eq(t, r.out, "")
}

func TestUnboundSessionTwoOpenDeniesWrites(t *testing.T) {
	r := newTestRepo(t)
	r.twoFeaturesS1Open()
	r.hook("pre-tool-use", sessionIn(r.root, "z", "Write", "file_path", filepath.Join(r.root, codePath)))
	o := hookSpecific(t, r.out)
	eq(t, str(o, "permissionDecision"), "deny")
	contains(t, str(o, "permissionDecisionReason"), "aisdlc use")
}

func TestAssigningSessionVarDenied(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	for _, cmd := range []string{"AISDLC_SESSION=b aisdlc use PAY-9", "export AISDLC_SESSION=b",
		"env AISDLC_SESSION=b aisdlc use PAY-9", "FOO=1 AISDLC_SESSION=b ls"} {
		r.hook("pre-tool-use", sessionIn(r.root, "a", "Bash", "command", cmd))
		o := hookSpecific(t, r.out)
		if str(o, "permissionDecision") != "deny" {
			t.Fatalf("%s: %s", cmd, r.out)
		}
		contains(t, str(o, "permissionDecisionReason"), "AISDLC_SESSION")
	}
	r.hook("pre-tool-use", sessionIn(r.root, "a", "Bash", "command", "echo $AISDLC_SESSION"))
	eq(t, r.out, "")
}

// ---- HookControlTest, phần chỉ đọc cache điều khiển đã ký (không outbox, không sổ lệnh chờ, không web flush).
// Để giai đoạn 4: pausedDenySpawnsFlushThrottled, instructionDeliveredOnce, pendingAcceptAppliedByHook,
// scopeDeniedEvent, sessionIdOnEvents, userPromptSubmitDeliversOnceAndSpawnsFlush,
// userPromptSubmitSilentOutsideRepoAndWithoutControlDir, pausedDenyWaitsForInFlightFlushThatLiftsPause,
// pausedDenyStillDeniedWhenInFlightFlushKeepsPause, subagentToolCallDoesNotClaimInstructions,
// heldControlLockDeniesWriteAndSkipsOtherHooks; phần event/giao một lần của
// sessionStartAppendsUndeliveredInstructions.

type controlFx struct {
	*testRepo
	priv   ed25519.PrivateKey
	repoID string
}

func newHookControl(t *testing.T) *controlFx {
	r := newTestRepo(t)
	home := t.TempDir()
	r.env["HOME"] = home
	_, priv := pinKey(t, home)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	return &controlFx{r, priv, snapshot.RepoID(context.Background(), gitx.New(r.root), r.root)}
}

func (c *controlFx) writeControl(fc snapshot.FeatureControl, signer ed25519.PrivateKey) {
	c.t.Helper()
	if fc.Repo == "" {
		fc.Repo = c.repoID
	}
	b, err := fc.ToJSON()
	must(c.t, err)
	c.writeSigned(".aisdlc/web/control/PAY-1.json", b, signer)
}

func (c *controlFx) paused() snapshot.FeatureControl {
	return snapshot.FeatureControl{Feature: "PAY-1", IssuedAt: testNow, Pause: &snapshot.Pause{By: "an", At: testNow}}
}

func (c *controlFx) codeEditEdit() string {
	return hookIn("PreToolUse", c.root, "Edit", "file_path", filepath.Join(c.root, codePath))
}

func TestPausedDenies(t *testing.T) {
	c := newHookControl(t)
	c.hook("pre-tool-use", c.codeEditEdit())
	eq(t, c.out, "") // chưa tạm dừng: sửa code trong phạm vi được
	c.writeControl(c.paused(), c.priv)
	c.hook("pre-tool-use", c.codeEditEdit())
	o := hookSpecific(t, c.out)
	eq(t, str(o, "permissionDecision"), "deny")
	eq(t, str(o, "permissionDecisionReason"),
		"Dev đã tạm dừng tại web: an. Dừng lại và chờ. Khi dev báo tiếp tục thì thử lại.")
	// 2b không mở web flush khi bị chặn vì tạm dừng
	eq(t, c.exists(".aisdlc/web/control/spawned"), false)
}

func TestForgedControlIgnored(t *testing.T) {
	c := newHookControl(t)
	_, other, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	c.writeControl(c.paused(), other)
	c.hook("pre-tool-use", c.codeEditEdit())
	eq(t, c.out, "")
	// phong bì ký đúng nhưng của tính năng khác cũng bị bỏ qua
	p := c.paused()
	p.Feature = "PAY-2"
	c.writeControl(p, c.priv)
	c.hook("pre-tool-use", c.codeEditEdit())
	eq(t, c.out, "")
}

func TestStopAllowedWhenPaused(t *testing.T) {
	c := newHookControl(t)
	c.hook("stop", hookIn("Stop", c.root, "", "", ""))
	contains(t, c.out, "block")
	c.writeControl(c.paused(), c.priv)
	c.hook("stop", hookIn("Stop", c.root, "", "", ""))
	notContains(t, c.out, "block")
}

// Ở 2b chỉ đọc: chỉ dẫn chưa có trong sổ cục bộ được nối vào ngữ cảnh SessionStart (không ghi sổ, không event;
// giao một lần là việc của giai đoạn 4). Subagent không nhận chỉ dẫn.
func TestSessionStartAppendsUndeliveredInstructions(t *testing.T) {
	c := newHookControl(t)
	c.writeControl(snapshot.FeatureControl{Feature: "PAY-1", IssuedAt: testNow, Instructions: []snapshot.Instruction{
		{ID: 42, By: "an", At: testNow, Text: "đọc lại BRD"}, {ID: 43, By: "an", At: testNow, Text: "đã giao"}}},
		c.priv)
	c.write(".aisdlc/web/control/PAY-1.local.json", `{"delivered":[43]}`)
	c.hook("session-start", hookIn("SessionStart", c.root, "", "", ""))
	ctx := str(hookSpecific(t, c.out), "additionalContext")
	if !strings.HasPrefix(ctx, "Quy tắc AISDLC") || !strings.HasSuffix(ctx, "): đọc lại BRD") {
		t.Fatalf("got %q", ctx)
	}
	contains(t, ctx, "\n\nChỉ dẫn của dev (")
	notContains(t, ctx, "đã giao")
	eq(t, c.read(".aisdlc/web/control/PAY-1.local.json"), `{"delivered":[43]}`)

	c.hook("session-start", hookJSON(map[string]any{"session_id": "abc", "cwd": c.root, "agent_id": "sub-1"}, "", "", ""))
	ctx = str(hookSpecific(t, c.out), "additionalContext")
	contains(t, ctx, "Quy tắc AISDLC")
	notContains(t, ctx, "đọc lại BRD")
	_, err := os.Stat(filepath.Join(c.root, ".aisdlc/web/outbox"))
	eq(t, os.IsNotExist(err), true)
}
