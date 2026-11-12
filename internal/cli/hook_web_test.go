package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
	"aisdlc/internal/webclient/fakeweb"
)

// Sự kiện web của hook và lệnh (HookCommandTest phần post-tool-use, WebPublisherTest phần chạy lệnh, event rủi ro).

// closedPort là cổng TCP vừa đóng: web "bật nhưng chết" (kết nối bị từ chối ngay).
func closedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	addr := l.Addr().String()
	must(t, l.Close())
	return "http://" + addr
}

// webDown bật web trỏ tới cổng đóng: event ở lại outbox để test đọc.
func (r *testRepo) webDown() {
	r.env["AISDLC_WEB_URL"] = closedPort(r.t)
	r.env["AISDLC_WEB_TOKEN"] = "t0k"
}

// webOff tắt web (bỏ biến cấu hình).
func (r *testRepo) webOff() {
	delete(r.env, "AISDLC_WEB_URL")
	delete(r.env, "AISDLC_WEB_TOKEN")
}

// outboxEvents là các event trong outbox theo thứ tự tên file (= thứ tự ghi).
func (r *testRepo) outboxEvents() []map[string]any {
	r.t.Helper()
	dir := filepath.Join(r.root, ".aisdlc/web/outbox")
	es, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	must(r.t, err)
	var names []string
	for _, e := range es {
		if strings.HasPrefix(e.Name(), "event-") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var out []map[string]any
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		must(r.t, err)
		var m map[string]any
		must(r.t, json.Unmarshal(b, &m))
		out = append(out, m)
	}
	return out
}

func (r *testRepo) postToolUse(tool, field, value string) {
	r.t.Helper()
	r.hook("post-tool-use", hookIn("PostToolUse", r.root, tool, field, value))
}

func isNull(m map[string]any, k string) bool {
	v, ok := m[k]
	return ok && v == nil
}

func TestPostToolUseEmitsFileEditWithScope(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.webDown()
	r.postToolUse("Write", "file_path", filepath.Join(r.root, "README2.txt"))
	eq(t, r.out, "")
	r.postToolUse("Write", "file_path", filepath.Join(r.root, codePath))
	eq(t, r.out, "")
	ev := r.outboxEvents()
	eq(t, len(ev), 2)
	eq(t, ev[0]["type"], any("FILE_EDIT"))
	eq(t, ev[0]["feature"], any("PAY-1"))
	eq(t, ev[0]["step"], any("S1"))
	eq(t, ev[0]["path"], any("README2.txt"))
	eq(t, ev[0]["outOfScope"], any(true))
	eq(t, ev[0]["sessionId"], any("abc"))
	eq(t, ev[0]["sender"], any("Dev Một"))
	eq(t, ev[1]["path"], any(codePath))
	eq(t, ev[1]["outOfScope"], any(false))
}

func TestPostToolUseBashOnlyWhenStepRunning(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.webDown()
	r.postToolUse("Bash", "command", "go test ./...")
	eq(t, len(r.outboxEvents()), 0)
	r.webOff() // bắt đầu bước khi web tắt để outbox chỉ có event của hook
	r.ok(noConsole(), "step", "start", "S1")
	r.webDown()
	long := "echo " + strings.Repeat("x", 300)
	r.postToolUse("Bash", "command", long)
	eq(t, r.out, "")
	ev := r.outboxEvents()
	eq(t, len(ev), 1)
	eq(t, ev[0]["step"], any("S1"))
	eq(t, isNull(ev[0], "path"), true)
	eq(t, ev[0]["detail"], any(long[:120]))
}

