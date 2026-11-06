package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/snapshot"
)

// HookCommandTest. Phần event web (outbox, flush nền) ở hook_web_test.go.

// codePath là file trong phạm vi S1 của design.yaml mẫu.
const codePath = "internal/limit/handler/a.go"

// hookIn là input JSON của hook (session "abc"); tool rỗng thì không có tool_name/tool_input.
func hookIn(event, cwd, tool, field, value string) string {
	return hookJSON(map[string]any{"session_id": "abc", "hook_event_name": event, "cwd": cwd}, tool, field, value)
}

func hookJSON(base map[string]any, tool, field, value string) string {
	if tool != "" {
		base["tool_name"] = tool
		base["tool_input"] = map[string]any{field: value}
	}
	b, err := json.Marshal(base)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// hookSpecific là hookSpecificOutput trong stdout của hook (map rỗng nếu hook im lặng).
func hookSpecific(t *testing.T, out string) map[string]any {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		return map[string]any{}
	}
	var o map[string]any
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatalf("stdout không phải JSON: %v\n%s", err, out)
	}
	m, _ := o["hookSpecificOutput"].(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// hook chạy `aisdlc hook <event>` với stdin và đòi exit 0 (HookCommand của Java luôn exit 0).
func (r *testRepo) hook(event, stdin string) {
	r.t.Helper()
	if code := r.runWithStdin(stdin, "hook", event); code != 0 {
		r.t.Fatalf("hook %s: exit %d\n%s%s", event, code, r.out, r.err)
	}
}

func (r *testRepo) codeEdit() string {
	return hookIn("PreToolUse", r.root, "Write", "file_path", filepath.Join(r.root, codePath))
}

func TestNoAisdlcRepoIsSilent(t *testing.T) {
	plain := newTestRepo(t)
	outside := plain.root
	plain.hook("pre-tool-use", hookIn("PreToolUse", outside, "Write", "file_path", filepath.Join(outside, "a.go")))
	eq(t, plain.out, "")
	plain.hook("pre-tool-use", `{"tool_name":"Write",`)
	eq(t, plain.out, "")
	plain.hook("stop", hookIn("Stop", outside, "", "", ""))
	eq(t, plain.out, "")
	plain.hook("session-start", hookIn("SessionStart", outside, "", "", ""))
	eq(t, plain.out, "")
	eq(t, plain.err, "")
}

func TestCwdFromJsonDecidesRepo(t *testing.T) {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	outside := t.TempDir()
	// tiến trình chạy trong repo AISDLC nhưng JSON nói cwd ở chỗ khác -> no-op
	r.hook("pre-tool-use", hookIn("PreToolUse", outside, "Write", "file_path", filepath.Join(outside, "a.go")))
	eq(t, r.out, "")
}

func TestMalformedJsonInAisdlcRepoDeniesWrites(t *testing.T) {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	r.hook("pre-tool-use", `{"tool_name":"Write",`)
	contains(t, r.out, `"permissionDecision":"deny"`, "aisdlc lỗi: không đọc được JSON của hook (")
	r.hook("pre-tool-use", `[1, 2]`)
	contains(t, r.out, `"permissionDecision":"deny"`, "không phải object JSON")
	r.hook("stop", `{"tool_name":"Write",`)
	eq(t, r.out, "")
	r.hook("session-start", `{"tool_name":"Write",`)
	eq(t, r.out, "")
}

func TestDenyJsonShape(t *testing.T) {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	r.hook("pre-tool-use", r.codeEdit())
	// đúng thứ tự khóa và dạng gọn như Jackson, một dòng
	if !strings.HasPrefix(r.out, `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny",`+
		`"permissionDecisionReason":"Chưa có tính năng đang làm`) || !strings.HasSuffix(r.out, "\"}}\n") ||
		strings.Count(r.out, "\n") != 1 {
		t.Fatalf("got %q", r.out)
	}
	o := hookSpecific(t, r.out)
	eq(t, str(o, "hookEventName"), "PreToolUse")
	eq(t, str(o, "permissionDecision"), "deny")
	contains(t, str(o, "permissionDecisionReason"), "Chưa có tính năng đang làm", "`aisdlc use <KEY>`")
}

func TestAllowedCallPrintsNothing(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "Edit", "file_path",
		filepath.Join(r.root, "aisdlc/features/PAY-1/brd.md")))
	eq(t, r.out, "")
	r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "Read", "file_path", filepath.Join(r.root, "go.mod")))
	eq(t, r.out, "")
}

