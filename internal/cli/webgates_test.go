package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
	"aisdlc/internal/webclient/fakeweb"
)

// WebGatesTest (phần cần web sống qua fakeweb): làm mới cache, feedback, chế độ, migrate, nộp/duyệt/trả lại ở chế
// độ web, lỗi 401/403/409, đổi khóa ký. Phần chỉ đọc cache ở webcache_test.go; ghim khóa ở webclient.

// gatesFx như setUp của WebGatesTest: setup + init PAY-1 + brd.md, HOME tạm có khóa ghim của fakeweb, web cấu
// hình qua env (token ingest-tok).
type gatesFx struct {
	*testRepo
	web    *fakeweb.Server
	repoID string
	home   string
}

func newGatesFx(t *testing.T) *gatesFx {
	r := newTestRepo(t)
	r.setupInit()
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	repoID := snapshot.RepoID(context.Background(), gitx.New(r.root), r.root)
	web := fakeweb.New(t)
	web.SetFeature(repoID, "PAY-1")
	home := t.TempDir()
	r.env["HOME"] = home
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "ingest-tok"
	must(t, webclient.Pin(home, web.Pub))
	return &gatesFx{testRepo: r, web: web, repoID: repoID, home: home}
}

func (g *gatesFx) webMode() {
	g.write("aisdlc/config.yaml", g.read("aisdlc/config.yaml")+"approvals: web\n")
}

func (g *gatesFx) cfg() model.RepoConfig {
	c, err := model.LoadRepoConfig(g.root)
	must(g.t, err)
	return c
}

// gates là WebGates của một lệnh mới (cờ webDown riêng). Stderr bỏ đi.
func (g *gatesFx) gates() *WebGates {
	return NewWebCache(g.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), g.root)
}

func (g *gatesFx) hash(gt gate.Gate) string {
	c, err := gt.ArtifactContents(model.FeaturePaths{RepoRoot: g.root, Key: "PAY-1"}, model.FlowFull)
	must(g.t, err)
	return model.SHA256Contents(c)
}

func (g *gatesFx) serverState(subs []snapshot.Sub, apprs []snapshot.Appr, fb []snapshot.Fb) {
	g.web.Update(func(s *fakeweb.Server) {
		s.State = snapshot.FeatureGateState{Repo: g.repoID, Feature: "PAY-1", Mode: "web", IssuedAt: fakeweb.T,
			Submissions: subs, Approvals: apprs, Feedback: fb}
	})
}

func (g *gatesFx) cached() (snapshot.FeatureGateState, bool) {
	return gate.ReadWebGateState(g.gates().CacheFile("PAY-1"), webclient.LoadPinnedKey(g.home), g.repoID, "PAY-1")
}

func (g *gatesFx) cacheBytes() []byte {
	b, err := os.ReadFile(g.gates().CacheFile("PAY-1"))
	must(g.t, err)
	return b
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	return pub, priv
}

// endorse: chữ ký của khóa cũ trên bytes X.509 của khóa mới (base64), như server ký khi đổi khóa.
func endorse(old ed25519.PrivateKey, next ed25519.PublicKey) string {
	der, _ := base64.StdEncoding.DecodeString(snapshot.EncodePublicKey(next))
	return base64.StdEncoding.EncodeToString(ed25519.Sign(old, der))
}

// ---- refresh / cache ----

func TestRefreshWritesVerifiedCache(t *testing.T) {
	g := newGatesFx(t)
	g.serverState([]snapshot.Sub{{Gate: "BRD", Hash: g.hash(gate.BRD), At: fakeweb.T}}, nil, nil)
	w := g.gates()
	eq(t, w.Refresh("PAY-1"), true)
	eq(t, w.CacheFile("PAY-1"), filepath.Join(g.root, ".aisdlc/web/gates/PAY-1.json"))
	s, ok := g.cached()
	eq(t, ok, true)
	eq(t, len(s.Submissions), 1)
	q := g.web.RequestsFor("GET", "/api/cli/gates")[0]
	contains(t, q.Query, "feature=PAY-1", "repo="+url.QueryEscape(g.repoID))
	eq(t, q.Auth, "Bearer ingest-tok")
}

func TestBadSignatureKeepsOldCacheAndReturnsFalse(t *testing.T) {
	g := newGatesFx(t)
	w := g.gates()
	eq(t, w.Refresh("PAY-1"), true)
	before := g.cacheBytes()
	_, other := newKey(t)
	g.web.Update(func(s *fakeweb.Server) { s.Signer = other })
	g.serverState([]snapshot.Sub{{Gate: "BRD", Hash: "x", At: fakeweb.T}},
		[]snapshot.Appr{{Gate: "BRD", Hash: "x", Approver: "giả", At: fakeweb.T, Channel: "web"}}, nil)
	eq(t, w.Refresh("PAY-1"), false)
	eq(t, string(g.cacheBytes()), string(before))
}

