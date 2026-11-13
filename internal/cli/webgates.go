package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"aisdlc/internal/fsx"
	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/review"
	"aisdlc/internal/rewind"
	"aisdlc/internal/risk"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/spec"
	"aisdlc/internal/webclient"
)

// Phần có mạng của WebGates (Java WebGates): cầu nối CLI ↔ /api/cli của web ở chế độ duyệt trên web. Chỉ Mode,
// RefreshMode, Refresh, Submit, Act, Migrate, PolicyPull gọi mạng; hook chỉ dùng CachedMode và Store (webcache.go).
// Mọi phong bì từ web được xác minh bằng khóa đã ghim trước khi ghi cache (fail closed); token không bao giờ vào
// thông báo lỗi.

// WebGates là tên Java của WebCache: cùng một kiểu, phần chỉ đọc ở webcache.go, phần có mạng ở đây.
type WebGates = WebCache

const (
	webReadTimeout  = 2 * time.Second
	webWriteTimeout = 5 * time.Second
)

// FlagRef là cờ HIGH/WARN của review card lúc duyệt (R3).
type FlagRef struct {
	Code  string `json:"code"`
	Level string `json:"level"`
}

// ---- mạng ----

// unreachable: web không với tới được trong tiến trình này, hoặc đang backoff (lệnh không chờ lại 2 giây).
func (w *WebCache) unreachable() bool {
	return w.ctx.webDown || webclient.BackoffActive(w.root, w.ctx.now())
}

// down: sau một lỗi mạng, đánh dấu cả tiến trình và bật backoff cho các lệnh/tiến trình nền sau.
func (w *WebCache) down() {
	w.ctx.webDown = true
	_ = webclient.BackoffStart(w.root, w.ctx.now()) // không ghi được: vẫn còn cờ trong tiến trình
}

// do gửi một request tới web trong thời hạn timeout; lỗi chỉ cho mạng/URL/quá giờ (không chứa token).
func (w *WebCache) do(method, path, token string, body []byte, timeout time.Duration) (webclient.Response, error) {
	cfg, _ := w.Config()
	c := &webclient.Client{BaseURL: cfg.URL, Token: token, Now: w.ctx.now}
	std, cancel := context.WithTimeout(w.ctx.std, timeout)
	defer cancel()
	return c.Do(std, method, path, body)
}

// post gửi body JSON (5 giây); lỗi mạng → Abort(2, prefix + lỗi).
func (w *WebCache) post(path, token string, body any, prefix string) (webclient.Response, error) {
	b, err := jsonMarshal(body)
	if err != nil {
		return webclient.Response{}, abort(2, prefix+err.Error())
	}
	r, err := w.do(http.MethodPost, path, token, b, webWriteTimeout)
	if err != nil {
		return r, abort(2, prefix+w.scrub(err.Error()))
	}
	return r, nil
}

// webError là câu lỗi của server ({"error": ...}), không có thì "HTTP <status>". 401 → token hết hiệu lực; 403 của
// lớp xác thực ("forbidden", hoặc không có câu) → sai loại token; 403 nghiệp vụ giữ câu của server.
func (w *WebCache) webError(r webclient.Response) string {
	var n map[string]any
	server, has := "", false
	if json.Unmarshal(r.Body, &n) == nil {
		server, has = n["error"].(string)
	}
	switch {
	case r.Status == 401:
		return i18n.T("Token không còn hiệu lực, chạy lại `aisdlc web login`")
	case r.Status == 403 && (!has || strings.TrimSpace(server) == "" || server == "forbidden"):
		return i18n.T("Token không đúng loại (cần token ingest cho CLI, approve cho duyệt)")
	case has:
		return w.scrub(server)
	}
	return "HTTP " + strconv.Itoa(r.Status)
}

// scrub bỏ mọi token đã biết (ingest, approve) khỏi s: phòng server hay thư viện phản chiếu token vào câu lỗi.
func (w *WebCache) scrub(s string) string {
	cfg, _ := w.Config()
	for _, t := range []string{cfg.Token, w.approveToken()} {
		if t != "" {
			s = strings.ReplaceAll(s, t, "***")
		}
	}
	return s
}

