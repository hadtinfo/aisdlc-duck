package cli

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
)

// Phần chỉ đọc của web client (giai đoạn 2b): cấu hình web, khóa ghim, định danh repo và các cache phong bì ký
// trong .aisdlc/web/. Không có gì ở đây gọi mạng; phần có mạng (login, refresh, nộp/duyệt, outbox) ở giai đoạn 4.

// WebConfig là cấu hình web của máy: ~/.config/aisdlc/web.json (do `aisdlc web login` ghi) nếu có; chỉ khi không
// có file đó mới dùng env AISDLC_WEB_URL + AISDLC_WEB_TOKEN.
type WebConfig struct {
	URL   string
	Token string
}

// LoadWebConfig đọc cấu hình web như WebConfig.load của Java; ok=false khi chưa cấu hình.
func LoadWebConfig(ctx *Context) (WebConfig, bool) {
	if c, ok := webConfigFromFile(ctx); ok {
		return c, true
	}
	u, okU := ctx.Env("AISDLC_WEB_URL")
	t, okT := ctx.Env("AISDLC_WEB_TOKEN")
	if okU && okT {
		return newWebConfig(u, t), true
	}
	return WebConfig{}, false
}

// envTokenWarning: token đăng nhập web đặt trong env (agent đọc được).
const envTokenWarning = "AISDLC_WEB_TOKEN đang đặt trong môi trường. Token đăng nhập" +
	" (login) không dùng cho CLI và agent đọc được biến này: xóa nó khỏi môi trường, dùng `aisdlc web login`" +
	" với token ingest."

// warnEnvToken in envTokenWarning ra stderr khi biến token web đang đặt; không bao giờ in giá trị token.
func warnEnvToken(ctx *Context) {
	if _, ok := ctx.Env("AISDLC_WEB_TOKEN"); ok {
		ctx.Eprintln("aisdlc web: " + envTokenWarning)
	}
}

func webConfigFromFile(ctx *Context) (WebConfig, bool) {
	home, ok := ctx.Home()
	if !ok {
		return WebConfig{}, false
	}
	f := filepath.Join(WebConfigDir(home), "web.json")
	if st, err := os.Stat(f); err != nil || !st.Mode().IsRegular() {
		return WebConfig{}, false
	}
	b, err := os.ReadFile(f)
	var n map[string]any
	if err == nil {
		err = json.Unmarshal(b, &n)
	}
	if err != nil {
		ctx.Eprintln("aisdlc web: không đọc được " + f + ": " + err.Error())
		return WebConfig{}, false
	}
	u, t := strings.TrimSpace(jsonText(n["url"])), strings.TrimSpace(jsonText(n["token"]))
	if u == "" || t == "" {
		return WebConfig{}, false
	}
	return newWebConfig(u, t), true
}

// jsonText như JsonNode.asText(""): chuỗi, số và bool thành chữ; còn lại rỗng.
func jsonText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func newWebConfig(url, token string) WebConfig {
	return WebConfig{URL: strings.TrimRight(strings.TrimSpace(url), "/"), Token: strings.TrimSpace(token)}
}

// WebConfigDir là thư mục cấu hình web của dev: <home>/.config/aisdlc.
func WebConfigDir(home string) string { return filepath.Join(home, ".config", "aisdlc") }

// PinnedKeyFile là file khóa công khai đã ghim của server web: <home>/.config/aisdlc/web-pubkey.
func PinnedKeyFile(home string) string { return filepath.Join(WebConfigDir(home), "web-pubkey") }

// LoadPinnedKey đọc khóa đã ghim (base64 X.509); nil khi chưa ghim hoặc file hỏng. Không bao giờ lỗi.
func LoadPinnedKey(home string) ed25519.PublicKey {
	b, err := os.ReadFile(PinnedKeyFile(home))
	if err != nil {
		return nil
	}
	k, err := snapshot.PublicKeyFromBase64(string(b))
	if err != nil {
		return nil
	}
	return k
}

// WebCache là phần đọc cache của WebGates: cấu hình, khóa ghim và định danh repo đọc lười (repo chế độ local
// không tốn gì thêm).
type WebCache struct {
	ctx  *Context
	root string

	cfgDone, pinDone, repoDone bool
	cfg                        WebConfig
	cfgOK                      bool
	pin                        ed25519.PublicKey
	repo                       string
}

// NewWebCache tạo WebCache cho repo root.
func NewWebCache(ctx *Context, root string) *WebCache { return &WebCache{ctx: ctx, root: root} }

// Config là cấu hình web của máy (LoadWebConfig, nhớ kết quả).
func (w *WebCache) Config() (WebConfig, bool) {
	if !w.cfgDone {
		w.cfg, w.cfgOK = LoadWebConfig(w.ctx)
		w.cfgDone = true
	}
	return w.cfg, w.cfgOK
}

// PinnedKey là khóa đã ghim theo HOME; nil nếu chưa `aisdlc web login`.
func (w *WebCache) PinnedKey() ed25519.PublicKey {
	if !w.pinDone {
		if home, ok := w.ctx.Home(); ok {
			w.pin = LoadPinnedKey(home)
		}
		w.pinDone = true
	}
	return w.pin
}

