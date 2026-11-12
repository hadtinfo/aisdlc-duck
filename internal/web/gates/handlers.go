package gates

import (
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"

	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/signing"
)

// Handlers là các route của gates.
type Handlers struct {
	Modes *ModeService
	Keys  *signing.Keys
	Gates *Service // nộp/đọc cổng của CLI và duyệt/trả lại/mở lại/xác nhận (nil: không đăng ký các route đó)
}

// Routes đăng ký GET /api/cli/pubkey, POST /api/cli/mode, GET /api/repos, POST /api/repos/mode, và (khi có Gates)
// POST /api/cli/submissions, GET /api/cli/gates, POST /api/gates/{approve,reject,reopen,confirm-import},
// POST /api/cli/import, GET /api/ci/approvals.
func Routes(r chi.Router, h *Handlers) {
	r.Get("/api/cli/pubkey", h.pubkey)
	r.Post("/api/cli/mode", h.mode)
	r.Get("/api/repos", h.repos)
	r.Post("/api/repos/mode", h.setMode)
	if h.Gates != nil {
		gateRoutes(r, h.Gates)
	}
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
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
		httpx.Fail(w, r, err)
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
		return httpx.Err(400, httpx.T("configured phải là local hoặc web"))
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
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rows)
}

func (h *Handlers) setMode(w http.ResponseWriter, r *http.Request) {
	if err := h.doSetMode(w, r); err != nil {
		httpx.Fail(w, r, err)
	}
}

func (h *Handlers) doSetMode(w http.ResponseWriter, r *http.Request) error {
	p, _ := httpx.PrincipalFrom(r.Context())
	if !p.IsLead() {
		return httpx.Err(403, httpx.T("Chỉ lead được đổi chế độ duyệt"))
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
		return httpx.Err(400, httpx.T("approvals phải là local hoặc web"))
	}
	m, err := h.Modes.Set(r.Context(), p, clientIP(r), repo, approvals)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": m.Repo, "approvals": m.Mode, "setBy": m.SetBy, "setAt": httpx.Instant(*m.At)})
	return nil
}