// requireLogin: đã cấu hình web, đã ghim khóa, có định danh repo; không thì Abort(2, prefix + lý do).
func (w *WebCache) requireLogin(prefix string) (WebConfig, error) {
	c, ok := w.Config()
	if !ok {
		return c, abort(2, i18n.Tf("%sweb chưa cấu hình; chạy `aisdlc web login`.", prefix))
	}
	if w.PinnedKey() == nil {
		return c, abort(2, i18n.Tf("%schưa `aisdlc web login` (chưa ghim khóa web).", prefix))
	}
	if w.RepoID() == "" {
		return c, abort(2, i18n.Tf("%skhông xác định được định danh repo.", prefix))
	}
	return c, nil
}

// canCall: đủ điều kiện gọi web để làm mới (đã đăng nhập, có định danh repo, không đang backoff).
func (w *WebCache) canCall() (WebConfig, bool) {
	c, ok := w.Config()
	if !ok || w.PinnedKey() == nil || w.RepoID() == "" || w.unreachable() {
		return c, false
	}
	return c, true
}

// ---- chế độ ----

func configuredMode(cfg model.RepoConfig) gate.ApprovalMode {
	if cfg.Approvals == "web" {
		return gate.ModeWeb
	}
	return gate.ModeLocal
}

// Mode như CachedMode, nhưng chưa có cache hợp lệ thì hỏi web (2 giây) rồi lưu; lỗi → theo config. Cache nói local
// mà config.yaml nói web (dev vừa chuyển) thì cũng hỏi lại để web ghim chế độ web.
func (w *WebCache) Mode(cfg model.RepoConfig) gate.ApprovalMode {
	cached, ok := w.modeFromCache()
	if ok && (cached == gate.ModeWeb || configuredMode(cfg) == gate.ModeLocal) {
		return cached
	}
	if m, fresh := w.RefreshMode(cfg); fresh {
		return m
	}
	if ok {
		return cached
	}
	return configuredMode(cfg)
}

// RefreshMode báo chế độ trong config.yaml cho web, nhận phong bì chế độ, xác minh rồi lưu cache. ok=false khi
// không hỏi được hoặc phản hồi không xác minh được.
func (w *WebCache) RefreshMode(cfg model.RepoConfig) (gate.ApprovalMode, bool) {
	c, ok := w.canCall()
	if !ok {
		return "", false
	}
	configured := "local"
	if configuredMode(cfg) == gate.ModeWeb {
		configured = "web"
	}
	body, err := jsonMarshal(struct {
		Repo       string `json:"repo"`
		Configured string `json:"configured"`
	}{w.RepoID(), configured})
	if err != nil {
		return "", false
	}
	r, err := w.do(http.MethodPost, "/api/cli/mode", c.Token, body, webReadTimeout)
	if err != nil {
		w.down()
		return "", false
	}
	if !r.OK() {
		return "", false
	}
	m, ok := w.parseMode(r.Body)
	if !ok {
		return "", false
	}
	// phong bì đến muộn mà cũ hơn cache (đã xác minh) thì không ghi đè; chế độ theo cache
	if w.locked(func() bool { return w.writeModeIfNewerLocked(r.Body) }) {
		return m, true
	}
	if cm, ok := w.modeFromCache(); ok {
		return cm, true
	}
	return m, true
}

// writeModeIfNewerLocked ghi mode.json khi issuedAt của phong bì mới ≥ của cache hợp lệ đang có. Cache có issuedAt
// mà phong bì mới không có thì không ghi. Gọi khi đã giữ khóa gates.lock; true nếu đã ghi.
func (w *WebCache) writeModeIfNewerLocked(envelope []byte) bool {
	next, hasNext := w.issuedAt(envelope)
	var cur time.Time
	hasCur := false
	if _, ok := w.modeFromCache(); ok {
		if raw, err := os.ReadFile(w.ModeFile()); err == nil {
			cur, hasCur = w.issuedAt(raw)
		}
	}
	if hasCur && (!hasNext || next.Before(cur)) {
		return false
	}
	if err := fsx.WriteAtomic(w.ModeFile(), envelope, 0o644); err != nil {
		w.ctx.Eprintln(i18n.Tf("aisdlc web: không ghi được %s: %s", w.ModeFile(), err.Error()))
		return false
	}
	return true
}