// LoggedIn: đã `aisdlc web login` (có khóa ghim).
func (w *WebCache) LoggedIn() bool { return w.PinnedKey() != nil }

// RepoID là định danh repo như web thấy; "" nếu không tính được.
func (w *WebCache) RepoID() string {
	if !w.repoDone {
		w.repo = snapshot.RepoID(w.ctx.std, w.ctx.Git(w.root), w.root)
		w.repoDone = true
	}
	return w.repo
}

// Dir là .aisdlc/web của repo.
func (w *WebCache) Dir() string { return filepath.Join(w.root, ".aisdlc", "web") }

// ModeFile là .aisdlc/web/mode.json: phong bì chế độ duyệt đã xác minh.
func (w *WebCache) ModeFile() string { return filepath.Join(w.Dir(), "mode.json") }

// CacheFile là .aisdlc/web/gates/<KEY>.json: phong bì trạng thái cổng mới nhất đã xác minh.
func (w *WebCache) CacheFile(key model.FeatureKey) string {
	return filepath.Join(w.Dir(), "gates", key.String()+".json")
}

// Store là GateStore đọc cache đã ký (không khóa ghim thì không có approval nào).
func (w *WebCache) Store(key model.FeatureKey) gate.GateStore {
	return gate.NewWebGateStore(w.CacheFile(key), w.PinnedKey(), w.RepoID(), key)
}

// CachedMode không gọi mạng (WebGates.cachedMode): phong bì chế độ trong cache hợp lệ thì theo nó; không thì theo
// approvals của config.yaml.
func (w *WebCache) CachedMode(cfg model.RepoConfig) gate.ApprovalMode {
	if m, ok := w.modeFromCache(); ok {
		return m
	}
	if cfg.Approvals == "web" {
		return gate.ModeWeb
	}
	return gate.ModeLocal
}

func (w *WebCache) modeFromCache() (gate.ApprovalMode, bool) {
	f := w.ModeFile()
	if st, err := os.Stat(f); err != nil || !st.Mode().IsRegular() || w.PinnedKey() == nil {
		return "", false
	}
	raw, err := os.ReadFile(f)
	if err != nil {
		return "", false
	}
	var env *snapshot.SignedEnvelope
	if json.Unmarshal(raw, &env) != nil || env == nil {
		return "", false
	}
	payload, ok := env.Verify(w.PinnedKey())
	if !ok {
		return "", false
	}
	var n map[string]any
	if json.Unmarshal(payload, &n) != nil {
		return "", false
	}
	repo, _ := n["repo"].(string)
	if id := w.RepoID(); id == "" || repo != id {
		return "", false
	}
	switch jsonText(n["mode"]) {
	case "web":
		return gate.ModeWeb, true
	case "local":
		return gate.ModeLocal, true
	}
	return "", false
}

// Control là điều khiển từ web của tính năng trong cache đã xác minh (khóa ghim, đúng repo và tính năng); nil nếu
// không có. Không có file cache thì không tốn đọc khóa ghim hay định danh repo.
func (w *WebCache) Control(key model.FeatureKey) *snapshot.FeatureControl {
	f := snapshot.ControlCacheFile(w.root, key)
	if !exists(f) {
		return nil
	}
	c, ok := snapshot.ReadControlCache(f, w.PinnedKey(), w.RepoID(), key)
	if !ok {
		return nil
	}
	return &c
}

// Policy là chính sách rủi ro từ web trong .aisdlc/web/policy.json đã xác minh (khóa ghim, đúng repo).
func (w *WebCache) Policy() (snapshot.CachedPolicy, bool) {
	f := snapshot.PolicyCacheFile(w.root)
	if !exists(f) {
		return snapshot.CachedPolicy{}, false
	}
	return snapshot.ReadPolicyCache(f, w.PinnedKey(), w.RepoID())
}

// PolicyWebMode: repo dùng chính sách từ web (đã cấu hình web và đã đăng nhập), như PolicySync.webMode.
func (w *WebCache) PolicyWebMode() bool {
	_, ok := w.Config()
	return ok && w.LoggedIn()
}

// readLedger đọc sổ cục bộ .aisdlc/web/control/<KEY>.local.json (ControlLedger của Java, chỉ đọc):
// {"delivered":[id...],"results":{"<id>":"APPLIED|STALE"}}. Thiếu hoặc hỏng thì rỗng.
func readLedger(root string, key model.FeatureKey) (delivered, done map[int64]bool) {
	delivered, done = map[int64]bool{}, map[int64]bool{}
	b, err := os.ReadFile(filepath.Join(root, ".aisdlc", "web", "control", key.String()+".local.json"))
	if err != nil {
		return
	}
	var l struct {
		Delivered []int64           `json:"delivered"`
		Results   map[string]string `json:"results"`
	}
	if json.Unmarshal(b, &l) != nil {
		return
	}
	for _, id := range l.Delivered {
		delivered[id] = true
	}
	for k := range l.Results {
		if id, err := strconv.ParseInt(k, 10, 64); err == nil {
			done[id] = true
		}
	}
	return
}