func TestEnvelopeOfOtherFeatureIsNotCached(t *testing.T) {
	g := newGatesFx(t)
	g.web.Update(func(s *fakeweb.Server) {
		s.State = snapshot.FeatureGateState{Repo: g.repoID, Feature: "PAY-2", Mode: "web", IssuedAt: fakeweb.T}
	})
	w := g.gates()
	eq(t, w.Refresh("PAY-1"), false)
	eq(t, g.exists(".aisdlc/web/gates/PAY-1.json"), false)
}

func TestNewFeedbackAppendedOnceAcrossTwoRefreshes(t *testing.T) {
	g := newGatesFx(t)
	fb7 := snapshot.Fb{ID: 7, Gate: "BRD", Kind: "REJECTED", By: "binh", At: fakeweb.T,
		Comment: "Thiếu tiêu chí hạn mức đêm."}
	g.serverState(nil, nil, []snapshot.Fb{fb7})
	w := g.gates()
	eq(t, w.Refresh("PAY-1"), true)
	eq(t, w.Refresh("PAY-1"), true)
	fb := filepath.Join(g.root, "aisdlc/features/PAY-1/feedback.md")
	eq(t, strings.Count(g.read("aisdlc/features/PAY-1/feedback.md"), "Thiếu tiêu chí hạn mức đêm"), 1)
	last, ok, err := webclient.LastFeedback(fb, gate.BRD)
	must(t, err)
	eq(t, ok, true)
	eq(t, last.Kind, "REJECTED")
	eq(t, last.By, "binh")
	eq(t, last.Comment, "Thiếu tiêu chí hạn mức đêm.")
	// mục mới (id khác) thì nối thêm
	g.serverState(nil, nil, []snapshot.Fb{fb7,
		{ID: 8, Gate: "BRD", Kind: "REOPENED", By: "an", At: fakeweb.T.Add(time.Minute)}})
	eq(t, w.Refresh("PAY-1"), true)
	last, _, err = webclient.LastFeedback(fb, gate.BRD)
	must(t, err)
	eq(t, last.Kind, "REOPENED")
	text := g.read("aisdlc/features/PAY-1/feedback.md")
	eq(t, strings.Count(text, "Thiếu tiêu chí"), 1)
	contains(t, text, "<!-- aisdlc-fb:7 -->", "<!-- aisdlc-fb:8 -->")
}

func TestConcurrentRefreshesAppendFeedbackOnce(t *testing.T) {
	g := newGatesFx(t)
	g.serverState(nil, nil, []snapshot.Fb{{ID: 9, Gate: "BRD", Kind: "REJECTED", By: "binh", At: fakeweb.T,
		Comment: "Một lần thôi."}})
	ws := make([]*WebGates, 6)
	for i := range ws {
		ws[i] = g.gates()
		ws[i].PinnedKey() // đọc lười trước khi chạy song song (mỗi WebGates một lệnh)
		ws[i].RepoID()
		ws[i].Config()
	}
	var wg sync.WaitGroup
	for _, w := range ws {
		wg.Add(1)
		go func() { defer wg.Done(); w.Refresh("PAY-1") }()
	}
	wg.Wait()
	text := g.read("aisdlc/features/PAY-1/feedback.md")
	eq(t, strings.Count(text, "Một lần thôi"), 1)
	eq(t, strings.Count(text, "aisdlc-fb:9"), 1)
}

func TestOlderEnvelopeArrivingLaterDoesNotOverwriteCache(t *testing.T) {
	g := newGatesFx(t)
	newer := fakeweb.T.Add(10 * time.Minute)
	g.web.Update(func(s *fakeweb.Server) {
		s.State = snapshot.FeatureGateState{Repo: g.repoID, Feature: "PAY-1", Mode: "web", IssuedAt: newer,
			Approvals: []snapshot.Appr{{Gate: "BRD", Hash: "h-new", Approver: "dev", At: newer, Channel: "web"}}}
	})
	w := g.gates()
	eq(t, w.Refresh("PAY-1"), true)
	before := g.cacheBytes()
	g.serverState(nil, nil, nil)
	eq(t, w.Refresh("PAY-1"), true) // hợp lệ nhưng cũ hơn: giữ cache
	eq(t, string(g.cacheBytes()), string(before))
	s, _ := g.cached()
	eq(t, s.IssuedAt.Equal(newer), true)
	// cùng thời điểm hoặc mới hơn thì ghi
	g.web.Update(func(s *fakeweb.Server) {
		s.State = snapshot.FeatureGateState{Repo: g.repoID, Feature: "PAY-1", Mode: "web",
			IssuedAt: newer.Add(time.Second)}
	})
	eq(t, w.Refresh("PAY-1"), true)
	s, _ = g.cached()
	eq(t, len(s.Approvals), 0)
}

