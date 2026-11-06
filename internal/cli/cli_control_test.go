package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
	"aisdlc/internal/webclient/fakeweb"
)

// CliControlTest (4): lệnh CLI có mạng với web giả, repo duyệt local: `web flush` gỡ tạm dừng, lệnh chờ áp dụng.
// StatusControlTest.pausedOfflineMessage. Và carry-over Task 4: lệnh duyệt ở chế độ web không console, không phiếu.

// newCliControlFx như setUp của CliControlTest: chưa setup; web giả chế độ local, đã ghim khóa.
func newCliControlFx(t *testing.T) *controlWebFx {
	r := newTestRepo(t)
	home := t.TempDir()
	r.env["HOME"] = home
	repoID := NewWebCache(r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), r.root).RepoID()
	web := fakeweb.New(t)
	web.SetFeature(repoID, "PAY-1")
	web.Update(func(s *fakeweb.Server) { s.Mode = "local" }) // repo duyệt local; điều khiển từ web vẫn chạy
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "ingest-tok"
	must(t, webclient.Pin(home, web.Pub))
	return &controlWebFx{r, web, repoID}
}

func (c *controlWebFx) writeCache(fc snapshot.FeatureControl) {
	c.t.Helper()
	fc.Repo = c.repoID
	b, err := fc.ToJSON()
	must(c.t, err)
	c.write(c.relOf(c.cacheFile()), string(c.web.Envelope(b)))
}

func (c *controlWebFx) codeEdit() string {
	return hookIn("PreToolUse", c.root, "Edit", "file_path", filepath.Join(c.root, codePath))
}

// actionResults là detail của mọi event ACTION_RESULT: còn trong outbox hoặc đã gửi lên web giả.
func (c *controlWebFx) actionResults() []map[string]any {
	c.t.Helper()
	events := c.outboxEvents()
	for _, q := range c.web.RequestsFor("POST", "/api/ingest/events") {
		var b struct {
			Events []map[string]any `json:"events"`
		}
		must(c.t, q.JSON(&b))
		events = append(events, b.Events...)
	}
	var out []map[string]any
	for _, e := range events {
		if e["type"] == "ACTION_RESULT" {
			var d map[string]any
			must(c.t, json.Unmarshal([]byte(e["detail"].(string)), &d))
			out = append(out, d)
		}
	}
	return out
}

func (c *controlWebFx) endTree() string {
	return (&controlFx{testRepo: c.testRepo}).endTree()
}

func TestWebFlushLiftsPauseInLocalMode(t *testing.T) {
	c := newCliControlFx(t)
	c.casesApproved()
	c.ok(noConsole(), "step", "start", "S1")
	// cache có tạm dừng (ký đúng)
	c.writeCache(snapshot.FeatureControl{Feature: "PAY-1", IssuedAt: fakeweb.T.Add(10 * time.Second),
		Pause: &snapshot.Pause{By: "an", At: fakeweb.T}})
	c.hook("pre-tool-use", c.codeEdit())
	contains(t, c.out, "Dev đã tạm dừng tại web: an")
	// dev bấm Tiếp tục: web trả pause null, mới hơn
	c.serve(fakeweb.T.Add(time.Minute), nil)
	eq(t, c.run("web", "flush"), 0)
	if len(c.web.RequestsFor("GET", "/api/cli/control")) == 0 {
		t.Fatal("web flush không hỏi /api/cli/control")
	}
	got, ok := snapshot.ReadControlCache(c.cacheFile(), c.web.Pub, c.repoID, "PAY-1")
	eq(t, ok, true)
	if got.Pause != nil {
		t.Fatalf("còn tạm dừng: %+v", got)
	}
	eq(t, c.exists(".aisdlc/web/mode.json"), true)
	c.hook("pre-tool-use", c.codeEdit())
	eq(t, c.out, "") // đã gỡ tạm dừng: sửa code trong phạm vi được
}

func TestStatusAppliesPendingActionsFromWeb(t *testing.T) {
	c := newCliControlFx(t)
	c.s1AwaitingDev()
	exp := fakeweb.T.Add(7 * 24 * time.Hour)
	endTree := c.endTree()
	c.web.Update(func(s *fakeweb.Server) {
		s.Control = snapshot.FeatureControl{Repo: c.repoID, Feature: "PAY-1", IssuedAt: fakeweb.T.Add(time.Minute),
			Actions: []snapshot.Action{
				{ID: 77, Kind: "ACCEPT_STEP", Step: "S1", EndTree: endTree, By: "an", At: fakeweb.T, ExpiresAt: &exp},
				{ID: 78, Kind: "ALLOW_SCOPE", Step: "S1", Path: "src/x/B.java", By: "an", At: fakeweb.T,
					ExpiresAt: &exp},
			}}
	})
	before := len(c.web.Requests())
	eq(t, c.run("status"), 0)
	asked := false
	for _, q := range c.web.Requests()[before:] {
		asked = asked || q.Path == "/api/cli/control"
	}
	eq(t, asked, true)
	contains(t, c.err, "Áp dụng từ web: đã accept S1 (an)\n", "Áp dụng từ web: đã cho phép src/x/B.java ở S1 (an)\n")
	contains(t, c.read("aisdlc/features/PAY-1/steps.json"), "ACCEPTED", "an (web)")
	contains(t, c.read("aisdlc/features/PAY-1/scope-extra.json"), "src/x/B.java")
	res := c.actionResults()
	eq(t, len(res), 2)
	seen := map[float64]bool{}
	for _, d := range res {
		eq(t, d["status"], any("APPLIED"))
		seen[d["id"].(float64)] = true
	}
	eq(t, seen[77] && seen[78], true)
	// lần sau không áp dụng hay báo lại
	eq(t, c.run("status"), 0)
	notContains(t, c.err, "Áp dụng từ web")
	eq(t, len(c.actionResults()), 2)
}