func TestNotebookPathAndBashCommandAreChecked(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "NotebookEdit", "notebook_path",
		filepath.Join(r.root, "nb.ipynb")))
	contains(t, r.out, `"permissionDecision":"deny"`, "BRD")
	r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "Bash", "command", "aisdlc approve brd"))
	contains(t, r.out, `"permissionDecision":"deny"`, "chỉ dev")
}

func TestSessionStartIncludesStatus(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.hook("session-start", hookIn("SessionStart", r.root, "", "", ""))
	if !strings.HasPrefix(r.out, `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"`) {
		t.Fatalf("got %q", r.out)
	}
	o := hookSpecific(t, r.out)
	eq(t, str(o, "hookEventName"), "SessionStart")
	contains(t, str(o, "additionalContext"), "Quy tắc AISDLC", "PAY-1", "BRD=NOT_SUBMITTED")
}

func TestStopBlocksWhileStepInProgressUnlessAlreadyActive(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.hook("stop", hookIn("Stop", r.root, "", "", ""))
	if !strings.HasPrefix(r.out, `{"decision":"block","reason":"`) {
		t.Fatalf("got %q", r.out)
	}
	var o map[string]any
	must(t, json.Unmarshal([]byte(r.out), &o))
	eq(t, str(o, "decision"), "block")
	contains(t, str(o, "reason"), "aisdlc step finish S1")

	r.hook("stop", hookJSON(map[string]any{"session_id": "abc", "cwd": r.root, "stop_hook_active": true}, "", "", ""))
	eq(t, r.out, "")
}

func TestCorruptGatesJsonDeniesWritesButOtherEventsAreSilent(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.write("aisdlc/features/PAY-1/gates.json", "{hỏng")
	r.hook("pre-tool-use", r.codeEdit())
	contains(t, r.out, `"permissionDecision":"deny"`, "aisdlc lỗi: gates.json hỏng")
	r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "Read", "file_path", filepath.Join(r.root, "go.mod")))
	eq(t, r.out, "")
	r.hook("session-start", hookIn("SessionStart", r.root, "", "", ""))
	eq(t, r.out, "")
	r.hook("stop", hookIn("Stop", r.root, "", "", ""))
	eq(t, r.out, "")
}

func TestActiveFeatureMissingOnThisBranchIsNoActiveFeature(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	// đổi sang nhánh chưa có PAY-1: .aisdlc/active (gitignore) vẫn trỏ PAY-1
	must(t, os.RemoveAll(filepath.Join(r.root, "aisdlc/features/PAY-1")))
	for _, cmd := range []string{"aisdlc init PAY-2", "ls", "aisdlc use PAY-3"} {
		r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "Bash", "command", cmd))
		eq(t, r.out, "")
	}
	r.hook("pre-tool-use", r.codeEdit())
	reason := str(hookSpecific(t, r.out), "permissionDecisionReason")
	contains(t, reason, "aisdlc use <KEY>", "/aisdlc:init")
	notContains(t, reason, "aisdlc lỗi")

	r.hook("session-start", hookIn("SessionStart", r.root, "", "", ""))
	contains(t, str(hookSpecific(t, r.out), "additionalContext"), "Quy tắc AISDLC")
	r.hook("stop", hookIn("Stop", r.root, "", "", ""))
	eq(t, r.out, "")
}

func TestErrorNotJustRuntimeExceptionIsHandledOnEveryHook(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.git = func(string) *gitx.Repo { panic("aisdlc/internal/gitx: không nạp được") }
	r.hook("stop", hookIn("Stop", r.root, "", "", ""))
	eq(t, r.out, "")
	r.hook("session-start", hookIn("SessionStart", r.root, "", "", ""))
	eq(t, r.out, "")
	r.hook("pre-tool-use", r.codeEdit())
	contains(t, r.out, `"permissionDecision":"deny"`, "aisdlc lỗi: aisdlc/internal/gitx: không nạp được")
}

func TestPostToolUseDisabledWebWritesNothing(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.hook("post-tool-use", hookIn("PostToolUse", r.root, "Write", "file_path", filepath.Join(r.root, "README2.txt")))
	eq(t, r.out, "")
	eq(t, r.exists(".aisdlc/web"), false)
	r.hook("user-prompt-submit", hookJSON(map[string]any{"session_id": "abc", "cwd": r.root, "prompt": "x"}, "", "", ""))
	eq(t, r.out, "")
	eq(t, r.exists(".aisdlc/web"), false)
	eq(t, len(r.spawned), 0)
}

