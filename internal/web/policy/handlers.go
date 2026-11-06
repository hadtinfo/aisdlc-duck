package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"aisdlc/internal/risk"
	"aisdlc/internal/web/httpx"
)

// maxBytes là giới hạn body của /api/policy/** và /api/cli/policy/import.
const maxBytes = 256 * 1024

// Routes đăng ký GET /api/cli/policy, POST /api/cli/policy/import, GET /api/policy, GET /api/policy/versions,
// POST /api/policy/{publish,validate,classify}.
func Routes(r chi.Router, s *Service) {
	limit := httpx.LimitBodyWith(maxBytes, httpx.NewError(http.StatusRequestEntityTooLarge, "Payload quá lớn"))
	r.Get("/api/cli/policy", wrap(s.cliGet))
	r.With(limit).Post("/api/cli/policy/import", wrap(s.cliImport))
	r.Get("/api/policy", wrap(s.view))
	r.Get("/api/policy/versions", wrap(s.versions))
	r.With(limit).Post("/api/policy/publish", wrap(s.publish))
	r.With(limit).Post("/api/policy/validate", wrap(s.validate))
	r.With(limit).Post("/api/policy/classify", wrap(s.classify))
}

func wrap(f func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(w, r); err != nil {
			writeErr(w, err)
		}
	}
}

// writeErr: RiskPolicyException → 400 {"error": lỗi đầu, "errors": [...]}; Conflict → 409 {"message","error"}.
func writeErr(w http.ResponseWriter, err error) {
	var pe *risk.PolicyError
	if errors.As(err, &pe) {
		msg := "Chính sách không hợp lệ"
		if len(pe.Errors) > 0 {
			msg = pe.Errors[0]
		}
		errs := pe.Errors
		if errs == nil {
			errs = []string{}
		}
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": msg, "errors": errs})
		return
	}
	var c *Conflict
	if errors.As(err, &c) {
		httpx.WriteJSON(w, http.StatusConflict, map[string]string{"message": c.Error(), "error": c.Error()})
		return
	}
	httpx.WriteError(w, err)
}

func bad(msg string) error { return httpx.NewError(http.StatusBadRequest, msg) }

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

func noNul(s string) (string, error) {
	if strings.ContainsRune(s, 0) {
		return "", bad("Chuỗi chứa ký tự NUL (U+0000)")
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
		return "", bad("Thiếu repo")
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
		return nil, bad("Body phải là object JSON")
	}
	var raw json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&raw); err != nil {
		return nil, bad("JSON không hợp lệ")
	}
	var o object
	if raw[0] != '{' || json.Unmarshal(raw, &o) != nil {
		return nil, bad("Body phải là object JSON")
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
		return "", bad("Thiếu " + field)
	}
	return noNul(s)
}

func (o object) obj(field string) (string, error) {
	v := o[field]
	if len(v) == 0 || v[0] != '{' {
		return "", bad(field + " phải là object JSON")
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
		return httpx.NewError(http.StatusNotFound, "Repo chưa có chính sách")
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
		return httpx.NewError(http.StatusNotFound, "Repo chưa có chính sách")
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
		return bad("Cần nhập lý do xuất bản")
	}
	base, ok := jsonInt32(b["baseVersion"])
	if !ok {
		return bad("Thiếu baseVersion")
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

func (s *Service) classify(w http.ResponseWriter, r *http.Request) error {
	b, err := body(r)
	if err != nil {
		return err
	}
	policy, err := b.obj("policy")
	if err != nil {
		return err
	}
	inputs, err := b.obj("inputs")
	if err != nil {
		return err
	}
	out, err := Classify(policy, inputs)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}