// Phần hỏi web và refresh của openFeaturesInWebModeReadsCacheOnlyWithoutNetworkOrSideEffects (phần cache ở
// webcache_test.go): sau khi status đã hỏi web, liệt kê tính năng mở vẫn không gọi mạng.
func TestOpenFeaturesInWebModeReadsCacheOnlyAfterStatusTalkedToWeb(t *testing.T) {
	g := newGatesFx(t)
	g.ok(noConsole(), "init", "PAY-2")
	g.webMode()
	g.ok(noConsole(), "status", "--feature", "PAY-1")
	before := len(g.web.Requests())
	open, err := NewFeatureResolver(g.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{})).OpenFeatures(g.root)
	must(t, err)
	eq(t, len(open), 2)
	eq(t, len(g.web.Requests()), before)
	eq(t, g.exists(".aisdlc/web/control"), false)
}

func TestAfterOneNetworkFailureNoMoreRequestsInProcessAndBackoffStarts(t *testing.T) {
	g := newGatesFx(t)
	g.webMode()
	eq(t, g.run("status"), 0) // phong bì chế độ đã có trong cache
	g.web.Update(func(s *fakeweb.Server) { s.Drop = true })
	c := g.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{})
	before := len(g.web.Requests())
	_, err := c.Feature()
	must(t, err)
	// một lần thử GET trạng thái (client HTTP có thể tự thử lại GET một lần khi kết nối bị đóng)
	afterFirst := len(g.web.Requests())
	if afterFirst < before+1 || afterFirst > before+2 {
		t.Fatalf("requests: trước %d, sau %d", before, afterFirst)
	}
	eq(t, len(g.web.RequestsFor("GET", "/api/cli/gates")) > 0, true)
	eq(t, g.exists(".aisdlc/web/backoff-until"), true)
	_, err = g.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}).Feature()
	must(t, err)
	eq(t, len(g.web.Requests()), afterFirst)                               // tiến trình khác: đang backoff
	must(t, os.Remove(filepath.Join(g.root, ".aisdlc/web/backoff-until"))) // chỉ còn cờ trong tiến trình
	_, err = c.Feature()
	must(t, err)
	eq(t, len(g.web.Requests()), afterFirst) // cùng tiến trình: không thử lại
}

// ---- migrate ----

func (g *gatesFx) saveLocalApproval(gt gate.Gate) {
	g.t.Helper()
	must(g.t, gate.NewLocalGateStore(model.FeaturePaths{RepoRoot: g.root, Key: "PAY-1"}).SaveApproval(
		gate.Approval{Gate: gt, Hash: g.hash(gt), Approver: "Dev Một", At: fakeweb.T}))
}

func (g *gatesFx) setServerMode(m string) { g.web.Update(func(s *fakeweb.Server) { s.Mode = m }) }

func TestMigrateSendsOnlyMatchingLocalApprovalsAndSkipsReview(t *testing.T) {
	g := newGatesFx(t)
	g.setServerMode("local")
	g.ok(noConsole(), "submit", "brd")
	g.ok(typed("PAY-1"), "approve", "brd")
	g.ok(noConsole(), "submit", "design")
	g.ok(typed("PAY-1"), "approve", "design")
	g.write("aisdlc/features/PAY-1/design.md", g.read("aisdlc/features/PAY-1/design.md")+"\nsửa sau khi duyệt\n")
	g.saveLocalApproval(gate.REVIEW)
	g.webMode()
	g.setServerMode("web")
	if code := g.run("web", "migrate"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, g.out, g.err)
	}
	imports := g.web.RequestsFor("POST", "/api/cli/import")
	eq(t, len(imports), 1)
	var b struct {
		Repo, Feature, Gate, Hash, Approver, At string
		Findings                                []any
		Artifacts                               []struct{ Path string }
	}
	must(t, imports[0].JSON(&b))
	eq(t, b.Repo, g.repoID)
	eq(t, b.Feature, "PAY-1")
	eq(t, b.Gate, "BRD")
	eq(t, b.Hash, g.hash(gate.BRD))
	eq(t, b.Approver, "Dev Một")
	eq(t, b.At, "2026-11-02T03:00:00Z")
	eq(t, b.Findings != nil, true)
	eq(t, b.Artifacts[0].Path, "aisdlc/features/PAY-1/brd.md")
	contains(t, g.out, "Đã gửi 1 bản duyệt. Chủ tính năng xác nhận trên trang B.", "Bỏ qua REVIEW của PAY-1")
	// snapshot đi trước bản nhập (web cần luồng của tính năng)
	snap, imp := -1, -1
	for i, q := range g.web.Requests() {
		if q.Method == "POST" && q.Path == "/api/ingest/snapshot" && snap < 0 {
			snap = i
		}
		if q.Method == "POST" && q.Path == "/api/cli/import" && imp < 0 {
			imp = i
		}
	}
	eq(t, snap >= 0 && snap < imp, true)
}