func TestPostToolUseOutsideAisdlcRepoSilent(t *testing.T) {
	plain := newTestRepo(t)
	plain.webDown()
	plain.hook("post-tool-use", hookIn("PostToolUse", plain.root, "Write", "file_path", filepath.Join(plain.root, "a.go")))
	eq(t, plain.out, "")
	eq(t, plain.err, "")
	eq(t, plain.exists(".aisdlc"), false)
	// trong repo nhưng file ngoài repo: bỏ qua
	r := newTestRepo(t)
	r.casesApproved()
	r.webDown()
	r.hook("post-tool-use", hookIn("PostToolUse", r.root, "Write", "file_path", filepath.Join(plain.root, "a.go")))
	eq(t, len(r.outboxEvents()), 0)
}

func TestSessionStartStillOutputsContextAndEmitsEvent(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.webDown()
	r.hook("session-start", hookIn("SessionStart", r.root, "", "", ""))
	contains(t, str(hookSpecific(t, r.out), "additionalContext"), "Quy tắc AISDLC", "PAY-1")
	ev := r.outboxEvents()
	eq(t, len(ev), 1)
	eq(t, ev[0]["type"], any("SESSION_START"))
	eq(t, ev[0]["feature"], any("PAY-1"))
	eq(t, isNull(ev[0], "step"), true)
}

func TestStopBlockedEmitsNoSessionStopButAllowedDoes(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.webDown()
	r.hook("stop", hookIn("Stop", r.root, "", "", ""))
	contains(t, r.out, `"decision":"block"`)
	eq(t, len(r.outboxEvents()), 0)
	r.hook("stop", hookJSON(map[string]any{"session_id": "abc", "cwd": r.root, "stop_hook_active": true}, "", "", ""))
	eq(t, r.out, "")
	ev := r.outboxEvents()
	eq(t, len(ev), 1)
	eq(t, ev[0]["type"], any("SESSION_STOP"))
}

// ---- ~/.config/aisdlc và chế độ duyệt trên web ----

func TestWebConfigDirIsOffLimitsForEveryTool(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	home := t.TempDir()
	r.env["HOME"] = home
	cfg := filepath.Join(home, ".config/aisdlc")
	must(t, os.MkdirAll(cfg, 0o700))
	must(t, os.WriteFile(filepath.Join(cfg, "web.json"), []byte("{}"), 0o600))
	reason := "File cấu hình web của dev, agent không được đọc/ghi"
	outside := t.TempDir()
	for _, c := range []struct{ cwd, tool, field, value string }{
		{r.root, "Read", "file_path", filepath.Join(cfg, "web.json")},
		{r.root, "Grep", "path", cfg},
		{r.root, "Glob", "pattern", "~/.config/aisdlc/*"},
		{r.root, "Bash", "command", "cat ~/.config/aisdlc/approve-token"},
		{r.root, "Write", "file_path", filepath.Join(cfg, "approve-token")},
		{outside, "Read", "file_path", filepath.Join(cfg, "web.json")}, // ngoài repo AISDLC cũng chặn
	} {
		r.hook("pre-tool-use", hookIn("PreToolUse", c.cwd, c.tool, c.field, c.value))
		contains(t, r.out, `"permissionDecision":"deny"`, reason)
	}
	// tên cũ của hook cho tool đọc vẫn chạy
	r.hook("pre-tool-use-read", hookIn("PreToolUse", r.root, "Read", "file_path", filepath.Join(cfg, "web.json")))
	contains(t, r.out, reason)
	// file thường: Read/Grep/Glob vẫn tự do
	for _, c := range []struct{ tool, field, value string }{
		{"Read", "file_path", filepath.Join(r.root, "go.mod")},
		{"Grep", "path", filepath.Join(r.root, "src")},
		{"Glob", "pattern", "**/*.go"},
		{"Grep", "pattern", ".*"}, // pattern của Grep là regex tìm trong nội dung, không phải đường dẫn
	} {
		r.hook("pre-tool-use", hookIn("PreToolUse", r.root, c.tool, c.field, c.value))
		eq(t, r.out, "")
	}
}