// issuedAt của payload đã xác minh trong phong bì; false khi không có hoặc không đọc được.
func (w *WebCache) issuedAt(envelope []byte) (time.Time, bool) {
	payload, ok := w.verified(envelope)
	if !ok {
		return time.Time{}, false
	}
	var n map[string]any
	if json.Unmarshal(payload, &n) != nil {
		return time.Time{}, false
	}
	s, ok := n["issuedAt"].(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

// parseMode đọc chế độ trong phong bì đã xác minh của đúng repo (phong bì chế độ hay phong bì trạng thái cổng).
func (w *WebCache) parseMode(envelope []byte) (gate.ApprovalMode, bool) {
	payload, ok := w.verified(envelope)
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

// verified là payload của phong bì nếu chữ ký hợp lệ với khóa đang ghim.
func (w *WebCache) verified(envelope []byte) ([]byte, bool) {
	key := w.PinnedKey()
	if key == nil || envelope == nil {
		return nil, false
	}
	var env *snapshot.SignedEnvelope
	if json.Unmarshal(envelope, &env) != nil || env == nil {
		return nil, false
	}
	return env.Verify(key)
}

// ---- trạng thái cổng ----

// Refresh: GET trạng thái cổng (2 giây) → xác minh → ghi cache → nối feedback mới vào feedback.md. False khi chưa
// đăng nhập, lỗi mạng, hoặc phản hồi không xác minh được (cache cũ giữ nguyên).
func (w *WebCache) Refresh(key model.FeatureKey) bool {
	c, ok := w.canCall()
	if !ok {
		return false
	}
	r, err := w.do(http.MethodGet, "/api/cli/gates?repo="+enc(w.RepoID())+"&feature="+enc(key.String()), c.Token,
		nil, webReadTimeout)
	if err != nil {
		w.down()
		return false
	}
	if !r.OK() {
		return false
	}
	if w.accept(key, r.Body) {
		return true
	}
	// web đã đổi khóa: chỉ nhận khóa mới khi khóa đang ghim ký xác nhận, rồi thử lại một lần
	rotated, err := w.rotate(c, r.Body)
	if err != nil {
		w.down()
		return false
	}
	return rotated && w.accept(key, r.Body)
}

// refreshGates làm mới trạng thái cổng của tính năng từ web rồi cho GateStore đọc lại cache.
func refreshGates(_ *Context, f *Feature) (bool, error) {
	ok := f.Web.Refresh(f.Key)
	return ok, f.Store.Refresh()
}

// rotate: phong bì ký bằng khóa khác khóa đang ghim. Hỏi /api/cli/pubkey; khóa trả về đúng là khóa đã ký phong bì
// và được khóa đang ghim xác nhận (endorsement) thì ghim lại. Lỗi mạng → err; mọi lỗi khác → false, khóa ghim giữ
// nguyên.
func (w *WebCache) rotate(c WebConfig, envelope []byte) (bool, error) {
	cur := w.PinnedKey()
	home, ok := w.ctx.Home()
	if cur == nil || !ok {
		return false, nil
	}
	var env *snapshot.SignedEnvelope
	if json.Unmarshal(envelope, &env) != nil || env == nil || env.KeyID == "" ||
		env.KeyID == snapshot.KeyID(cur) {
		return false, nil
	}
	r, err := w.do(http.MethodGet, "/api/cli/pubkey", c.Token, nil, webReadTimeout)
	if err != nil {
		return false, err
	}
	if !r.OK() {
		return false, nil
	}
	var n map[string]any
	if json.Unmarshal(r.Body, &n) != nil {
		return false, nil
	}
	pub, err := snapshot.PublicKeyFromBase64(jsonText(n["publicKey"]))
	if err != nil || snapshot.KeyID(pub) != env.KeyID {
		return false, nil
	}
	endorsement, _ := n["endorsement"].(string)
	if _, err := webclient.AcceptKey(home, pub, endorsement); err != nil {
		return false, nil // khóa mới không được khóa đang ghim ký
	}
	w.pin, w.pinDone = pub, true
	w.ctx.Eprintln(i18n.Tf("aisdlc web: web đã đổi khóa ký; đã ghim khóa mới %s (được khóa cũ xác nhận).", env.KeyID))
	return true, nil
}

// accept xác minh phong bì trạng thái cổng của đúng repo/tính năng rồi ghi cache và feedback mới (dưới khóa).
func (w *WebCache) accept(key model.FeatureKey, envelope []byte) bool {
	payload, ok := w.verified(envelope)
	if !ok {
		return false
	}
	s, err := snapshot.FeatureGateStateFromJSON(payload)
	if err != nil || s.Feature != key.String() || w.RepoID() == "" || s.Repo != w.RepoID() {
		return false
	}
	return w.locked(func() bool {
		// phong bì đến muộn mà cũ hơn cache (đã xác minh) thì không ghi đè
		cached, ok := gate.ReadWebGateState(w.CacheFile(key), w.PinnedKey(), w.RepoID(), key)
		if ok && !cached.IssuedAt.IsZero() && (s.IssuedAt.IsZero() || s.IssuedAt.Before(cached.IssuedAt)) {
			return true
		}
		if err := fsx.WriteAtomic(w.CacheFile(key), envelope, 0o644); err != nil {
			w.ctx.Eprintln(i18n.Tf("aisdlc web: không ghi được %s: %s", w.CacheFile(key), err.Error()))
			return false
		}
		// phong bì đã xác minh nói repo ở local (lead vừa chuyển): lưu làm phong bì chế độ để CLI/hook theo ngay.
		// parseMode đọc "repo"/"mode" của payload, nên phong bì cổng dùng được nguyên chữ ký.
		if s.Mode == "local" {
			w.writeModeIfNewerLocked(envelope)
		}
		w.appendFeedback(key, s)
		return true
	})
}

// locked chạy body dưới khóa file .aisdlc/web/gates.lock (lệnh CLI và `web flush` có thể làm mới cùng lúc). flock
// gắn với từng lần mở file nên hai goroutine trong một tiến trình cũng loại trừ nhau. Lỗi khóa → false.
func (w *WebCache) locked(body func() bool) bool {
	lock := filepath.Join(w.Dir(), "gates.lock")
	fail := func(err error) bool {
		w.ctx.Eprintln(i18n.Tf("aisdlc web: không khóa được %s: %s", lock, err.Error()))
		return false
	}
	if err := os.MkdirAll(w.Dir(), 0o755); err != nil {
		return fail(err)
	}
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fail(err)
	}
	defer f.Close()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		return fail(err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return body()
}

// appendFeedback nối feedback (trả lại / mở lại trên web) chưa có trong feedback.md (theo id, dòng
// <!-- aisdlc-fb:<id> -->) vào cuối, cùng định dạng tiêu đề với local. Gọi dưới locked.
func (w *WebCache) appendFeedback(key model.FeatureKey, s snapshot.FeatureGateState) {
	if len(s.Feedback) == 0 {
		return
	}
	p := model.FeaturePaths{RepoRoot: w.root, Key: key}
	if st, err := os.Stat(p.Dir()); err != nil || !st.IsDir() {
		return
	}
	existing := ""
	if st, err := os.Stat(p.Feedback()); err == nil && st.Mode().IsRegular() {
		b, err := os.ReadFile(p.Feedback())
		if err != nil {
			w.ctx.Eprintln(i18n.Tf("aisdlc web: không ghi được feedback.md: %s", err.Error()))
			return
		}
		existing = string(b)
	}
	var add strings.Builder
	for _, fb := range s.Feedback {
		if fb.Gate == "" || fb.At.IsZero() {
			continue
		}
		marker := "<!-- aisdlc-fb:" + strconv.FormatInt(fb.ID, 10) + " -->"
		if strings.Contains(existing, marker+"\n") || strings.Contains(add.String(), marker+"\n") {
			continue
		}
		kind := i18n.T("trả lại")
		switch fb.Kind {
		case "REOPENED":
			kind = i18n.T("mở lại")
		case "REWOUND":
			kind = i18n.T("quay lại mốc cũ")
		}
		add.WriteString(i18n.Tf("## %s — %s bởi %s lúc %s\n%s\n\n", fb.Gate, kind, oneLineOr(fb.By), review.InstantString(fb.At), marker))
		if c := strings.TrimSpace(fb.Comment); c != "" {
			add.WriteString(c + "\n\n")
		}
	}
	if add.Len() == 0 {
		return
	}
	if err := fsx.WriteAtomic(p.Feedback(), []byte(existing+add.String()), 0o644); err != nil {
		w.ctx.Eprintln(i18n.Tf("aisdlc web: không ghi được feedback.md: %s", err.Error()))
	}
}

func oneLineOr(s string) string {
	if strings.TrimSpace(s) == "" {
		return "?"
	}
	return strings.Join(strings.Fields(s), " ")
}

// ---- nộp / duyệt ----

type webFinding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type webArtifact struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
}

func webFindings(fs []spec.Finding) []webFinding {
	out := make([]webFinding, 0, len(fs))
	for _, x := range fs {
		out = append(out, webFinding{x.Code, x.Message})
	}
	return out
}

// webArtifacts là artifact gửi toàn văn base64 theo đúng thứ tự tính hash; file không có → content null.
func webArtifacts(c []model.Content) []webArtifact {
	out := make([]webArtifact, 0, len(c))
	for _, e := range c {
		a := webArtifact{Path: filepath.ToSlash(e.Path)}
		if e.Data != nil {
			s := base64.StdEncoding.EncodeToString(e.Data)
			a.Content = &s
		}
		out = append(out, a)
	}
	return out
}

// Submit nộp cổng lên web (5 giây): artifact gửi toàn văn; riskLevel (mức trong risk.yaml, "" khi không có) để server
// tự duyệt theo flow. Lỗi → Abort(2, "Không nộp được lên web: ..."), không ghi gì.
func (w *WebCache) Submit(f *Feature, g gate.Gate, findings []spec.Finding, riskLevel string) error {
	prefix := i18n.T("Không nộp được lên web: ")
	c, err := w.requireLogin(prefix)
	if err != nil {
		return err
	}
	entries, err := g.ArtifactContents(f.Paths, f.Flow())
	if err != nil {
		return err
	}
	type riskRef struct {
		Level string `json:"level"`
	}
	body := struct {
		Repo      string        `json:"repo"`
		Feature   string        `json:"feature"`
		Gate      string        `json:"gate"`
		Hash      string        `json:"hash"`
		Risk      *riskRef      `json:"risk,omitempty"`
		Findings  []webFinding  `json:"findings"`
		Artifacts []webArtifact `json:"artifacts"`
	}{w.RepoID(), f.Key.String(), string(g), model.SHA256Contents(entries), nil, webFindings(findings),
		webArtifacts(entries)}
	if riskLevel != "" {
		body.Risk = &riskRef{riskLevel}
	}
	r, err := w.post("/api/cli/submissions", c.Token, body, prefix)
	if err != nil {
		return err
	}
	if !r.OK() {
		return abort(2, prefix+w.webError(r))
	}
	if !w.accept(f.Key, r.Body) {
		w.ctx.Eprintln(i18n.T("aisdlc web: web đã nhận bản nộp nhưng phản hồi không xác minh được bằng khóa đã ghim; chạy `aisdlc status` để làm mới."))
	}
	return f.Store.Refresh()
}

// approveToken là token approve đã lưu (~/.config/aisdlc/approve-token); "" khi chưa có.
func (w *WebCache) approveToken() string {
	home, ok := w.ctx.Home()
	if !ok {
		return ""
	}
	return webclient.LoadApproveToken(home)
}

// RequireApproveToken: chưa có token approve thì Abort(2) kèm link duyệt trên web.
func (w *WebCache) RequireApproveToken(f *Feature, g gate.Gate) error {
	if w.approveToken() != "" {
		return nil
	}
	u := i18n.T("<url web>")
	if c, ok := w.Config(); ok {
		u = webclient.SafeURL(c.URL, c.Token) // không in userinfo hay token
	}
	return abort(2, i18n.Tf("Chưa có token approve. Duyệt trên web: %s/f/%s/%s?g=%s hoặc chạy `aisdlc web approve-token`", u, repoPath(w.RepoID()), enc(f.Key.String()), string(g)))
}

// Act duyệt / trả lại / mở lại (action: approve|reject|reopen) bằng token approve (5 giây). Server 409 → Abort(1,
// câu của server), trừ khi repo không ở chế độ web (Abort 2); lỗi khác → Abort(2). Thành công → cache cập nhật từ
// phong bì trả về. channel: gate.ApprovalClaudeCode khi xác nhận đến từ phiếu của Claude Code; còn lại terminal.
func (w *WebCache) Act(action string, f *Feature, g gate.Gate, comment *string, flags []FlagRef, channel string) error {
	if err := w.RequireApproveToken(f, g); err != nil {
		return err
	}
	prefix := i18n.T("Không gửi được lên web: ")
	if _, err := w.requireLogin(prefix); err != nil {
		return err
	}
	if channel != gate.ApprovalClaudeCode {
		channel = "terminal"
	}
	if flags == nil {
		flags = []FlagRef{}
	}
	body := struct {
		Repo            string    `json:"repo"`
		Feature         string    `json:"feature"`
		Gate            string    `json:"gate"`
		Channel         string    `json:"channel"`
		Hash            string    `json:"hash,omitempty"`
		Comment         *string   `json:"comment,omitempty"`
		Flags           []FlagRef `json:"flags"`
		AssumptionsSeen []string  `json:"assumptionsSeen"`
	}{Repo: w.RepoID(), Feature: f.Key.String(), Gate: string(g), Channel: channel, Comment: comment, Flags: flags,
		AssumptionsSeen: []string{}}
	if action == "approve" {
		h, err := f.Gates.CurrentHash(g)
		if err != nil {
			return err
		}
		body.Hash = h
	}
	r, err := w.post("/api/gates/"+action, w.approveToken(), body, prefix)
	if err != nil {
		return err
	}
	if r.Status == http.StatusConflict {
		e := w.webError(r)
		// thiếu điều kiện (repo không ở chế độ web) là exit 2; vi phạm luật duyệt là exit 1
		if isNotWeb(r, e) {
			return abort(2, e)
		}
		return abort(1, e)
	}
	if !r.OK() {
		return abort(2, prefix+w.webError(r))
	}
	if !w.accept(f.Key, r.Body) {
		w.ctx.Eprintln(i18n.T("aisdlc web: phản hồi không xác minh được bằng khóa đã ghim; chạy `aisdlc status`."))
	}
	return f.Store.Refresh()
}

// Rewind báo server quay tính năng về mốc cp: xoá bản nộp/duyệt/feedback của gates, tăng attempt. requestID là tên ref
// dự phòng (chạy lại sau khi đứt dùng cùng giá trị, server coi là cùng một lần). Xử lý lỗi như Act; thành công → cache
// cập nhật từ phong bì trả về. Gọi trước mọi thao tác xoá cục bộ.
func (w *WebCache) Rewind(f *Feature, cp rewind.Checkpoint, gates []gate.Gate, requestID, channel string) error {
	first := cp.Gate
	if len(gates) > 0 {
		first = gates[0]
	}
	if err := w.RequireApproveToken(f, first); err != nil {
		return err
	}
	prefix := i18n.T("Không gửi được lên web: ")
	if _, err := w.requireLogin(prefix); err != nil {
		return err
	}
	if channel != gate.ApprovalClaudeCode {
		channel = "terminal"
	}
	names := make([]string, len(gates))
	for i, g := range gates {
		names[i] = string(g)
	}
	body := struct {
		Repo       string   `json:"repo"`
		Feature    string   `json:"feature"`
		Checkpoint string   `json:"checkpoint"`
		Gates      []string `json:"gates"`
		RequestID  string   `json:"requestId"`
		Channel    string   `json:"channel"`
	}{w.RepoID(), f.Key.String(), cp.ID, names, requestID, channel}
	r, err := w.post("/api/gates/rewind", w.approveToken(), body, prefix)
	if err != nil {
		return err
	}
	if r.Status == http.StatusConflict {
		e := w.webError(r)
		if isNotWeb(r, e) {
			return abort(2, e)
		}
		return abort(1, e)
	}
	if !r.OK() {
		return abort(2, prefix+w.webError(r))
	}
	if !w.accept(f.Key, r.Body) {
		w.ctx.Eprintln(i18n.T("aisdlc web: phản hồi không xác minh được bằng khóa đã ghim; chạy `aisdlc status`."))
	}
	return f.Store.Refresh()
}

// Migrate (`web migrate`): gửi mỗi approval cục bộ (gates.json) còn khớp nội dung hiện tại lên /api/cli/import.
// REVIEW không gửi (web từ chối nhập REVIEW). Trả số bản đã gửi; lỗi từng bản in ra stderr rồi bỏ qua.
func (w *WebCache) Migrate(f *Feature) (int, error) {
	c, err := w.requireLogin(i18n.T("Không gửi được lên web: "))
	if err != nil {
		return 0, err
	}
	local := gate.NewLocalGateStore(f.Paths)
	sent := 0
	for _, g := range gate.Sequence(f.Flow()) {
		a, ok, err := local.Approval(g)
		if err != nil {
			return sent, err
		}
		if !ok || a.Superseded { // bản tự duyệt đã thay thế: chưa duyệt
			continue
		}
		if g == gate.REVIEW {
			// web không nhận nhập cổng REVIEW (cần kiểm eval trên cây code hiện tại): duyệt lại trên web
			w.ctx.Println(i18n.Tf("Bỏ qua REVIEW của %s: web không nhận nhập cổng REVIEW; nộp lại `aisdlc submit review` rồi duyệt trên web.", f.Key.String()))
			continue
		}
		entries, err := g.ArtifactContents(f.Paths, f.Flow())
		if err != nil {
			return sent, err
		}
		hash := model.SHA256Contents(entries)
		if hash != a.Hash {
			continue
		}
		var findings []spec.Finding
		if s, ok, err := local.Submission(g); err == nil && ok {
			findings = s.Findings
		}
		body := struct {
			Repo      string        `json:"repo"`
			Feature   string        `json:"feature"`
			Gate      string        `json:"gate"`
			Hash      string        `json:"hash"`
			Approver  string        `json:"approver"`
			At        string        `json:"at"`
			Findings  []webFinding  `json:"findings"`
			Artifacts []webArtifact `json:"artifacts"`
		}{w.RepoID(), f.Key.String(), string(g), hash, a.Approver, review.InstantString(a.At),
			webFindings(findings), webArtifacts(entries)}
		r, err := w.post("/api/cli/import", c.Token, body, "")
		var ab *Abort
		switch {
		case asAbort(err, &ab):
			w.ctx.Eprintln(i18n.Tf("Không gửi được %s %s: %s", f.Key.String(), string(g), ab.Msg))
		case err != nil:
			return sent, err
		case r.OK():
			sent++
			w.accept(f.Key, r.Body)
		default:
			w.ctx.Eprintln(i18n.Tf("Không gửi được %s %s: %s", f.Key.String(), string(g), w.webError(r)))
		}
	}
	return sent, nil
}

// ---- chính sách rủi ro (PolicySync) ----

// PolicyPull (`aisdlc policy pull`): GET /api/cli/policy; 404 (web chưa có) thì gửi chính sách trong repo lên
// (không có file thì mẫu generic bỏ flow) rồi hỏi lại; xác minh rồi ghi .aisdlc/web/policy.json. Lỗi → Abort(2).
func (w *WebCache) PolicyPull() (snapshot.CachedPolicy, error) {
	prefix := i18n.T("Không lấy được chính sách từ web: ")
	c, ok := w.Config()
	if !ok || w.PinnedKey() == nil {
		return snapshot.CachedPolicy{}, abort(2, i18n.Tf("%schưa `aisdlc web login` trên máy này.", prefix))
	}
	if w.RepoID() == "" {
		return snapshot.CachedPolicy{}, abort(2, i18n.Tf("%skhông xác định được định danh repo.", prefix))
	}
	netErr := func(err error) (snapshot.CachedPolicy, error) {
		w.down()
		return snapshot.CachedPolicy{}, abort(2, prefix+w.scrub(err.Error()))
	}
	get := func() (webclient.Response, error) {
		return w.do(http.MethodGet, "/api/cli/policy?repo="+enc(w.RepoID()), c.Token, nil, webReadTimeout)
	}
	r, err := get()
	if err != nil {
		return netErr(err)
	}
	if r.Status == http.StatusNotFound {
		if err := w.importRepoPolicy(c, prefix); err != nil {
			var ab *Abort
			if asAbort(err, &ab) {
				return snapshot.CachedPolicy{}, err
			}
			return netErr(err)
		}
		if r, err = get(); err != nil {
			return netErr(err)
		}
	}
	if !r.OK() {
		return snapshot.CachedPolicy{}, abort(2, prefix+"HTTP "+strconv.Itoa(r.Status))
	}
	p, ok := snapshot.VerifyPolicy(r.Body, w.PinnedKey(), w.RepoID())
	if !ok {
		rotated, err := w.rotate(c, r.Body)
		if err != nil {
			return netErr(err)
		}
		if rotated {
			p, ok = snapshot.VerifyPolicy(r.Body, w.PinnedKey(), w.RepoID())
		}
	}
	if !ok {
		return snapshot.CachedPolicy{}, abort(2, i18n.Tf("%sphản hồi không xác minh được bằng khóa đã ghim (hoặc sai repo).", prefix))
	}
	if err := fsx.WriteAtomic(snapshot.PolicyCacheFile(w.root), r.Body, 0o644); err != nil {
		return snapshot.CachedPolicy{}, err
	}
	return p, nil
}

// importRepoPolicy: lần đầu repo kết nối web, gửi chính sách trong repo lên (web nhập làm phiên bản 1). Lỗi mạng
// trả nguyên (người gọi bật backoff); web từ chối → Abort(2).
func (w *WebCache) importRepoPolicy(c WebConfig, prefix string) error {
	var yaml string
	f := filepath.Join(w.root, RepoPolicyFile)
	if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() {
		b, err := os.ReadFile(f)
		if err != nil {
			return abort(2, prefix+err.Error())
		}
		yaml = string(b)
	} else {
		p, err := risk.LatestPreset("generic")
		if err != nil {
			return abort(2, prefix+err.Error())
		}
		if yaml, err = withoutFlow(p).ToYAML(); err != nil {
			return abort(2, prefix+err.Error())
		}
	}
	body, err := jsonMarshal(struct {
		Repo string `json:"repo"`
		YAML string `json:"yaml"`
	}{w.RepoID(), yaml})
	if err != nil {
		return abort(2, prefix+err.Error())
	}
	r, err := w.do(http.MethodPost, "/api/cli/policy/import", c.Token, body, webWriteTimeout)
	if err != nil {
		return err
	}
	if !r.OK() {
		// 400 kèm lỗi chính sách (vd. còn schema 1: nhắc `aisdlc policy migrate`) thì in lỗi đó
		var e struct {
			Error string `json:"error"`
		}
		if r.Status == http.StatusBadRequest && json.Unmarshal(r.Body, &e) == nil && strings.TrimSpace(e.Error) != "" {
			return abort(2, i18n.Tf("%sweb không nhận chính sách (HTTP 400): %s", prefix, w.scrub(oneLineErr(errors.New(e.Error)))))
		}
		return abort(2, i18n.Tf("%sweb không nhận chính sách (HTTP %s).", prefix, strconv.Itoa(r.Status)))
	}
	return nil
}

// PolicyPullQuietly kéo chính sách ở các lần đồng bộ sẵn có (status, flush nền, nộp cổng): mọi lỗi im lặng, cache
// cũ giữ nguyên.
func (w *WebCache) PolicyPullQuietly() {
	defer func() { _ = recover() }()
	if w.PolicyWebMode() && !w.unreachable() {
		_, _ = w.PolicyPull()
	}
}

// ---- tiện ích ----

// enc mã hóa thành phần URL như URLEncoder của Java, khoảng trắng thành %20.
func enc(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

// repoPath mã hóa từng đoạn của repoId như trang web (không sinh %2F).
func repoPath(repoID string) string {
	if repoID == "" {
		return "<repo>"
	}
	segs := strings.Split(repoID, "/")
	for i, s := range segs {
		segs[i] = enc(s)
	}
	return strings.Join(segs, "/")
}
