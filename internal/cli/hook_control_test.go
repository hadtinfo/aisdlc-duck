package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/fsx"
	"aisdlc/internal/gitx"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
)

// HookControlTest, phần cần outbox và sổ điều khiển: giao chỉ dẫn một lần, lệnh chờ do hook áp dụng,
// tạm dừng mở/chờ `web flush`, khóa control.lock bị giữ. Web bật nhưng không với tới được: event ở lại outbox.

// newHookControlBare: HOME tạm có khóa ghim (ký cache điều khiển), chưa làm gì trong repo.
func newHookControlBare(t *testing.T) *controlFx {
	r := newTestRepo(t)
	home := t.TempDir()
	r.env["HOME"] = home
	_, priv := pinKey(t, home)
	return &controlFx{r, priv, snapshot.RepoID(context.Background(), gitx.New(r.root), r.root)}
}

// signed là phong bì JSON của fc ký bằng khóa ghim (Repo mặc định là repo của test).
func (c *controlFx) signed(fc snapshot.FeatureControl) []byte {
	if fc.Repo == "" {
		fc.Repo = c.repoID
	}
	b, err := fc.ToJSON()
	if err != nil {
		panic(err)
	}
	env, err := jsonMarshal(snapshot.Sign(b, c.priv, "k"))
	if err != nil {
		panic(err)
	}
	return env
}

func (c *controlFx) cacheFile() string { return snapshot.ControlCacheFile(c.root, "PAY-1") }

func (c *controlFx) instructions(ins ...snapshot.Instruction) snapshot.FeatureControl {
	return snapshot.FeatureControl{Feature: "PAY-1", IssuedAt: testNow, Instructions: ins}
}

func (c *controlFx) readIn() string {
	return hookIn("PostToolUse", c.root, "Read", "file_path", filepath.Join(c.root, "go.mod"))
}

// events là các event type trong outbox.
func (c *controlFx) events(typ string) []map[string]any {
	var out []map[string]any
	for _, e := range c.outboxEvents() {
		if e["type"] == typ {
			out = append(out, e)
		}
	}
	return out
}

// endTree là endTree của bước S1 trong steps.json.
func (c *controlFx) endTree() string {
	c.t.Helper()
	var v []struct {
		ID      string `json:"id"`
		EndTree string `json:"endTree"`
	}
	must(c.t, json.Unmarshal([]byte(c.read("aisdlc/features/PAY-1/steps.json")), &v))
	for _, s := range v {
		if s.ID == "S1" && s.EndTree != "" {
			return s.EndTree
		}
	}
	c.t.Fatalf("không có endTree của S1: %s", c.read("aisdlc/features/PAY-1/steps.json"))
	return ""
}

func (c *controlFx) acceptAction(id int64) snapshot.Action {
	exp := testNow.Add(7 * 24 * time.Hour)
	return snapshot.Action{ID: id, Kind: "ACCEPT_STEP", Step: "S1", EndTree: c.endTree(), By: "an", At: testNow,
		ExpiresAt: &exp}
}

func (c *controlFx) setMtime(p string, t time.Time) { must(c.t, os.Chtimes(p, t, t)) }

func TestPausedDenySpawnsFlushThrottled(t *testing.T) {
	c := newHookControl(t)
	c.webDown()
	c.writeControl(c.paused(), c.priv)
	c.spawned = nil
	c.hook("pre-tool-use", c.codeEditEdit())
	contains(t, c.out, "deny")
	c.hook("pre-tool-use", c.codeEditEdit())
	contains(t, c.out, "deny")
	eq(t, strings.Join(c.spawned, ","), c.root)
	mark := filepath.Join(c.root, ".aisdlc/web/control/spawned")
	c.setMtime(mark, mtime(mark).Add(-6*time.Second))
	c.hook("pre-tool-use", c.codeEditEdit())
	eq(t, len(c.spawned), 2)
}

func TestInstructionDeliveredOnce(t *testing.T) {
	c := newHookControl(t)
	c.webDown()
	c.writeControl(c.instructions(snapshot.Instruction{ID: 41, By: "an", At: testNow,
		Text: "dùng BigDecimal cho hạn mức"}), c.priv)
	c.hook("post-tool-use", c.readIn())
	o := hookSpecific(t, c.out)
	eq(t, str(o, "hookEventName"), "PostToolUse")
	contains(t, str(o, "additionalContext"), "Chỉ dẫn của dev (", "dùng BigDecimal cho hạn mức")
	c.hook("post-tool-use", c.readIn())
	eq(t, c.out, "")
	ev := c.events("INSTRUCTION_DELIVERED")
	eq(t, len(ev), 1)
	eq(t, ev[0]["detail"], any("41"))
	eq(t, ev[0]["feature"], any("PAY-1"))
	eq(t, ev[0]["sessionId"], any("abc"))
	eq(t, c.exists(".aisdlc/web/control/inbox"), false)
}