func TestBashReadingWebTokenFromEnvironmentDenied(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.env["HOME"] = t.TempDir()
	for _, cmd := range []string{"echo $AISDLC_WEB_TOKEN", "env", "printenv"} {
		r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "Bash", "command", cmd))
		contains(t, r.out, `"permissionDecision":"deny"`, "biến môi trường")
	}
	r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "Bash", "command", "env FOO=1 git status"))
	notContains(t, r.out, "biến môi trường")
}

// webModeWithSignedCache: repo đã duyệt tới CASES ở local, S1 đang chạy, chuyển sang chế độ web với cache ký từ
// approval cục bộ. Trả đường dẫn cache.
func (r *testRepo) webModeWithSignedCache() string {
	r.t.Helper()
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	home := r.t.TempDir()
	r.env["HOME"] = home
	_, priv := pinKey(r.t, home)
	f := r.feature()
	var subs []snapshot.Sub
	var apprs []snapshot.Appr
	for _, g := range []gate.Gate{gate.BRD, gate.DESIGN, gate.CASES} {
		a, ok, err := f.Store.Approval(g)
		must(r.t, err)
		if !ok {
			r.t.Fatalf("thiếu approval %s", g)
		}
		at := a.At
		subs = append(subs, snapshot.Sub{Gate: string(g), Hash: a.Hash, At: at})
		apprs = append(apprs, snapshot.Appr{Gate: string(g), Hash: a.Hash, Approver: "dev", At: at, Channel: "web"})
	}
	repo := snapshot.RepoID(context.Background(), gitx.New(r.root), r.root)
	st := snapshot.FeatureGateState{Repo: repo, Feature: "PAY-1", Mode: "web", IssuedAt: testNow,
		Submissions: subs, Approvals: apprs}
	b, err := st.ToJSON()
	must(r.t, err)
	cache := ".aisdlc/web/gates/PAY-1.json"
	r.writeSigned(cache, b, priv)
	r.write("aisdlc/config.yaml", r.read("aisdlc/config.yaml")+"approvals: web\n")
	return filepath.Join(r.root, cache)
}

func TestWebModeHookUsesSignedCacheOnly(t *testing.T) {
	r := newTestRepo(t)
	cache := r.webModeWithSignedCache()
	r.hook("pre-tool-use", r.codeEdit())
	eq(t, r.out, "") // cache hợp lệ: được sửa code trong phạm vi

	good, err := os.ReadFile(cache)
	must(t, err)
	bad := append([]byte(nil), good...)
	i := len(bad) / 2
	if bad[i] == 'A' {
		bad[i] = 'B'
	} else {
		bad[i] = 'A'
	}
	must(t, os.WriteFile(cache, bad, 0o644))
	r.hook("pre-tool-use", r.codeEdit())
	contains(t, r.out, `"permissionDecision":"deny"`, "Chưa được viết code")

	must(t, os.Remove(cache))
	r.hook("pre-tool-use", r.codeEdit())
	contains(t, r.out, `"permissionDecision":"deny"`, "Chưa được viết code")
	// gates.json cục bộ vẫn còn approval nhưng không được dùng ở chế độ web
	contains(t, r.read("aisdlc/features/PAY-1/gates.json"), "CASES")
}

// FakeWeb của Java thay bằng httptest: chỉ đếm request (hook không được gọi mạng).
func TestWebModeHookNeverCallsNetworkEvenWithWebConfigured(t *testing.T) {
	r := newTestRepo(t)
	r.webModeWithSignedCache()
	var requests atomic.Int64
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(500)
	}))
	defer web.Close()
	r.env["AISDLC_WEB_URL"] = web.URL
	r.env["AISDLC_WEB_TOKEN"] = "t0k"
	_ = os.Remove(filepath.Join(r.root, ".aisdlc/web/mode.json"))
	r.hook("pre-tool-use", r.codeEdit())
	eq(t, r.out, "")
	r.hook("session-start", hookIn("SessionStart", r.root, "", "", ""))
	r.hook("stop", hookIn("Stop", r.root, "", "", ""))
	r.hook("post-tool-use", hookIn("PostToolUse", r.root, "Write", "file_path", filepath.Join(r.root, codePath)))
	r.hook("user-prompt-submit", hookJSON(map[string]any{"session_id": "abc", "cwd": r.root, "prompt": "x"}, "", "", ""))
	eq(t, requests.Load(), int64(0))
}

