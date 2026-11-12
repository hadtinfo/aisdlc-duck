package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"aisdlc/internal/i18n"
	"aisdlc/internal/risk"
	"aisdlc/internal/web/httpx"
)

// maxBytes là giới hạn body của /api/policy/** và /api/cli/policy/import.
const maxBytes = 256 * 1024

// Routes đăng ký GET /api/cli/policy, POST /api/cli/policy/import, GET /api/policy, GET /api/policy/versions,
// POST /api/policy/{publish,validate,migrate-draft}.
func Routes(r chi.Router, s *Service) {
	limit := httpx.LimitBodyWith(maxBytes, httpx.NewError(http.StatusRequestEntityTooLarge, i18n.VI.T("Payload quá lớn")))
	r.Get("/api/cli/policy", wrap(s.cliGet))
	r.With(limit).Post("/api/cli/policy/import", wrap(s.cliImport))
	r.Get("/api/policy", wrap(s.view))
	r.Get("/api/policy/versions", wrap(s.versions))
	r.With(limit).Post("/api/policy/publish", wrap(s.publish))
	r.With(limit).Post("/api/policy/validate", wrap(s.validate))
	r.With(limit).Post("/api/policy/migrate-draft", wrap(s.migrateDraft))
}

func wrap(f func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(w, r); err != nil {
			writeErr(w, r, err)
		}
	}
}

// writeErr: RiskPolicyException → 400 {"error": lỗi đầu, "errors": [...]}; Conflict → 409 {"message","error"}.
// Chữ theo locale của request.
func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	loc := httpx.LocaleFrom(r.Context())
	var pe *risk.PolicyError
	if errors.As(err, &pe) {
		msg := loc.T("Chính sách không hợp lệ")
		errs := localizeAll(loc, pe.Errors)
		if len(errs) > 0 {
			msg = errs[0]
		}
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": msg, "errors": errs})
		return
	}
	var c *Conflict
	if errors.As(err, &c) {
		m := c.MessageIn(loc)
		httpx.WriteJSON(w, http.StatusConflict, map[string]string{"message": m, "error": m})
		return
	}
	httpx.Fail(w, r, err)
}

// localizeAll: lỗi của gói risk giờ đã theo locale của request (hàm …In), nên với chuỗi đã dịch đây là no-op; giữ làm lưới an toàn cho
// thông điệp tiếng Việt còn sót (khóa catalog là chính câu tiếng Việt). Luôn khác nil.
func localizeAll(loc i18n.Locale, msgs []string) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = loc.T(m)
	}
	return out
}

func bad(msg httpx.Text) error { return httpx.Err(http.StatusBadRequest, msg) }

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

func noNul(s string) (string, error) {
	if strings.ContainsRune(s, 0) {
		return "", bad(httpx.T("Chuỗi chứa ký tự NUL (U+0000)"))
	}
	return s, nil
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// repoParam là tham số query repo bắt buộc.
func repoParam(r *http.Request) (string, error) {
	v := r.URL.Query().Get("repo")
	if isBlank(v) {
		return "", bad(httpx.T("Thiếu repo"))
	}
	return noNul(v)
}

type object map[string]json.RawMessage

// body đọc body thành object JSON (giữ nguyên văn từng giá trị).
func body(r *http.Request) (object, error) {
	b, err := httpx.ReadBody(r)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, bad(httpx.T("Body phải là object JSON"))
	}
	var raw json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&raw); err != nil {
		return nil, bad(httpx.T("JSON không hợp lệ"))
	}
	var o object
	if raw[0] != '{' || json.Unmarshal(raw, &o) != nil {
		return nil, bad(httpx.T("Body phải là object JSON"))
	}
	return o, nil
}

// rawString trả chuỗi nếu v là JSON string.
func rawString(v json.RawMessage) (string, bool) {
	var s string
	if len(v) == 0 || v[0] != '"' || json.Unmarshal(v, &s) != nil {
		return "", false
	}
	return s, true
}

func (o object) text(field string) (string, error) {
	s, ok := rawString(o[field])
	if !ok || isBlank(s) {
		return "", bad(httpx.Tf("Thiếu %s", field))
	}
	return noNul(s)
}

