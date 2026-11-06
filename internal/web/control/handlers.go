package control

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"aisdlc/internal/web/httpx"
)

// maxBytes là giới hạn body của API điều khiển (ControlController.MAX_BYTES).
const maxBytes = 64 * 1024

func tooLarge() *httpx.ApiError {
	return httpx.NewError(http.StatusRequestEntityTooLarge, "Payload quá lớn")
}

// Routes đăng ký POST /api/control/{pause,instructions,actions} (phiên + CSRF, auth.Middleware đã kiểm) và
// GET /api/cli/control (Bearer ingest, phong bì ký cho CLI).
func Routes(r chi.Router, s *Service) {
	limit := httpx.LimitBodyWith(maxBytes, tooLarge())
	r.With(limit).Post("/api/control/pause", s.handle(s.pause))
	r.With(limit).Post("/api/control/instructions", s.handle(s.instruct))
	r.With(limit).Post("/api/control/actions", s.handle(s.action))
	r.Get("/api/cli/control", s.cli)
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Service) handle(fn func(r *http.Request, p httpx.Principal) (ViewJSON, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := httpx.PrincipalFrom(r.Context())
		v, err := fn(r, p)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, v)
	}
}

func body(r *http.Request) (map[string]any, error) {
	b, err := httpx.ReadBody(r)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, bad("Body phải là object JSON")
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, bad("JSON không hợp lệ")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, bad("Body phải là object JSON")
	}
	return m, nil
}

func noNul(s string) (string, error) {
	if strings.ContainsRune(s, 0) {
		return "", bad("Chuỗi chứa ký tự NUL (U+0000)")
	}
	return s, nil
}

// text là chuỗi bắt buộc: khác rỗng, không chỉ khoảng trắng, không NUL.
func text(b map[string]any, field string) (string, error) {
	s, ok := b[field].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", bad("Thiếu " + field)
	}
	return noNul(s)
}

// optText: thiếu/null → nil; có mà không phải chuỗi → 400.
func optText(b map[string]any, field string) (*string, error) {
	v, ok := b[field]
	if !ok || v == nil {
		return nil, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, bad(field + " phải là chuỗi")
	}
	if _, err := noNul(s); err != nil {
		return nil, err
	}
	return &s, nil
}

func target(b map[string]any) (repo, feature string, err error) {
	if repo, err = text(b, "repo"); err != nil {
		return
	}
	feature, err = text(b, "feature")
	return
}

func (s *Service) pause(r *http.Request, p httpx.Principal) (ViewJSON, error) {
	b, err := body(r)
	if err != nil {
		return ViewJSON{}, err
	}
	paused, ok := b["paused"].(bool)
	if !ok {
		return ViewJSON{}, bad("paused phải là true hoặc false")
	}
	repo, feature, err := target(b)
	if err != nil {
		return ViewJSON{}, err
	}
	return s.Pause(r.Context(), p, clientIP(r), repo, feature, paused)
}

func (s *Service) instruct(r *http.Request, p httpx.Principal) (ViewJSON, error) {
	b, err := body(r)
	if err != nil {
		return ViewJSON{}, err
	}
	t, ok := b["text"].(string)
	if !ok {
		return ViewJSON{}, bad(MsgBadInstruction)
	}
	repo, feature, err := target(b)
	if err != nil {
		return ViewJSON{}, err
	}
	if t, err = noNul(t); err != nil {
		return ViewJSON{}, err
	}
	return s.Instruct(r.Context(), p, clientIP(r), repo, feature, t)
}

func (s *Service) action(r *http.Request, p httpx.Principal) (ViewJSON, error) {
	b, err := body(r)
	if err != nil {
		return ViewJSON{}, err
	}
	if v, ok := b["path"]; ok && v != nil {
		if _, isStr := v.(string); !isStr {
			return ViewJSON{}, bad("path phải là chuỗi")
		}
	}
	repo, feature, err := target(b)
	if err != nil {
		return ViewJSON{}, err
	}
	kind, err := text(b, "kind")
	if err != nil {
		return ViewJSON{}, err
	}
	stepID, err := text(b, "step")
	if err != nil {
		return ViewJSON{}, err
	}
	path, err := optText(b, "path")
	if err != nil {
		return ViewJSON{}, err
	}
	endTree, err := optText(b, "endTree")
	if err != nil {
		return ViewJSON{}, err
	}
	return s.Action(r.Context(), p, clientIP(r), repo, feature, kind, stepID, path, endTree)
}

func (s *Service) cli(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repo, feature := q.Get("repo"), q.Get("feature")
	if strings.TrimSpace(repo) == "" {
		httpx.WriteError(w, bad("Thiếu repo"))
		return
	}
	if strings.TrimSpace(feature) == "" {
		httpx.WriteError(w, bad("Thiếu feature"))
		return
	}
	repo, err := noNul(repo)
	if err == nil {
		feature, err = noNul(feature)
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	c, err := s.ForCLI(r.Context(), repo, feature)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	payload, err := c.ToJSON()
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.Keys.Sign(payload))
}