func TestMigrateFullFeatureSendsBrdDesignCasesSkipsReview(t *testing.T) {
	g := newGatesFx(t)
	g.setServerMode("local")
	g.ok(noConsole(), "submit", "brd")
	g.ok(typed("PAY-1"), "approve", "brd")
	g.ok(noConsole(), "submit", "design")
	g.ok(typed("PAY-1"), "approve", "design")
	g.saveLocalApproval(gate.CASES)
	g.saveLocalApproval(gate.REVIEW)
	g.webMode()
	g.setServerMode("web")
	if code := g.run("web", "migrate"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, g.out, g.err)
	}
	var gates []string
	for _, q := range g.web.RequestsFor("POST", "/api/cli/import") {
		gates = append(gates, q.Field("gate"))
	}
	eqList(t, gates, "BRD", "DESIGN", "CASES")
	contains(t, g.out, "Đã gửi 3 bản duyệt.", "Bỏ qua REVIEW của PAY-1")
}

func TestMigrateInLocalModeExit2(t *testing.T) {
	g := newGatesFx(t)
	g.setServerMode("local")
	eq(t, g.run("web", "migrate"), 2)
	contains(t, g.err, "Repo chưa ở chế độ duyệt trên web: đặt `approvals: web` trong aisdlc/config.yaml trước.")
	eq(t, len(g.web.RequestsFor("POST", "/api/cli/import")), 0)
}

// ---- submit ----

func TestSubmitNetworkErrorAbortsAndWritesNothing(t *testing.T) {
	g := newGatesFx(t)
	g.setServerMode("local")
	g.ok(noConsole(), "submit", "brd") // chế độ local: gates.json có bản nộp
	before := g.read("aisdlc/features/PAY-1/gates.json")
	must(t, os.Remove(filepath.Join(g.root, ".aisdlc/web/mode.json"))) // chưa có phong bì chế độ: theo config.yaml
	g.webMode()
	g.env["AISDLC_WEB_URL"] = closedPort(t)
	c := g.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{})
	f, err := c.Feature()
	must(t, err)
	eq(t, f.Mode, gate.ModeWeb)
	err = f.Web.Submit(f, gate.BRD, nil, "")
	var a *Abort
	if !asAbort(err, &a) {
		t.Fatalf("muốn Abort, got %v", err)
	}
	eq(t, a.Code, 2)
	eq(t, strings.HasPrefix(a.Msg, "Không nộp được lên web: "), true)
	eq(t, g.read("aisdlc/features/PAY-1/gates.json"), before)
	eq(t, g.exists(".aisdlc/web/gates/PAY-1.json"), false)
	// qua lệnh: exit 2, gates.json không đổi
	eq(t, g.run("submit", "brd"), 2)
	contains(t, g.err, "Không nộp được lên web")
	eq(t, g.read("aisdlc/features/PAY-1/gates.json"), before)
}

func TestSubmitSendsSnapshotThenArtifactsAndUpdatesCache(t *testing.T) {
	g := newGatesFx(t)
	g.webMode()
	if code := g.run("submit", "brd"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, g.out, g.err)
	}
	contains(t, g.out, "Đã nộp cổng BRD lên web")
	eq(t, g.exists("aisdlc/features/PAY-1/gates.json"), false)
	snap, sub := -1, -1
	for i, q := range g.web.Requests() {
		if q.Path == "/api/ingest/snapshot" && snap < 0 {
			snap = i
		}
		if q.Path == "/api/cli/submissions" && sub < 0 {
			sub = i
		}
	}
	eq(t, snap >= 0 && snap < sub, true)
	var b struct {
		Repo, Gate, Hash string
		Artifacts        []struct{ Path, Content string }
	}
	must(t, g.web.RequestsFor("POST", "/api/cli/submissions")[0].JSON(&b))
	eq(t, b.Repo, g.repoID)
	eq(t, b.Gate, "BRD")
	eq(t, b.Hash, g.hash(gate.BRD))
	eq(t, b.Artifacts[0].Path, "aisdlc/features/PAY-1/brd.md")
	content, err := base64.StdEncoding.DecodeString(b.Artifacts[0].Content)
	must(t, err)
	eq(t, string(content), validBRD)
	// cache cập nhật từ phong bì trả về: status thấy AWAITING_APPROVAL
	eq(t, g.state(gate.BRD), "AWAITING_APPROVAL")
}