func TestPostToolUseFastWhenWebDown(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.webDown()
	in := hookIn("PostToolUse", r.root, "Write", "file_path", filepath.Join(r.root, "README2.txt"))
	ds := make([]time.Duration, 5)
	for i := range ds {
		t0 := time.Now()
		r.hook("post-tool-use", in)
		ds[i] = time.Since(t0)
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	if ds[2] >= 300*time.Millisecond {
		t.Fatalf("trung vị %v >= 300ms", ds[2])
	}
	eq(t, r.out, "")
}

func TestPostToolUseSpawnsBackgroundFlushUnlessBackoff(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.webDown()
	r.spawned = nil
	r.postToolUse("Write", "file_path", filepath.Join(r.root, "README2.txt"))
	eq(t, strings.Join(r.spawned, ","), r.root)
	eq(t, r.exists(".aisdlc/web/backoff-until"), false) // hook không tự gửi mạng
	r.write(".aisdlc/web/backoff-until", "2026-11-02T03:01:00Z")
	r.postToolUse("Write", "file_path", filepath.Join(r.root, "README2.txt"))
	r.hook("stop", hookJSON(map[string]any{"session_id": "abc", "cwd": r.root, "stop_hook_active": true}, "", "", ""))
	eq(t, len(r.spawned), 1)
	eq(t, len(r.outboxEvents()), 3)
}

func TestPostToolUseWithoutActiveFeatureEmitsUntaggedEditButNoBash(t *testing.T) {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	r.webDown()
	r.postToolUse("Write", "file_path", filepath.Join(r.root, "README2.txt"))
	r.postToolUse("Bash", "command", "ls")
	ev := r.outboxEvents()
	eq(t, len(ev), 1)
	eq(t, ev[0]["type"], any("FILE_EDIT"))
	eq(t, isNull(ev[0], "feature"), true)
	eq(t, isNull(ev[0], "step"), true)
	eq(t, ev[0]["outOfScope"], any(false))
	eq(t, ev[0]["path"], any("README2.txt"))
}

// Hook không bao giờ chờ web: web treo (nhận kết nối, không trả lời) thì post-tool-use vẫn trả về ngay, vì hook chỉ
// ghi outbox và mở tiến trình gửi nền (bản chạy binary thật, mở `web flush` thật: cmd/aisdlc).
func TestPostToolUseFastWhenWebHangs(t *testing.T) {
	web := fakeweb.New(t)
	web.Update(func(s *fakeweb.Server) { s.Hang = true })
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "t0k"
	t0 := time.Now()
	r.postToolUse("Write", "file_path", filepath.Join(r.root, codePath))
	if d := time.Since(t0); d >= 200*time.Millisecond {
		t.Fatalf("post-tool-use mất %v (phải < 200ms)", d)
	}
	eq(t, r.out, "")
	eq(t, r.err, "")
	eq(t, len(web.Requests()), 0)
	eq(t, len(r.outboxEvents()), 1)
}

// Lệnh CLI gửi ngay nhưng tối đa 2 giây tổng: web treo thì lệnh vẫn xong đúng hạn, stdout và mã thoát như khi web tắt.
func TestCommandFinishesWhenWebHangs(t *testing.T) {
	web := fakeweb.New(t)
	web.Update(func(s *fakeweb.Server) { s.Hang = true })
	r := newTestRepo(t)
	r.casesApproved()
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "t0k"
	t0 := time.Now()
	r.ok(noConsole(), "step", "start", "S1")
	if d := time.Since(t0); d >= webclient.InlineDeadline+time.Second {
		t.Fatalf("step start mất %v", d)
	}
	contains(t, r.out, "Bắt đầu bước S1")
	notContains(t, r.out, "aisdlc web")
}

func TestSessionIdOnEvents(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.webDown()
	r.hook("post-tool-use", hookJSON(map[string]any{"session_id": "s-1", "cwd": r.root}, "Write", "file_path",
		filepath.Join(r.root, codePath)))
	ev := r.outboxEvents()
	eq(t, len(ev), 1)
	eq(t, ev[0]["sessionId"], any("s-1"))
}

// Port phần SCOPE_DENIED của HookControlTest.scopeDeniedEvent.
func TestScopeDeniedEvent(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.webDown()
	r.spawned = nil
	r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "Write", "file_path", filepath.Join(r.root, "README2.txt")))
	contains(t, r.out, "deny", "Ngoài phạm vi")
	ev := r.outboxEvents()
	eq(t, len(ev), 1)
	eq(t, ev[0]["type"], any("SCOPE_DENIED"))
	eq(t, ev[0]["path"], any("README2.txt"))
	eq(t, ev[0]["step"], any("S1"))
	eq(t, ev[0]["feature"], any("PAY-1"))
	eq(t, ev[0]["outOfScope"], any(true))
	eq(t, len(r.spawned) > 0, true)
}

// ---- lệnh CLI (WebPublisherTest phần chạy lệnh) ----

func TestEventGoesToOutboxAndStdoutStaysClean(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.env["AISDLC_WEB_URL"] = "http://127.0.0.1:1"
	r.env["AISDLC_WEB_TOKEN"] = "t0k"
	o, e := &bytes.Buffer{}, &bytes.Buffer{}
	ctx := r.context(noConsole(), "", o, e)
	ctx.Publisher(r.root).Emit(webclient.Event{Type: "FILE_EDIT", At: testNow, Path: "a.go"})
	eq(t, webclient.NewOutbox(r.root).Size(), 1)
	eq(t, o.String(), "")
}