func TestTerminalAcceptAfterWebAcceptSucceeds(t *testing.T) {
	c := newCliControlFx(t)
	c.s1AwaitingDev()
	exp := fakeweb.T.Add(7 * 24 * time.Hour)
	endTree := c.endTree()
	c.web.Update(func(s *fakeweb.Server) {
		s.Control = snapshot.FeatureControl{Repo: c.repoID, Feature: "PAY-1", IssuedAt: fakeweb.T.Add(time.Minute),
			Actions: []snapshot.Action{
				{ID: 77, Kind: "ACCEPT_STEP", Step: "S1", EndTree: endTree, By: "an", At: fakeweb.T, ExpiresAt: &exp}}}
	})
	// lệnh dựng tính năng áp dụng Accept từ web trước, rồi mới tới accept ở terminal
	eq(t, c.run("step", "accept", "S1"), 0)
	contains(t, c.err, "Áp dụng từ web: đã accept S1 (an)")
	contains(t, c.out, "Bước S1 đã được accept từ web (an).")
	// chạy lại khi đã ACCEPTED bởi "an (web)": vẫn exit 0, cùng câu
	eq(t, c.run("step", "accept", "S1"), 0)
	contains(t, c.out, "Bước S1 đã được accept từ web (an).")
	contains(t, c.read("aisdlc/features/PAY-1/steps.json"), "an (web)")
}

func TestRefreshRejectedByWebIsNotReportedAsOffline(t *testing.T) {
	c := newCliControlFx(t)
	c.setupInit()
	c.writeCache(snapshot.FeatureControl{Feature: "PAY-1", IssuedAt: fakeweb.T,
		Pause: &snapshot.Pause{By: "an", At: fakeweb.T}})
	c.web.Update(func(s *fakeweb.Server) {
		s.Override["GET /api/cli/control"] = fakeweb.JSONResp(403, `{"error":"forbidden"}`)
	})
	eq(t, c.run("status"), 0)
	contains(t, c.out, "Đang tạm dừng từ web (an,")
	notContains(t, c.out, "không truy cập được")
}

// StatusControlTest.pausedOfflineMessage: web bật nhưng không với tới được.
func TestPausedOfflineMessage(t *testing.T) {
	r, write := newControlFx(t)
	r.webDown()
	exp := testNow.Add(24 * time.Hour)
	write(snapshot.FeatureControl{Feature: "PAY-1", IssuedAt: testNow, Pause: &snapshot.Pause{By: "an", At: testNow},
		Instructions: []snapshot.Instruction{{ID: 3, By: "an", At: testNow, Text: "x"}},
		Actions: []snapshot.Action{{ID: 9, Kind: "ACCEPT_STEP", Step: "S1", EndTree: "t", By: "an", At: testNow,
			ExpiresAt: &exp}}})
	eq(t, r.run("status"), 0)
	contains(t, r.out, "Đang tạm dừng từ web (an,", "web không truy cập được, chưa nhận lệnh tiếp tục",
		"Chỉ dẫn chờ giao cho agent: 1")
	// S1 chưa làm: lệnh STALE ngay khi dựng tính năng, không còn chờ
	notContains(t, r.out, "Lệnh từ web chờ áp dụng")
	contains(t, r.err, "Lệnh từ web hết hiệu lực: ")
}

// Carry-over Task 4: ở chế độ web, approve/reject/reopen do agent gọi thẳng (không console, không phiếu của Claude
// Code) exit 3 và không gửi gì tới /api/gates/*, kể cả khi đã có token approve và sessionApprove bật.
func TestWebModeDevOnlyCommandsWithoutConsoleOrGrantExitThreeAndSendNothing(t *testing.T) {
	g := newGatesFx(t)
	g.designAwaiting()
	g.approveToken()
	cmds := [][]string{{"approve", "design"}, {"reject", "design", "-m", "x"}, {"reopen", "design"}}
	check := func(want string) {
		t.Helper()
		for _, args := range cmds {
			eq(t, g.run(args...), 3)
			contains(t, g.err, want)
		}
		for _, q := range g.web.Requests() {
			if strings.HasPrefix(q.Path, "/api/gates/") {
				t.Fatalf("không được gửi %s %s", q.Method, q.Path)
			}
		}
	}
	check("không tìm thấy console")
	g.enableOnBase()
	g.env["AISDLC_SESSION"] = "s1"
	check("Không có xác nhận trong Claude Code")
}