func (o object) obj(field string) (string, error) {
	v := o[field]
	if len(v) == 0 || v[0] != '{' {
		return "", bad(httpx.Tf("%s phải là object JSON", field))
	}
	return string(v), nil
}

func (s *Service) cliGet(w http.ResponseWriter, r *http.Request) error {
	repo, err := repoParam(r)
	if err != nil {
		return err
	}
	p, err := s.CliPayloadOf(r.Context(), repo)
	if err != nil {
		return err
	}
	if p == nil {
		return httpx.Err(http.StatusNotFound, httpx.T("Repo chưa có chính sách"))
	}
	env, err := s.Keys.SignJSON(p)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, env)
	return nil
}

func (s *Service) cliImport(w http.ResponseWriter, r *http.Request) error {
	b, err := body(r)
	if err != nil {
		return err
	}
	repo, err := b.text("repo")
	if err != nil {
		return err
	}
	yaml, err := b.text("yaml")
	if err != nil {
		return err
	}
	p, _ := httpx.PrincipalFrom(r.Context())
	created, v, err := s.ImportFirst(r.Context(), repo, yaml, p.Login, clientIP(r))
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, map[string]int{"version": v})
	return nil
}

func (s *Service) view(w http.ResponseWriter, r *http.Request) error {
	repo, err := repoParam(r)
	if err != nil {
		return err
	}
	v, err := s.ViewOf(r.Context(), repo)
	if err != nil {
		return err
	}
	if v == nil {
		return httpx.Err(http.StatusNotFound, httpx.T("Repo chưa có chính sách"))
	}
	httpx.WriteJSON(w, http.StatusOK, v)
	return nil
}

func (s *Service) versions(w http.ResponseWriter, r *http.Request) error {
	repo, err := repoParam(r)
	if err != nil {
		return err
	}
	v, err := s.Versions(r.Context(), repo)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, v)
	return nil
}

// jsonInt32 đọc số nguyên (không phần lẻ/mũ) vừa int32 như JsonNode.isIntegralNumber && canConvertToInt.
func jsonInt32(v json.RawMessage) (int, bool) {
	if len(v) == 0 || string(v) == "null" || strings.ContainsAny(string(v), `.eE"`) {
		return 0, false
	}
	var n int32
	if json.Unmarshal(v, &n) != nil {
		return 0, false
	}
	return int(n), true
}

func (s *Service) publish(w http.ResponseWriter, r *http.Request) error {
	b, err := body(r)
	if err != nil {
		return err
	}
	repo, err := b.text("repo")
	if err != nil {
		return err
	}
	reason := ""
	if rs, ok := rawString(b["reason"]); ok {
		if rs, err = noNul(rs); err != nil {
			return err
		}
		reason = strings.TrimFunc(rs, func(c rune) bool { return c <= ' ' })
	}
	if reason == "" {
		return bad(httpx.T("Cần nhập lý do xuất bản"))
	}
	base, ok := jsonInt32(b["baseVersion"])
	if !ok {
		return bad(httpx.T("Thiếu baseVersion"))
	}
	policy, err := b.obj("policy")
	if err != nil {
		return err
	}
	fromPreset := false
	switch fp := string(b["fromPreset"]); fp {
	case "true", `"true"`:
		fromPreset = true
	}
	p, _ := httpx.PrincipalFrom(r.Context())
	out, err := s.Publish(r.Context(), repo, base, policy, reason, fromPreset, p.Login, clientIP(r))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
	return nil
}

func (s *Service) validate(w http.ResponseWriter, r *http.Request) error {
	b, err := body(r)
	if err != nil {
		return err
	}
	var repo *string
	if rs, ok := rawString(b["repo"]); ok {
		if rs, err = noNul(rs); err != nil {
			return err
		}
		repo = &rs
	}
	policy, err := b.obj("policy")
	if err != nil {
		return err
	}
	out, err := s.Validate(r.Context(), repo, policy)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// migrateDraft: POST /api/policy/migrate-draft {repo} → {policy, changes}; quyền như validate.
func (s *Service) migrateDraft(w http.ResponseWriter, r *http.Request) error {
	b, err := body(r)
	if err != nil {
		return err
	}
	repo, err := b.text("repo")
	if err != nil {
		return err
	}
	pj, ch, err := s.MigrateDraft(r.Context(), repo)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"policy": json.RawMessage(pj), "changes": ch})
	return nil
}