func TestWebJsonTokenUsedForCliEvenWhenEnvTokenSetAndSubmitWarns(t *testing.T) {
	g := newGatesFx(t)
	g.webMode()
	must(t, webclient.SaveConfig(g.home, webclient.Config{URL: g.web.URL(), Token: "file-ingest-tok"}))
	g.env["AISDLC_WEB_URL"] = "http://127.0.0.1:1" // env bị bỏ qua khi có web.json
	g.env["AISDLC_WEB_TOKEN"] = "login-tok-2a"
	if code := g.run("submit", "brd"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, g.out, g.err)
	}
	eq(t, g.web.RequestsFor("POST", "/api/cli/submissions")[0].Auth, "Bearer file-ingest-tok")
	for _, q := range g.web.Requests() {
		if q.Auth == "Bearer login-tok-2a" {
			t.Fatalf("token env bị gửi: %s %s", q.Method, q.Path)
		}
	}
	contains(t, g.err, "AISDLC_WEB_TOKEN đang đặt trong môi trường")
	notContains(t, g.out+g.err, "login-tok-2a", "file-ingest-tok")
}

// ---- mode ----

func TestModeIssuedAtMonotonic(t *testing.T) {
	g := newGatesFx(t)
	cfg := g.cfg()
	t2 := fakeweb.T.Add(10 * time.Minute)
	g.web.Update(func(s *fakeweb.Server) { s.ModeIssuedAt = t2 })
	w := g.gates()
	m, ok := w.RefreshMode(cfg)
	eq(t, ok, true)
	eq(t, m, gate.ModeWeb)
	before, err := os.ReadFile(w.ModeFile())
	must(t, err)
	// phong bì cũ hơn đến muộn: không ghi đè, chế độ giữ theo cache
	g.web.Update(func(s *fakeweb.Server) { s.ModeIssuedAt, s.Mode = fakeweb.T, "local" })
	m, ok = g.gates().RefreshMode(cfg)
	eq(t, ok, true)
	eq(t, m, gate.ModeWeb)
	after, _ := os.ReadFile(w.ModeFile())
	eq(t, string(after), string(before))
	// cùng thời điểm hoặc mới hơn thì ghi
	g.web.Update(func(s *fakeweb.Server) { s.ModeIssuedAt = t2.Add(time.Second) })
	m, ok = g.gates().RefreshMode(cfg)
	eq(t, ok, true)
	eq(t, m, gate.ModeLocal)
	after, _ = os.ReadFile(w.ModeFile())
	eq(t, string(after) != string(before), true)
}

// Phần hỏi web của validModeCacheBeatsConfigLocal (phần cache ở webcache_test.go).
func TestValidModeCacheBeatsConfigLocalAsksWebOnce(t *testing.T) {
	g := newGatesFx(t)
	local := g.cfg()
	eq(t, local.Approvals, "local")
	w := g.gates()
	eq(t, w.Mode(local), gate.ModeWeb) // hỏi web, lưu cache
	eq(t, g.exists(".aisdlc/web/mode.json"), true)
	reqs := g.web.RequestsFor("POST", "/api/cli/mode")
	eq(t, reqs[0].Field("configured"), "local")
	g.env["AISDLC_WEB_URL"] = "http://127.0.0.1:1"
	again := g.gates()
	eq(t, again.CachedMode(local), gate.ModeWeb)
	eq(t, again.Mode(local), gate.ModeWeb)
	eq(t, len(g.web.RequestsFor("POST", "/api/cli/mode")), len(reqs))
}

func TestSwitchingConfigToWebReasksServerDespiteLocalCache(t *testing.T) {
	g := newGatesFx(t)
	g.setServerMode("local")
	eq(t, g.gates().Mode(g.cfg()), gate.ModeLocal)
	g.webMode()
	g.setServerMode("web")
	eq(t, g.gates().Mode(g.cfg()), gate.ModeWeb)
	reqs := g.web.RequestsFor("POST", "/api/cli/mode")
	eq(t, reqs[len(reqs)-1].Field("configured"), "web")
	// lead chuyển về local trên web: web flush làm mới phong bì chế độ, phong bì thắng config.yaml
	g.setServerMode("local")
	later := g.gates()
	m, ok := later.RefreshMode(g.cfg())
	eq(t, ok, true)
	eq(t, m, gate.ModeLocal)
	eq(t, later.CachedMode(g.cfg()), gate.ModeLocal)
}

// ---- approve / reject ở chế độ web ----