func TestPublisherErrorsGoToStderrOnly(t *testing.T) {
	web := fakeweb.New(t)
	r := newTestRepo(t)
	r.setupInit()
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "t0k"
	o, e := &bytes.Buffer{}, &bytes.Buffer{}
	ctx := r.context(noConsole(), "", o, e)
	ctx.AfterCommandKey(r.root, "NOPE-9", "init") // snapshot không dựng được: tính năng không có trên đĩa
	eq(t, o.String(), "")
	if !strings.HasPrefix(e.String(), "aisdlc web: ") {
		t.Fatalf("stderr: %q", e.String())
	}
}

func TestDisabledWritesNothing(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.ok(noConsole(), "use", "PAY-1")
	eq(t, r.exists(".aisdlc/web"), false)
	ctx := r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{})
	eq(t, ctx.Publisher(r.root).Enabled(), false)
}

func TestServerDownKeepsQueueAndExitCodeUnchanged(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	disabled := r.run("submit", "brd")
	eq(t, disabled, 1)
	disabledOut := r.out
	r.webDown()
	t0 := time.Now()
	code := r.run("submit", "brd")
	ms := time.Since(t0)
	eq(t, code, disabled)
	eq(t, r.out, disabledOut)
	if ms >= 3*time.Second {
		t.Fatalf("submit mất %v", ms)
	}
	files := webclient.NewOutbox(r.root).List(100)
	snaps := 0
	for _, f := range files {
		if webclient.IsSnapshotFile(f) {
			snaps++
		}
	}
	eq(t, snaps, 1)
	eq(t, len(files)-snaps, 1)
}

func TestCommandsSendCommandEventAndSnapshot(t *testing.T) {
	web := fakeweb.New(t)
	r := newTestRepo(t)
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "t0k"
	r.setupInit()
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	r.ok(typed("PAY-1"), "approve", "brd")
	var cmds []string
	for _, q := range web.RequestsFor("POST", "/api/ingest/events") {
		eq(t, q.Auth, "Bearer t0k")
		var b struct {
			Version int              `json:"version"`
			Events  []map[string]any `json:"events"`
		}
		must(t, q.JSON(&b))
		eq(t, b.Version, 2)
		for _, e := range b.Events {
			if e["type"] == "COMMAND" {
				eq(t, e["feature"], any("PAY-1"))
				eq(t, e["sender"], any("Dev Một"))
				eq(t, e["at"], any("2026-11-02T03:00:00Z"))
				_, has := e["sessionId"]
				eq(t, has, false)
				cmds = append(cmds, e["detail"].(string))
			}
		}
	}
	eq(t, strings.Join(cmds, "|"), "init|submit brd|approve brd")
	snaps := web.RequestsFor("POST", "/api/ingest/snapshot")
	if len(snaps) == 0 {
		t.Fatal("không có snapshot")
	}
	var s map[string]any
	must(t, snaps[len(snaps)-1].JSON(&s))
	f, _ := s["feature"].(map[string]any)
	eq(t, f["key"], any("PAY-1"))
	eq(t, webclient.NewOutbox(r.root).Size(), 0)
}

func TestWebFlushCommandDrainsSilently(t *testing.T) {
	web := fakeweb.New(t)
	r := newTestRepo(t)
	r.setupInit()
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "t0k"
	box := webclient.NewOutbox(r.root)
	for i := 0; i < 3; i++ {
		must(t, box.PutEvent([]byte(`{"type":"FILE_EDIT"}`)))
	}
	eq(t, r.run("web", "flush"), 0)
	eq(t, r.out, "")
	eq(t, r.err, "")
	eq(t, len(web.RequestsFor("POST", "/api/ingest/events")), 3)
	eq(t, box.Size(), 0)
}

// web flush im lặng và exit 0 cả khi web chết, cấu hình hỏng, ngoài repo.
func TestWebFlushSilentAndExitZeroOnErrors(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	home := t.TempDir()
	r.env["HOME"] = home
	must(t, os.MkdirAll(webclient.ConfigDir(home), 0o700))
	must(t, os.WriteFile(webclient.ConfigFile(home), []byte("{hỏng"), 0o600))
	r.webDown()
	must(t, webclient.NewOutbox(r.root).PutEvent([]byte(`{"type":"FILE_EDIT"}`)))
	eq(t, r.run("web", "flush"), 0)
	eq(t, r.out, "")
	eq(t, r.err, "")
	eq(t, webclient.NewOutbox(r.root).Size(), 1)

	plain := newTestRepo(t)
	plain.webDown()
	eq(t, plain.run("web", "flush"), 0)
	eq(t, plain.out+plain.err, "")
	eq(t, plain.exists(".aisdlc"), false)
}

