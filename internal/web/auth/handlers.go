package auth

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"aisdlc/internal/web/httpx"
)

// Routes đăng ký /api/login, /api/me, /api/logout, /api/me/tokens... (AuthController, TokenController).
func Routes(r chi.Router, s *Service) {
	r.Post("/api/login", s.login)
	r.Get("/api/me", s.me)
	r.Post("/api/logout", s.logout)
	r.Get("/api/me/tokens", s.listTokens)
	r.Post("/api/me/tokens", s.createToken)
	r.Post("/api/me/tokens/{id}/revoke", s.revoke)
}

type meView struct {
	Login       string  `json:"login"`
	DisplayName string  `json:"displayName"`
	Role        string  `json:"role"`
	CSRF        *string `json:"csrf"`
}

func meOf(u *User, csrf *string) meView {
	return meView{Login: u.Login, DisplayName: u.DisplayName, Role: u.Role, CSRF: csrf}
}

// sessionCookie như ResponseCookie.toString của Spring: Path=/, Secure khi request TLS, HttpOnly, SameSite=Strict.
// expire=false → cookie phiên (không Max-Age); true → xóa (Max-Age=0, Expires=epoch).
func sessionCookie(value string, expire, secure bool) string {
	c := Cookie + "=" + value + "; Path=/"
	if expire {
		c += "; Max-Age=0; Expires=" + time.Unix(0, 0).UTC().Format(http.TimeFormat)
	}
	if secure {
		c += "; Secure"
	}
	return c + "; HttpOnly; SameSite=Strict"
}

// decodeObject đọc body JSON object; sai hay null → 400 "JSON không hợp lệ" (như @RequestBody Map thiếu/hỏng).
func decodeObject(r *http.Request) (map[string]any, error) {
	var m map[string]any
	if err := httpx.DecodeJSON(r, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, httpx.NewError(http.StatusBadRequest, "JSON không hợp lệ")
	}
	return m, nil
}

// login: chỉ token loại login mở phiên. Cookie chứa id phiên (không phải token); body trả CSRF. Audit session.open.
func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	body, err := decodeObject(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	tok, _ := body["token"].(string)
	ctx := r.Context()
	p, err := s.authenticateToken(ctx, tok)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var u *User
	if p != nil && p.Via == "login" {
		if u, err = s.UserByID(ctx, p.UserID); err != nil {
			httpx.WriteError(w, err)
			return
		}
	}
	if u == nil {
		httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "Token không hợp lệ"})
		return
	}
	id, csrf, err := s.createSession(ctx, u.ID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := s.WriteAudit(ctx, s.Pool, AuditEntry{Login: p.Login, IP: remoteIP(r), Channel: "web", Action: "session.open"}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.Header().Set("Set-Cookie", sessionCookie(id, false, r.TLS != nil))
	httpx.WriteJSON(w, http.StatusOK, meOf(u, &csrf))
}

func (s *Service) me(w http.ResponseWriter, r *http.Request) {
	p, _ := httpx.PrincipalFrom(r.Context())
	u, err := s.UserByID(r.Context(), p.UserID)
	if err == nil && u == nil {
		err = fmt.Errorf("không thấy user %d", p.UserID)
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var csrf *string
	if p.IsSession() {
		c, ok, err := s.sessionCSRF(r.Context(), sessionIDFrom(r.Context()))
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		if ok {
			csrf = &c
		}
	}
	httpx.WriteJSON(w, http.StatusOK, meOf(u, csrf))
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := httpx.PrincipalFrom(ctx)
	if err := s.deleteSession(ctx, sessionIDFrom(ctx)); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := s.WriteAudit(ctx, s.Pool, AuditEntry{Login: p.Login, IP: remoteIP(r), Channel: "web", Action: "session.close"}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.Header().Set("Set-Cookie", sessionCookie("", true, r.TLS != nil))
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type tokenView struct {
	ID         int64   `json:"id"`
	Kind       string  `json:"kind"`
	Label      string  `json:"label"`
	CreatedAt  *string `json:"createdAt"`
	LastUsedAt *string `json:"lastUsedAt"`
	RevokedAt  *string `json:"revokedAt"`
}

func instantPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := httpx.Instant(*t)
	return &s
}

func viewOf(t TokenInfo) tokenView {
	return tokenView{ID: t.ID, Kind: t.Kind, Label: t.Label, CreatedAt: instantPtr(t.CreatedAt),
		LastUsedAt: instantPtr(t.LastUsedAt), RevokedAt: instantPtr(t.RevokedAt)}
}

func (s *Service) listTokens(w http.ResponseWriter, r *http.Request) {
	p, _ := httpx.PrincipalFrom(r.Context())
	list, err := s.ListTokens(r.Context(), p.UserID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]tokenView, 0, len(list))
	for _, t := range list {
		out = append(out, viewOf(t))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

const maxLabel = 100

// createToken: chỉ ingest|approve (login/ci do admin cấp). Token thô chỉ trả một lần. Audit token.create.
func (s *Service) createToken(w http.ResponseWriter, r *http.Request) {
	body, err := decodeObject(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	kind, _ := body["kind"].(string)
	if kind != "ingest" && kind != "approve" {
		httpx.WriteError(w, httpx.NewError(http.StatusBadRequest, "kind phải là ingest hoặc approve"))
		return
	}
	label, ok := body["label"].(string)
	if !ok || javaBlank(label) || utf16Len(javaStrip(label)) > maxLabel {
		httpx.WriteError(w, httpx.NewError(http.StatusBadRequest, fmt.Sprintf("label bắt buộc, tối đa %d ký tự", maxLabel)))
		return
	}
	label = javaStrip(label)
	ctx := r.Context()
	p, _ := httpx.PrincipalFrom(ctx)
	created, err := s.CreateToken(ctx, s.Pool, p.UserID, kind, label)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := s.WriteAudit(ctx, s.Pool, AuditEntry{Login: p.Login, IP: remoteIP(r), Channel: "web", Action: "token.create",
		Target: strconv.FormatInt(created.ID, 10), Detail: map[string]any{"tokenId": created.ID, "kind": kind, "label": label}}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}{created.ID, created.Token})
}

// revoke: chỉ token của chính mình và loại ingest|approve (thu hồi login/ci ở đây có thể tự khóa mình). Audit token.revoke.
func (s *Service) revoke(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.NewError(http.StatusBadRequest, "Bad Request"))
		return
	}
	ctx := r.Context()
	p, _ := httpx.PrincipalFrom(ctx)
	notFound := httpx.NewError(http.StatusNotFound, "Không có token này")
	own, err := s.ownToken(ctx, p.UserID, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if own == nil {
		httpx.WriteError(w, notFound)
		return
	}
	if own.Kind == "login" || own.Kind == "ci" {
		httpx.WriteError(w, httpx.NewError(http.StatusBadRequest, "Không thu hồi được loại token này ở đây"))
		return
	}
	revoked, err := s.revokeToken(ctx, p.UserID, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if revoked == nil {
		httpx.WriteError(w, notFound)
		return
	}
	if err := s.WriteAudit(ctx, s.Pool, AuditEntry{Login: p.Login, IP: remoteIP(r), Channel: "web", Action: "token.revoke",
		Target: strconv.FormatInt(id, 10), Detail: map[string]any{"tokenId": id, "kind": revoked.Kind}}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, viewOf(*revoked))
}