func (g *gatesFx) designAwaiting() {
	g.t.Helper()
	g.webMode()
	g.serverState([]snapshot.Sub{{Gate: "BRD", Hash: g.hash(gate.BRD), At: fakeweb.T},
		{Gate: "DESIGN", Hash: g.hash(gate.DESIGN), At: fakeweb.T}},
		[]snapshot.Appr{{Gate: "BRD", Hash: g.hash(gate.BRD), Approver: "dev", At: fakeweb.T, Channel: "web"}}, nil)
}

func (g *gatesFx) approveToken() {
	g.t.Helper()
	must(g.t, webclient.SaveConfig(g.home, webclient.Config{URL: g.web.URL(), Token: "ingest-tok",
		ApproveToken: "appr-tok"}))
}

func (g *gatesFx) override(key string, status int, body string) {
	g.web.Update(func(s *fakeweb.Server) { s.Override[key] = fakeweb.JSONResp(status, body) })
}

func TestApproveWithoutApproveTokenPrintsWebLink(t *testing.T) {
	g := newGatesFx(t)
	g.designAwaiting()
	eq(t, g.runC(typed("PAY-1"), "approve", "design"), 2)
	contains(t, g.err, "Chưa có token approve. Duyệt trên web: "+g.web.URL()+"/f/", "/PAY-1?g=DESIGN",
		"`aisdlc web approve-token`")
	eq(t, len(g.web.RequestsFor("POST", "/api/gates/approve")), 0)
}

func TestApproveConflictExitsOneWithServerMessage(t *testing.T) {
	g := newGatesFx(t)
	g.designAwaiting()
	g.approveToken()
	g.override("POST /api/gates/approve", 409, `{"error":"Artifact của cổng DESIGN đã đổi kể từ lúc nộp"}`)
	eq(t, g.runC(typed("PAY-1"), "approve", "design"), 1)
	contains(t, g.err, "Artifact của cổng DESIGN đã đổi kể từ lúc nộp")
	eq(t, g.exists("aisdlc/features/PAY-1/gates.json"), false)
}

func TestApproveWithClaudeCodeGrantSendsClaudeCodeChannel(t *testing.T) {
	g := newGatesFx(t)
	g.designAwaiting()
	g.approveToken()
	g.enableOnBase()
	g.env["AISDLC_SESSION"] = "s1"
	in := approveHookIn(g.root, "s1", "default", "Bash", "command", "aisdlc approve design")
	eq(t, g.runWithStdin(in, "hook", "pre-tool-use"), 0)
	contains(t, g.out, "ask")
	if code := g.run("approve", "design"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, g.out, g.err)
	}
	eq(t, g.web.RequestsFor("POST", "/api/gates/approve")[0].Field("channel"), "claude-code")
}

func TestApproveSendsTerminalChannelAndCardFlags(t *testing.T) {
	g := newGatesFx(t)
	g.designAwaiting()
	g.approveToken()
	eq(t, g.run("review", "design", "--json"), 0)
	var card struct {
		Flags []struct{ Code, Level string }
	}
	must(t, json.Unmarshal([]byte(g.out), &card))
	var expected []string
	for _, x := range card.Flags {
		if x.Level == "HIGH" || x.Level == "WARN" {
			expected = append(expected, x.Code+":"+x.Level)
		}
	}
	if code := g.runC(typed("PAY-1"), "approve", "design"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, g.out, g.err)
	}
	contains(t, g.out, "Đã duyệt cổng DESIGN trên web")
	q := g.web.RequestsFor("POST", "/api/gates/approve")[0]
	eq(t, q.Auth, "Bearer appr-tok")
	var b struct {
		Channel, Gate, Hash string
		Flags               []struct{ Code, Level string }
		AssumptionsSeen     []any
	}
	must(t, q.JSON(&b))
	eq(t, b.Channel, "terminal")
	eq(t, b.Gate, "DESIGN")
	eq(t, b.Hash, g.hash(gate.DESIGN))
	var sent []string
	for _, x := range b.Flags {
		sent = append(sent, x.Code+":"+x.Level)
	}
	eq(t, strings.Join(sent, ","), strings.Join(expected, ","))
	eq(t, b.AssumptionsSeen != nil, true)
	eq(t, g.exists("aisdlc/features/PAY-1/gates.json"), false)
	eq(t, g.state(gate.DESIGN), "APPROVED")
	// Java ApproveCommand.java:67: lệnh duyệt trên web cũng gửi COMMAND "approve design"
	found := false
	for _, q := range g.web.RequestsFor("POST", "/api/ingest/events") {
		found = found || strings.Contains(q.Body, `"detail":"approve design"`)
	}
	eq(t, found, true)
}