// Phần event/giao một lần của sessionStartAppendsUndeliveredInstructions (carry-over 2b: không hiện lại mỗi phiên).
func TestSessionStartDeliversInstructionsOnce(t *testing.T) {
	c := newHookControl(t)
	c.webDown()
	c.writeControl(c.instructions(snapshot.Instruction{ID: 42, By: "an", At: testNow, Text: "đọc lại BRD"}), c.priv)
	c.hook("session-start", hookIn("SessionStart", c.root, "", "", ""))
	ctx := str(hookSpecific(t, c.out), "additionalContext")
	if !strings.HasPrefix(ctx, "Quy tắc AISDLC") || !strings.HasSuffix(ctx, "đọc lại BRD") {
		t.Fatalf("got %q", ctx)
	}
	eq(t, len(c.events("INSTRUCTION_DELIVERED")), 1)
	c.hook("session-start", hookIn("SessionStart", c.root, "", "", ""))
	contains(t, c.out, "Quy tắc AISDLC")
	notContains(t, c.out, "đọc lại BRD")
	eq(t, len(c.events("INSTRUCTION_DELIVERED")), 1)
}

func TestPendingAcceptAppliedByHook(t *testing.T) {
	c := newHookControlBare(t)
	c.s1AwaitingDev()
	c.webDown()
	c.writeControl(snapshot.FeatureControl{Feature: "PAY-1", IssuedAt: testNow,
		Actions: []snapshot.Action{c.acceptAction(77)}}, c.priv)
	in := hookIn("PreToolUse", c.root, "Write", "file_path", filepath.Join(c.root, "README2.txt"))
	c.hook("pre-tool-use", in)
	eq(t, c.err, "") // hook không in gì ra stderr
	contains(t, c.read("aisdlc/features/PAY-1/steps.json"), "ACCEPTED", "an (web)")
	ev := c.events("ACTION_RESULT")
	eq(t, len(ev), 1)
	var d map[string]any
	must(t, json.Unmarshal([]byte(ev[0]["detail"].(string)), &d))
	eq(t, d["id"], any(float64(77)))
	eq(t, d["status"], any("APPLIED"))
	eq(t, isNull(d, "reason"), true)
	eq(t, ev[0]["sessionId"], any("abc"))
	eq(t, ev[0]["step"], any("S1"))
	c.hook("pre-tool-use", in)
	eq(t, len(c.events("ACTION_RESULT")), 1)
}

func TestUserPromptSubmitDeliversOnceAndSpawnsFlush(t *testing.T) {
	c := newHookControl(t)
	c.webDown()
	// như Java (lệnh CLI trước đó gặp web chết): đang backoff nên event INSTRUCTION_DELIVERED không tự mở flush;
	// flush do UserPromptSubmit mở thì không xét backoff
	must(t, webclient.BackoffStart(c.root, testNow))
	c.writeControl(c.instructions(snapshot.Instruction{ID: 51, By: "an", At: testNow, Text: "giữ nguyên API cũ"}),
		c.priv)
	c.spawned = nil
	c.hook("user-prompt-submit", c.prompt(c.root))
	o := hookSpecific(t, c.out)
	eq(t, str(o, "hookEventName"), "UserPromptSubmit")
	contains(t, str(o, "additionalContext"), "Chỉ dẫn của dev (", "giữ nguyên API cũ")
	eq(t, strings.Join(c.spawned, ","), c.root)
	eq(t, c.exists(".aisdlc/web/control/spawned"), true)
	// tiến trình vừa mở chưa ghi lại cache (cache cũ hơn mốc spawned): không mở thêm; chỉ dẫn không giao lại
	c.setMtime(c.cacheFile(), testNow.Add(-10*time.Second))
	c.hook("user-prompt-submit", c.prompt(c.root))
	eq(t, c.out, "")
	eq(t, len(c.spawned), 1)
	// tiến trình đó đã ghi cache xong: prompt kế tiếp mở flush mới dù chưa đủ 5 giây
	c.setMtime(c.cacheFile(), testNow.Add(time.Second))
	c.hook("user-prompt-submit", c.prompt(c.root))
	eq(t, len(c.spawned), 2)
	ev := c.events("INSTRUCTION_DELIVERED")
	eq(t, len(ev), 1)
	eq(t, ev[0]["detail"], any("51"))
	eq(t, c.err, "")
}

