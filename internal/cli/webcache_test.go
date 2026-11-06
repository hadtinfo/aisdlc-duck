package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
)

// WebGatesTest, phần chỉ đọc cache (webcache.go). Phần cần FakeWeb sống (mode/refresh/submit/approve qua HTTP,
// outbox, flush) để giai đoạn 4: xem docs/superpowers/deferred-tests.md.

// webFx như setUp của WebGatesTest: setup + init PAY-1 + brd.md, HOME tạm có khóa ghim, web cấu hình qua env trỏ
// vào một server chỉ đếm request (không được gọi). Trả khóa riêng để ký cache, định danh repo và bộ đếm request.
type webFx struct {
	*testRepo
	priv     ed25519.PrivateKey
	repoID   string
	requests *atomic.Int64
}

func newWebFx(t *testing.T) *webFx {
	r := newTestRepo(t)
	r.setupInit()
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	home := t.TempDir()
	r.env["HOME"] = home
	_, priv := pinKey(t, home)
	var requests atomic.Int64
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(500)
	}))
	t.Cleanup(web.Close)
	r.env["AISDLC_WEB_URL"] = web.URL
	r.env["AISDLC_WEB_TOKEN"] = "ingest-tok"
	return &webFx{testRepo: r, priv: priv, requests: &requests,
		repoID: snapshot.RepoID(context.Background(), gitx.New(r.root), r.root)}
}

func (w *webFx) webMode() {
	w.write("aisdlc/config.yaml", w.read("aisdlc/config.yaml")+"approvals: web\n")
}

func (w *webFx) cfg() model.RepoConfig {
	c, err := model.LoadRepoConfig(w.root)
	must(w.t, err)
	return c
}

func (w *webFx) cache() *WebCache {
	return NewWebCache(w.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), w.root)
}

func (w *webFx) writeMode(repo, mode string, priv ed25519.PrivateKey) {
	w.writeSigned(".aisdlc/web/mode.json", []byte(`{"repo":"`+repo+`","mode":"`+mode+`"}`), priv)
}

func otherKey(t *testing.T) ed25519.PrivateKey {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	return priv
}

// Phần cache của validModeCacheBeatsConfigLocal: phong bì mode.json hợp lệ thắng approvals: local của config;
// phong bì bị sửa thì theo config.
func TestValidModeCacheBeatsConfigLocal(t *testing.T) {
	w := newWebFx(t)
	local := w.cfg()
	eq(t, local.Approvals, "local")
	w.writeMode(w.repoID, "web", w.priv)
	eq(t, w.cache().CachedMode(local), gate.ModeWeb)
	// cache bị sửa → theo config
	f := filepath.Join(w.root, ".aisdlc/web/mode.json")
	b, err := os.ReadFile(f)
	must(t, err)
	b[len(b)/2] ^= 1
	must(t, os.WriteFile(f, b, 0o644))
	eq(t, w.cache().CachedMode(local), gate.ModeLocal)
	eq(t, w.requests.Load(), int64(0))
}

// Chưa ghim khóa: chế độ theo config, không approval nào từ cache (kể cả cache đã ký có approval), không gọi mạng.
func TestNoPinnedKeyMeansConfigModeAndNoApprovals(t *testing.T) {
	w := newWebFx(t)
	w.webMode()
	hash, err := w.feature().Gates.CurrentHash(gate.BRD)
	must(t, err)
	st := snapshot.FeatureGateState{Repo: w.repoID, Feature: "PAY-1", Mode: "web", IssuedAt: testNow,
		Submissions: []snapshot.Sub{{Gate: "BRD", Hash: hash, At: testNow}},
		Approvals:   []snapshot.Appr{{Gate: "BRD", Hash: hash, Approver: "dev", At: testNow, Channel: "web"}}}
	b, err := st.ToJSON()
	must(t, err)
	w.writeSigned(".aisdlc/web/gates/PAY-1.json", b, w.priv)
	w.writeMode(w.repoID, "local", w.priv)
	must(t, os.Remove(PinnedKeyFile(w.env["HOME"])))
	eq(t, w.run("status"), 0)
	contains(t, w.err, "Chưa `aisdlc web login`")
	contains(t, w.out, "Duyệt: trên web", "BRD      NOT_SUBMITTED")
	eq(t, w.requests.Load(), int64(0))
}