func TestRejectInWebModeGoesToWebAndFeedbackComesBack(t *testing.T) {
	g := newGatesFx(t)
	g.designAwaiting()
	g.approveToken()
	st := snapshot.FeatureGateState{Repo: g.repoID, Feature: "PAY-1", Mode: "web", IssuedAt: fakeweb.T,
		Feedback: []snapshot.Fb{{ID: 3, Gate: "DESIGN", Kind: "REJECTED", By: "dev", At: fakeweb.T,
			Comment: "Thiếu chỉ mục"}}}
	p, err := st.ToJSON()
	must(t, err)
	env := g.web.Envelope(p)
	g.web.Update(func(s *fakeweb.Server) { s.Override["POST /api/gates/reject"] = fakeweb.Resp{Status: 200, Body: env} })
	if code := g.runC(typed("PAY-1"), "reject", "design", "-m", "Thiếu chỉ mục"); code != 0 {
		t.Fatalf("exit %d\n%s", code, g.err)
	}
	eq(t, g.web.RequestsFor("POST", "/api/gates/reject")[0].Field("comment"), "Thiếu chỉ mục")
	contains(t, g.read("aisdlc/features/PAY-1/feedback.md"), "Thiếu chỉ mục")
	eq(t, g.exists("aisdlc/features/PAY-1/gates.json"), false)
}

func TestApproveNotWebModeConflictExitsTwo(t *testing.T) {
	g := newGatesFx(t)
	g.designAwaiting()
	g.approveToken()
	g.override("POST /api/gates/approve", 409, `{"error":"Repo không ở chế độ duyệt trên web"}`)
	eq(t, g.runC(typed("PAY-1"), "approve", "design"), 2)
	contains(t, g.err, "Repo không ở chế độ duyệt trên web")
}

// ---- 401/403 của server bằng tiếng Việt ----

func TestUnauthorizedAndWrongKindAreExplainedInVietnamese(t *testing.T) {
	g := newGatesFx(t)
	g.webMode()
	g.override("POST /api/cli/submissions", 401, `{"error":"unauthorized"}`)
	eq(t, g.run("submit", "brd"), 2)
	contains(t, g.err, "Token không còn hiệu lực, chạy lại `aisdlc web login`")
	notContains(t, g.err, "unauthorized")
	g.override("POST /api/cli/submissions", 403, `{"error":"forbidden"}`)
	eq(t, g.run("submit", "brd"), 2)
	contains(t, g.err, "Token không đúng loại (cần token ingest cho CLI, approve cho duyệt)")
}

func TestForbiddenWithServerReasonKeepsReason(t *testing.T) {
	g := newGatesFx(t)
	g.designAwaiting()
	g.approveToken()
	g.override("POST /api/gates/approve", 403, `{"error":"Chỉ chủ tính năng hoặc lead được duyệt"}`)
	eq(t, g.runC(typed("PAY-1"), "approve", "design"), 2)
	contains(t, g.err, "Chỉ chủ tính năng hoặc lead được duyệt")
	g.override("POST /api/gates/approve", 401, `{"error":"unauthorized"}`)
	eq(t, g.runC(typed("PAY-1"), "approve", "design"), 2)
	contains(t, g.err, "Token không còn hiệu lực")
}

// ---- phong bì cổng nói local → chế độ chuyển local ngay ----

func TestGateEnvelopeSayingLocalSwitchesCachedModeToLocal(t *testing.T) {
	g := newGatesFx(t)
	g.webMode()
	cfg := g.cfg()
	eq(t, g.run("status"), 0) // phong bì chế độ web đã vào cache
	eq(t, g.gates().CachedMode(cfg), gate.ModeWeb)
	// lead chuyển về local; CLI chỉ làm mới trạng thái cổng (không hỏi /api/cli/mode)
	g.web.Update(func(s *fakeweb.Server) {
		s.State = snapshot.FeatureGateState{Repo: g.repoID, Feature: "PAY-1", Mode: "local",
			IssuedAt: fakeweb.T.Add(time.Minute)}
	})
	w := g.gates()
	eq(t, w.Refresh("PAY-1"), true)
	eq(t, g.gates().CachedMode(cfg), gate.ModeLocal) // hook đọc cache
	// chỉ dữ liệu đã xác minh: phong bì bị sửa → lại theo config.yaml (web)
	b, err := os.ReadFile(w.ModeFile())
	must(t, err)
	b[len(b)/2] ^= 1
	must(t, os.WriteFile(w.ModeFile(), b, 0o644))
	eq(t, g.gates().CachedMode(cfg), gate.ModeWeb)
}

