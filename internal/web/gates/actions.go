package gates

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/httpx"
)

// Route của cổng (GateActionController + phần nộp/đọc của CliGateController). Xác thực đã kiểm ở auth.Middleware:
// /api/cli/** = Bearer ingest; POST /api/gates/** = phiên + CSRF (kênh web) hoặc Bearer approve (kênh terminal, hoặc
// claude-code nếu body xin). Mọi trạng thái trả về là phong bì ký.
func gateRoutes(r chi.Router, s *Service) {
	limit := httpx.LimitBodyWith(MaxBytes, tooLarge())
	r.With(limit).Post("/api/cli/submissions", s.handle(s.submit))
	r.Get("/api/cli/gates", s.handle(s.gates))
	r.With(limit).Post("/api/gates/approve", s.handle(s.approve))
	r.With(limit).Post("/api/gates/reject", s.handle(s.reject))
	r.With(limit).Post("/api/gates/reopen", s.handle(s.reopen))
	r.With(limit).Post("/api/gates/rewind", s.handle(s.rewind))
	r.With(limit).Post("/api/gates/confirm-import", s.handle(s.confirmImport))
	ImportRoutes(r, s)
	CIRoutes(r, s)
}

func (s *Service) handle(fn func(r *http.Request, p httpx.Principal) (snapshot.FeatureGateState, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := httpx.PrincipalFrom(r.Context())
		st, err := fn(r, p)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		env, err := s.sign(r.Context(), st)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, env)
	}
}

// channel: phiên → web; token approve chỉ được xin claude-code, mọi giá trị khác (kể cả web) → terminal.
func channel(p httpx.Principal, b map[string]any) string {
	if p.IsSession() {
		return "web"
	}
	if c, _ := b["channel"].(string); c == "claude-code" {
		return "claude-code"
	}
	return "terminal"
}

// target đọc repo, feature, gate bắt buộc của body (theo thứ tự kiểm của Java).
func target(b map[string]any) (repo, feature string, g string, err error) {
	if repo, err = text(b, "repo"); err != nil {
		return
	}
	if feature, err = text(b, "feature"); err != nil {
		return
	}
	g, err = text(b, "gate")
	return
}

func (s *Service) approve(r *http.Request, p httpx.Principal) (snapshot.FeatureGateState, error) {
	var none snapshot.FeatureGateState
	b, err := body(r)
	if err != nil {
		return none, err
	}
	repo, feature, gname, err := target(b)
	if err != nil {
		return none, err
	}
	g, err := parseGate(gname)
	if err != nil {
		return none, err
	}
	hash, err := text(b, "hash")
	if err != nil {
		return none, err
	}
	req := ApproveRequest{Repo: repo, Feature: feature, Gate: g, Hash: hash, Flags: parseFlags(b["flags"]),
		AssumptionsSeen: parseStrings(b["assumptionsSeen"])}
	return s.Approve(r.Context(), p, clientIP(r), channel(p, b), req)
}

func (s *Service) feedbackArgs(r *http.Request) (b map[string]any, repo, feature string, g string, comment *string, err error) {
	if b, err = body(r); err != nil {
		return
	}
	if repo, feature, g, err = target(b); err != nil {
		return
	}
	if _, err = parseGate(g); err != nil {
		return
	}
	comment, err = optText(b, "comment")
	return
}

func (s *Service) reject(r *http.Request, p httpx.Principal) (snapshot.FeatureGateState, error) {
	b, repo, feature, g, comment, err := s.feedbackArgs(r)
	if err != nil {
		return snapshot.FeatureGateState{}, err
	}
	gt, _ := parseGate(g)
	return s.Reject(r.Context(), p, clientIP(r), channel(p, b), repo, feature, gt, comment)
}

func (s *Service) reopen(r *http.Request, p httpx.Principal) (snapshot.FeatureGateState, error) {
	b, repo, feature, g, comment, err := s.feedbackArgs(r)
	if err != nil {
		return snapshot.FeatureGateState{}, err
	}
	gt, _ := parseGate(g)
	return s.Reopen(r.Context(), p, clientIP(r), channel(p, b), repo, feature, gt, comment)
}

// confirmImport: chỉ qua phiên trình duyệt (không qua token approve của terminal).
func (s *Service) confirmImport(r *http.Request, p httpx.Principal) (snapshot.FeatureGateState, error) {
	var none snapshot.FeatureGateState
	if !p.IsSession() {
		return none, httpx.Err(http.StatusForbidden, httpx.T("Chỉ xác nhận bản nhập trên trình duyệt"))
	}
	b, err := body(r)
	if err != nil {
		return none, err
	}
	gname, err := text(b, "gate")
	if err != nil {
		return none, err
	}
	g, err := parseGate(gname)
	if err != nil {
		return none, err
	}
	repo, err := text(b, "repo")
	if err != nil {
		return none, err
	}
	feature, err := text(b, "feature")
	if err != nil {
		return none, err
	}
	return s.ConfirmImport(r.Context(), p, clientIP(r), repo, feature, g)
}

func (s *Service) submit(r *http.Request, p httpx.Principal) (snapshot.FeatureGateState, error) {
	var none snapshot.FeatureGateState
	b, err := body(r)
	if err != nil {
		return none, err
	}
	repo, feature, gname, err := target(b)
	if err != nil {
		return none, err
	}
	g, err := parseGate(gname)
	if err != nil {
		return none, err
	}
	hash, err := text(b, "hash")
	if err != nil {
		return none, err
	}
	fv, fp := b["findings"]
	fs, err := findings(fv, fp)
	if err != nil {
		return none, err
	}
	arts, err := parseArtifacts(b["artifacts"])
	if err != nil {
		return none, err
	}
	rv, rp := b["risk"]
	level, err := riskLevel(rv, rp)
	if err != nil {
		return none, err
	}
	return s.Submit(r.Context(), p, clientIP(r), SubmitRequest{Repo: repo, Feature: feature, Gate: g, Hash: hash,
		findings: fs, artifacts: arts, RiskLevel: level})
}

func (s *Service) gates(r *http.Request, _ httpx.Principal) (snapshot.FeatureGateState, error) {
	q := r.URL.Query()
	repo, err := param(q.Get("repo"), "repo")
	if err != nil {
		return snapshot.FeatureGateState{}, err
	}
	feature, err := param(q.Get("feature"), "feature")
	if err != nil {
		return snapshot.FeatureGateState{}, err
	}
	return s.GateState(r.Context(), repo, feature)
}