// Phần cache của gateEnvelopeSignedByOtherKeyDoesNotSwitchMode: phong bì (cổng hay chế độ) ký bằng khóa khác nói
// "local" không đổi chế độ web của config.
func TestGateEnvelopeSignedByOtherKeyDoesNotSwitchMode(t *testing.T) {
	w := newWebFx(t)
	w.webMode()
	cfg := w.cfg()
	other := otherKey(t)
	st := snapshot.FeatureGateState{Repo: w.repoID, Feature: "PAY-1", Mode: "local", IssuedAt: testNow}
	b, err := st.ToJSON()
	must(t, err)
	w.writeSigned(".aisdlc/web/gates/PAY-1.json", b, other)
	eq(t, w.cache().CachedMode(cfg), gate.ModeWeb)
	w.writeMode(w.repoID, "local", other)
	eq(t, w.cache().CachedMode(cfg), gate.ModeWeb)
	eq(t, w.requests.Load(), int64(0))
}

func TestOpenFeaturesInWebModeReadsCacheOnlyWithoutNetworkOrSideEffects(t *testing.T) {
	w := newWebFx(t)
	w.ok(noConsole(), "init", "PAY-2")
	w.webMode()
	before := w.requests.Load() // init gửi event/snapshot (web bật)
	open, err := NewFeatureResolver(w.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{})).OpenFeatures(w.root)
	must(t, err)
	var keys []string
	for _, k := range open {
		keys = append(keys, k.String())
	}
	eqList(t, keys, "PAY-1", "PAY-2")
	eq(t, w.requests.Load(), before)
	eq(t, w.exists(".aisdlc/web/control"), false)
	eq(t, w.exists(".aisdlc/web/gates"), false)
}

// modeFromCache: khóa khác, repo khác, chế độ lạ, payload hỏng → bỏ cache, theo config. Cache không hợp lệ luôn nói
// ngược config (config local thì cache nói web, config web thì cache nói local) để thấy rõ nó không thắng.
func TestModeFromCacheRejectsWrongKeyRepoOrModeAndFallsBackToConfig(t *testing.T) {
	w := newWebFx(t)
	other := otherKey(t)
	cases := []struct {
		name string
		put  func(mode string)
	}{
		{"khóa khác", func(m string) { w.writeMode(w.repoID, m, other) }},
		{"repo khác", func(m string) { w.writeMode("khac", m, w.priv) }},
		{"chế độ lạ", func(m string) { w.writeMode(w.repoID, strings.ToUpper(m), w.priv) }},
		{"thiếu repo", func(m string) {
			w.writeSigned(".aisdlc/web/mode.json", []byte(`{"mode":"`+m+`"}`), w.priv)
		}},
		{"payload không phải JSON", func(m string) { w.writeSigned(".aisdlc/web/mode.json", []byte(m), w.priv) }},
		{"không phải phong bì", func(m string) {
			w.write(".aisdlc/web/mode.json", `{"repo":"`+w.repoID+`","mode":"`+m+`"}`)
		}},
	}
	for _, c := range cases {
		c.put("web")
		if got := w.cache().CachedMode(w.cfg()); got != gate.ModeLocal {
			t.Errorf("%s, config local: got %s", c.name, got)
		}
	}
	w.webMode()
	for _, c := range cases {
		c.put("local")
		if got := w.cache().CachedMode(w.cfg()); got != gate.ModeWeb {
			t.Errorf("%s, config web: got %s", c.name, got)
		}
	}
	// đối chứng: đúng khóa, đúng repo, "local" thì cache thắng config web
	w.writeMode(w.repoID, "local", w.priv)
	eq(t, w.cache().CachedMode(w.cfg()), gate.ModeLocal)
	eq(t, w.requests.Load(), int64(0))
}