// Phần refresh của gateEnvelopeSignedByOtherKeyDoesNotSwitchMode (phần cache ở webcache_test.go).
func TestGateEnvelopeSignedByOtherKeyIsRefusedByRefresh(t *testing.T) {
	g := newGatesFx(t)
	g.webMode()
	cfg := g.cfg()
	eq(t, g.run("status"), 0)
	_, other := newKey(t)
	g.web.Update(func(s *fakeweb.Server) {
		s.Signer = other
		s.State = snapshot.FeatureGateState{Repo: g.repoID, Feature: "PAY-1", Mode: "local",
			IssuedAt: fakeweb.T.Add(time.Minute)}
	})
	eq(t, g.gates().Refresh("PAY-1"), false)
	eq(t, g.gates().CachedMode(cfg), gate.ModeWeb)
}

// ---- đổi khóa khi làm mới ----

func TestRefreshRepinsKeyEndorsedByPinnedKeyAndRetries(t *testing.T) {
	g := newGatesFx(t)
	next, nextPriv := newKey(t)
	g.web.Update(func(s *fakeweb.Server) {
		s.Signer = nextPriv
		s.Override["GET /api/cli/pubkey"] = fakeweb.JSONResp(200, `{"publicKey":"`+snapshot.EncodePublicKey(next)+
			`","keyId":"`+snapshot.KeyID(next)+`","endorsement":"`+endorse(s.Priv, next)+`"}`)
	})
	o, e := &bytes.Buffer{}, &bytes.Buffer{}
	w := NewWebCache(g.context(noConsole(), "", o, e), g.root)
	eq(t, w.Refresh("PAY-1"), true)
	eq(t, webclient.LoadPinnedKey(g.home).Equal(next), true)
	_, ok := gate.ReadWebGateState(w.CacheFile("PAY-1"), next, g.repoID, "PAY-1")
	eq(t, ok, true)
	reqs := g.web.RequestsFor("GET", "/api/cli/pubkey")
	eq(t, len(reqs), 1)
	eq(t, reqs[0].Auth, "Bearer ingest-tok")
	contains(t, e.String(), "aisdlc web: web đã đổi khóa ký; đã ghim khóa mới "+snapshot.KeyID(next))
	eq(t, o.String(), "")
}

func TestRefreshRefusesKeyNotEndorsedByPinnedKey(t *testing.T) {
	g := newGatesFx(t)
	next, nextPriv := newKey(t)
	_, stranger := newKey(t)
	g.web.Update(func(s *fakeweb.Server) {
		s.Signer = nextPriv
		s.Override["GET /api/cli/pubkey"] = fakeweb.JSONResp(200, `{"publicKey":"`+snapshot.EncodePublicKey(next)+
			`","endorsement":"`+endorse(stranger, next)+`"}`)
	})
	w := g.gates()
	eq(t, w.Refresh("PAY-1"), false)
	eq(t, webclient.LoadPinnedKey(g.home).Equal(g.web.Pub), true)
	eq(t, g.exists(".aisdlc/web/gates/PAY-1.json"), false)
	// không có endorsement cũng vậy
	g.override("GET /api/cli/pubkey", 200, `{"publicKey":"`+snapshot.EncodePublicKey(next)+`"}`)
	eq(t, g.gates().Refresh("PAY-1"), false)
	eq(t, webclient.LoadPinnedKey(g.home).Equal(g.web.Pub), true)
}

// ---- web flush làm mới chế độ và trạng thái cổng (WebCommand.flush) ----

func TestWebFlushRefreshesModeAndActiveGatesSilently(t *testing.T) {
	g := newGatesFx(t)
	g.webMode()
	g.serverState([]snapshot.Sub{{Gate: "BRD", Hash: g.hash(gate.BRD), At: fakeweb.T}}, nil, nil)
	eq(t, g.run("web", "flush"), 0)
	eq(t, g.out, "")
	eq(t, g.err, "")
	eq(t, len(g.web.RequestsFor("POST", "/api/cli/mode")), 1)
	eq(t, len(g.web.RequestsFor("GET", "/api/cli/gates")), 1)
	eq(t, g.exists(".aisdlc/web/mode.json"), true)
	s, ok := g.cached()
	eq(t, ok, true)
	eq(t, len(s.Submissions), 1)
	// chế độ local: chỉ làm mới chế độ, không hỏi trạng thái cổng
	g.setServerMode("local")
	g.web.Update(func(s *fakeweb.Server) { s.ModeIssuedAt = fakeweb.T.Add(time.Hour) })
	eq(t, g.run("web", "flush"), 0)
	eq(t, g.gates().CachedMode(g.cfg()), gate.ModeLocal)
	eq(t, len(g.web.RequestsFor("GET", "/api/cli/gates")), 1)
}
