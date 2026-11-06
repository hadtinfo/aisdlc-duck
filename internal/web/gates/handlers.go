package gates

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/signing"
)

// Handlers là các route của gates (task này: pubkey, mode, repos).
type Handlers struct {
	Modes *ModeService
	Keys  *signing.Keys
}

// Routes đăng ký GET /api/cli/pubkey, POST /api/cli/mode, GET /api/repos, POST /api/repos/mode.
func Routes(r chi.Router, h *Handlers) {
	r.Get("/api/cli/pubkey", h.pubkey)
	r.Post("/api/cli/mode", h.mode)
	r.Get("/api/repos", h.repos)
	r.Post("/api/repos/mode", h.setMode)
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// text là chuỗi bắt buộc: khác rỗng, không chỉ khoảng trắng, không NUL (GateRequests.text).
func text(b map[string]any, field string) (string, error) {
	s, ok := b[field].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", httpx.NewError(400, "Thiếu "+field)
	}
	if strings.ContainsRune(s, 0) {
		return "", httpx.NewError(400, field+" không được chứa ký tự NUL")
	}
	return s, nil
}

// body đọc body thành object JSON (GateRequests.body).
func body(r *http.Request) (map[string]any, error) {
	var raw json.RawMessage
	if err := httpx.DecodeJSON(r, &raw); err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, httpx.NewError(400, "Body phải là object JSON")
	}
	return m, nil
}

func (h *Handlers) pubkey(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, h.Keys.PubkeyResponse())
}

type modePayload struct {
	Repo     string `json:"repo"`
	Mode     string `json:"mode"`
	At       string `json:"at"`
	IssuedAt string `json:"issuedAt"`
}

func (h *Handlers) mode(w http.ResponseWriter, r *http.Request) {
	if err := h.doMode(w, r); err != nil {
		httpx.WriteError(w, err)
	}
}

func (h *Handlers) doMode(w http.ResponseWriter, r *http.Request) error {
	b, err := body(r)
	if err != nil {
		return err
	}
	repo, err := text(b, "repo")
	if err != nil {
		return err
	}
	configured, err := text(b, "configured")
	if err != nil {
		return err
	}
	if configured != ModeLocal && configured != ModeWeb {
		return httpx.NewError(400, "configured phải là local hoặc web")
	}
	p, _ := httpx.PrincipalFrom(r.Context())
	m, err := h.Modes.Report(r.Context(), p, clientIP(r), repo, configured)
	if err != nil {
		return err
	}
	now := h.Modes.now()
	at := now
	if m.At != nil {
		at = *m.At
	}
	env, err := h.Keys.SignJSON(modePayload{Repo: repo, Mode: m.Mode, At: httpx.Instant(at), IssuedAt: httpx.Instant(now)})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, env)
	return nil
}

func (h *Handlers) repos(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Modes.List(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rows)
}

func (h *Handlers) setMode(w http.ResponseWriter, r *http.Request) {
	if err := h.doSetMode(w, r); err != nil {
		httpx.WriteError(w, err)
	}
}

func (h *Handlers) doSetMode(w http.ResponseWriter, r *http.Request) error {
	p, _ := httpx.PrincipalFrom(r.Context())
	if !p.IsLead() {
		return httpx.NewError(403, "Chỉ lead được đổi chế độ duyệt")
	}
	b, err := body(r)
	if err != nil {
		return err
	}
	repo, err := text(b, "repo")
	if err != nil {
		return err
	}
	approvals, err := text(b, "approvals")
	if err != nil {
		return err
	}
	if approvals != ModeLocal && approvals != ModeWeb {
		return httpx.NewError(400, "approvals phải là local hoặc web")
	}
	m, err := h.Modes.Set(r.Context(), p, clientIP(r), repo, approvals)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": m.Repo, "approvals": m.Mode, "setBy": m.SetBy, "setAt": httpx.Instant(*m.At)})
	return nil
}