// Phần event của RiskCommandTest.lowerWithReason_writesOverrideAndEvent: web bật (gửi event) nhưng chưa login.
func TestLowerWithReasonSendsRiskEvents(t *testing.T) {
	web := fakeweb.New(t)
	r := bankingRepo(t)
	r.env["HOME"] = filepath.Join(r.root, "home")
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "ingest-tok"
	r.brdApproved()
	r.llmLevel = "high"
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "design")
	var cl map[string]any
	must(t, json.Unmarshal([]byte(eventDetail(t, web, "RISK_CLASSIFIED")), &cl))
	eq(t, cl["level"], any("high"))
	if _, ok := cl["policyVersion"].(float64); !ok {
		t.Fatalf("policyVersion: %v", cl)
	}
	if m, ok := cl["matched"].([]any); !ok || len(m) == 0 {
		t.Fatalf("matched: %v", cl)
	}
	if c := r.runC(typed("PAY-1"), "risk", "set", "medium", "--reason", "Chỉ đọc sổ cái"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	eq(t, r.risk().Level, "medium")
	eq(t, eventDetail(t, web, "RISK_OVERRIDDEN"), `{"from":"high","to":"medium","reason":"Chỉ đọc sổ cái"}`)
}

// eventDetail là detail của event type đầu tiên web nhận được.
func eventDetail(t *testing.T, web *fakeweb.Server, typ string) string {
	t.Helper()
	for _, q := range web.RequestsFor("POST", "/api/ingest/events") {
		var b struct {
			Events []map[string]any `json:"events"`
		}
		must(t, q.JSON(&b))
		for _, e := range b.Events {
			if e["type"] == typ {
				d, _ := e["detail"].(string)
				return d
			}
		}
	}
	t.Fatalf("không có event %s", typ)
	return ""
}

func (r *testRepo) prompt(cwd string) string {
	return hookJSON(map[string]any{"session_id": "s-1", "cwd": cwd, "hook_event_name": "UserPromptSubmit",
		"prompt": "tiếp đi"}, "", "", "")
}

// Phần mở `web flush` của HookControlTest.userPromptSubmitDeliversOnceAndSpawnsFlush (giao chỉ dẫn: Task 5).
func TestUserPromptSubmitSpawnsFlushUnlessInFlight(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.webDown()
	cache := snapshot.ControlCacheFile(r.root, "PAY-1")
	r.write(r.relOf(cache), "{}")
	r.spawned = nil
	r.hook("user-prompt-submit", r.prompt(r.root))
	eq(t, r.out, "")
	eq(t, strings.Join(r.spawned, ","), r.root)
	eq(t, r.exists(".aisdlc/web/control/spawned"), true)
	// tiến trình vừa mở chưa ghi lại cache (cache cũ hơn mốc spawned): không mở thêm
	old := testNow.Add(-10 * time.Second)
	must(t, os.Chtimes(cache, old, old))
	r.hook("user-prompt-submit", r.prompt(r.root))
	eq(t, len(r.spawned), 1)
	// tiến trình đó đã ghi cache xong: prompt kế tiếp mở flush mới dù chưa đủ 5 giây
	later := testNow.Add(time.Second)
	must(t, os.Chtimes(cache, later, later))
	r.hook("user-prompt-submit", r.prompt(r.root))
	eq(t, len(r.spawned), 2)
	eq(t, r.err, "")
}

func TestUserPromptSubmitSilentOutsideRepoAndWithoutControlDir(t *testing.T) {
	r := newTestRepo(t)
	r.casesApproved()
	r.ok(noConsole(), "step", "start", "S1")
	r.webDown()
	r.spawned = nil
	r.hook("user-prompt-submit", r.prompt(r.root))
	eq(t, r.out, "")
	eq(t, len(r.spawned), 0)
	eq(t, r.exists(".aisdlc/web/control"), false) // không tạo thư mục điều khiển
	r.hook("user-prompt-submit", r.prompt(t.TempDir()))
	eq(t, r.out, "")
	eq(t, len(r.spawned), 0)
	r.hook("user-prompt-submit", "không phải json")
	eq(t, r.out, "")
}

func (r *testRepo) relOf(abs string) string {
	rel, err := filepath.Rel(r.root, abs)
	must(r.t, err)
	return rel
}