func TestPreToolUseFastInWebMode(t *testing.T) {
	if testing.Short() {
		t.Skip("đo thời gian")
	}
	r := newTestRepo(t)
	r.webModeWithSignedCache()
	web := r.medianHook(r.codeEdit())
	r.write("aisdlc/config.yaml", strings.Replace(r.read("aisdlc/config.yaml"), "approvals: web\n", "", 1))
	local := r.medianHook(r.codeEdit())
	if web >= 300*time.Millisecond || local >= 300*time.Millisecond {
		t.Fatalf("trung vị: web %v, local %v (phải < 300ms)", web, local)
	}
}

func (r *testRepo) medianHook(in string) time.Duration {
	r.t.Helper()
	ds := make([]time.Duration, 7)
	for i := range ds {
		t0 := time.Now()
		r.hook("pre-tool-use", in)
		ds[i] = time.Since(t0)
		eq(r.t, r.out, "")
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	return ds[len(ds)/2]
}

// ---- thêm theo brief ----

// Hàng rào Bash chạy hết đường hook: cwd của input là gốc repo, lệnh cd vào aisdlc rồi ghi config.yaml. HookCommand
// của Java chặn bằng JSON deny và exit 0 (Claude Code bỏ qua JSON khi exit 2).
func TestHookBashHardeningEndToEnd(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	before := r.read("aisdlc/config.yaml")
	r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "Bash", "command", "cd aisdlc && echo x > config.yaml"))
	o := hookSpecific(t, r.out)
	eq(t, str(o, "permissionDecision"), "deny")
	contains(t, str(o, "permissionDecisionReason"), "config.yaml")
	// cwd của input là thư mục con: đường dẫn tương đối tính từ đó
	r.hook("pre-tool-use", hookIn("PreToolUse", filepath.Join(r.root, "aisdlc"), "Bash", "command",
		"echo x > config.yaml"))
	eq(t, str(hookSpecific(t, r.out), "permissionDecision"), "deny")
	r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "Bash", "command", "echo x > aisdlc/notes.txt"))
	eq(t, r.out, "")
	eq(t, r.read("aisdlc/config.yaml"), before)
}

func TestHookWithoutEventPrintsUsageExit2(t *testing.T) {
	r := newTestRepo(t)
	eq(t, r.run("hook"), 2)
	contains(t, r.err, "pre-tool-use", "session-start", "stop")
	eq(t, r.runWithStdin("{}", "hook", "nope"), 2)
}

func TestHelpListsHook(t *testing.T) {
	r := newTestRepo(t)
	eq(t, r.run("--help"), 0)
	contains(t, r.out, "hook")
}

// JSON hỏng: Java quyết "trong repo AISDLC" theo cwd của tiến trình; cwd trích bằng regex (ngoài repo) không được
// làm mất lớp chặn đó.
func TestMalformedJsonDeniesWhenProcessCwdInRepoEvenIfExtractedCwdOutside(t *testing.T) {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	outside := t.TempDir()
	r.hook("pre-tool-use", `{"session_id":"abc","cwd":"`+outside+`","tool_name":"Write",`)
	contains(t, r.out, `"permissionDecision":"deny"`, "aisdlc lỗi: không đọc được JSON của hook (")
}

// panicReader giả lỗi ngoài phần recover riêng của sự kiện (lúc đọc stdin, trước khi biết repo).
type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("stdin hỏng") }

// Panic lọt khỏi phần recover của sự kiện: Stop và các sự kiện khác im lặng exit 0, PreToolUse vẫn đóng (exit 2).
func TestHookPanicOutsideEventRecoverIsSilentExceptPreToolUse(t *testing.T) {
	r := newTestRepo(t)
	for _, event := range []string{"stop", "session-start", "post-tool-use", "user-prompt-submit"} {
		var o, e bytes.Buffer
		ctx := r.context(noConsole(), "", &o, &e)
		ctx.in = panicReader{}
		eq(t, runWith(ctx, nil, "hook", event), 0)
		eq(t, o.String(), "")
		eq(t, e.String(), "")
	}
	var o, e bytes.Buffer
	ctx := r.context(noConsole(), "", &o, &e)
	ctx.in = panicReader{}
	eq(t, runWith(ctx, nil, "hook", "pre-tool-use"), 2)
	contains(t, e.String(), "Lỗi: stdin hỏng")
}