// pausedInFlight: cache tạm dừng cũ (10 giây trước), UserPromptSubmit vừa mở web flush 1 giây trước.
func (c *controlFx) pausedInFlight() {
	c.writeControl(c.paused(), c.priv)
	c.setMtime(c.cacheFile(), testNow.Add(-10*time.Second))
	mark := filepath.Join(c.root, ".aisdlc/web/control/spawned")
	c.write(c.relOf(mark), "")
	c.setMtime(mark, testNow.Add(-time.Second))
}

func TestPausedDenyWaitsForInFlightFlushThatLiftsPause(t *testing.T) {
	c := newHookControl(t)
	c.webDown()
	c.pausedInFlight()
	c.spawned = nil
	// tiến trình gửi nền ghi cache (đã gỡ tạm dừng) sau 300 ms
	lifted := c.signed(snapshot.FeatureControl{Feature: "PAY-1", IssuedAt: testNow.Add(5 * time.Second)})
	done := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		done <- fsx.WriteAtomic(c.cacheFile(), lifted, 0o644)
	}()
	c.hook("pre-tool-use", c.codeEditEdit())
	must(t, <-done)
	eq(t, c.out, "") // chờ tiến trình gửi nền rồi xét lại: được phép
	eq(t, len(c.spawned), 0)
}

func TestPausedDenyStillDeniedWhenInFlightFlushKeepsPause(t *testing.T) {
	c := newHookControl(t)
	c.webDown()
	c.pausedInFlight()
	t0 := time.Now()
	c.hook("pre-tool-use", c.codeEditEdit())
	ms := time.Since(t0)
	if !strings.HasPrefix(str(hookSpecific(t, c.out), "permissionDecisionReason"),
		"Dev đã tạm dừng tại web: an. Dừng lại và chờ.") {
		t.Fatalf("got %s", c.out)
	}
	if ms < 1900*time.Millisecond || ms > 6*time.Second {
		t.Fatalf("chờ tối đa khoảng 2 giây: %v", ms)
	}
}

func TestSubagentToolCallDoesNotClaimInstructions(t *testing.T) {
	c := newHookControl(t)
	c.webDown()
	c.writeControl(c.instructions(snapshot.Instruction{ID: 61, By: "an", At: testNow, Text: "chỉ cho agent chính"}),
		c.priv)
	sub := hookJSON(map[string]any{"session_id": "abc", "hook_event_name": "PostToolUse", "cwd": c.root,
		"agent_id": "a-7", "agent_type": "aisdlc-reviewer"}, "Read", "file_path", filepath.Join(c.root, "go.mod"))
	c.hook("post-tool-use", sub)
	eq(t, c.out, "")
	eq(t, len(c.events("INSTRUCTION_DELIVERED")), 0)
	eq(t, len(NewControlLedger(c.root, "PAY-1", 0).Delivered()), 0)
	// agent chính (không có agent_id) nhận ở lần kế tiếp
	c.hook("post-tool-use", c.readIn())
	contains(t, c.out, "chỉ cho agent chính")
}

func TestHeldControlLockDeniesWriteAndSkipsOtherHooks(t *testing.T) {
	c := newHookControlBare(t)
	c.s1AwaitingDev()
	c.webDown()
	before := c.read("aisdlc/features/PAY-1/steps.json")
	c.writeControl(snapshot.FeatureControl{Feature: "PAY-1", IssuedAt: testNow,
		Instructions: []snapshot.Instruction{{ID: 71, By: "an", At: testNow, Text: "x"}},
		Actions:      []snapshot.Action{c.acceptAction(77)}}, c.priv)
	release := holdLock(t, controlLockFile(c.root))
	t0 := time.Now()
	c.hook("pre-tool-use", hookIn("PreToolUse", c.root, "Bash", "command", "ls")) // không khóa thì được phép
	if d := time.Since(t0); d >= 6*time.Second {
		t.Fatalf("treo tới timeout của hook: %v", d)
	}
	o := hookSpecific(t, c.out)
	eq(t, str(o, "permissionDecision"), "deny")
	contains(t, str(o, "permissionDecisionReason"), "control.lock", "Thử lại")
	eq(t, c.read("aisdlc/features/PAY-1/steps.json"), before)
	// hook khác: im lặng, không nhận chỉ dẫn, không chặn
	c.hook("post-tool-use", c.readIn())
	eq(t, c.out, "")
	c.hook("stop", hookIn("Stop", c.root, "", "", ""))
	notContains(t, c.out, "deny")
	release()
	eq(t, len(c.events("INSTRUCTION_DELIVERED")), 0)
	// khóa đã nhả: áp dụng bình thường
	c.hook("pre-tool-use", hookIn("PreToolUse", c.root, "Write", "file_path", filepath.Join(c.root, "README2.txt")))
	contains(t, c.read("aisdlc/features/PAY-1/steps.json"), "an (web)")
}
