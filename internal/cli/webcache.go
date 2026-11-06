package cli

import (
	"crypto/ed25519"
	"os"
	"path/filepath"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
)

// Phần chỉ đọc của web client: cấu hình web, khóa ghim, định danh repo và các cache phong bì ký trong .aisdlc/web/.
// Không có gì ở đây gọi mạng; phần có mạng (chế độ, làm mới, nộp/duyệt, migrate, chính sách) ở webgates.go.

// WebConfig là cấu hình web của máy (xem webclient.Config).
type WebConfig = webclient.Config

// LoadWebConfig đọc cấu hình web như WebConfig.load của Java; ok=false khi chưa cấu hình. Cảnh báo (file hỏng) ra
// stderr.
func LoadWebConfig(ctx *Context) (WebConfig, bool) {
	home, _ := ctx.Home()
	c, ok, warns := webclient.LoadConfig(home, ctx.envValue)
	for _, w := range warns {
		ctx.Eprintln(w)
	}
	return c, ok
}

// warnEnvToken in cảnh báo ra stderr khi biến token web đang đặt; không bao giờ in giá trị token.
func warnEnvToken(ctx *Context) {
	if w, set := webclient.EnvTokenWarning(ctx.envValue); set {
		ctx.Eprintln(w)
	}
}

// jsonText như JsonNode.asText(""): chuỗi, số và bool thành chữ; còn lại rỗng.
func jsonText(v any) string { return webclient.JSONText(v) }

// WebConfigDir là thư mục cấu hình web của dev: <home>/.config/aisdlc.
func WebConfigDir(home string) string { return webclient.ConfigDir(home) }

// PinnedKeyFile là file khóa công khai đã ghim của server web: <home>/.config/aisdlc/web-pubkey.
func PinnedKeyFile(home string) string { return webclient.PinnedKeyFile(home) }

// LoadPinnedKey đọc khóa đã ghim (base64 X.509); nil khi chưa ghim hoặc file hỏng. Không bao giờ lỗi.
func LoadPinnedKey(home string) ed25519.PublicKey { return webclient.LoadPinnedKey(home) }

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
	return configuredMode(cfg)
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
	return w.parseMode(raw)
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
