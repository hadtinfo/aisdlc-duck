package auth

import (
	"context"
	"crypto/subtle"
	"log"
	"net"
	"net/http"
	"strings"

	"aisdlc/internal/web/httpx"
)

// Tên cookie phiên và header CSRF.
const (
	Cookie     = "AISDLC_SESSION"
	CSRFHeader = "X-CSRF-Token"
)

type sessionKey struct{}

// sessionIDFrom trả id phiên thô của request (chỉ trong bộ nhớ, không log).
func sessionIDFrom(ctx context.Context) string {
	s, _ := ctx.Value(sessionKey{}).(string)
	return s
}

// Middleware là AuthFilter. Mọi /api/** trừ /api/login cần xác thực: "Authorization: Bearer <token>" hoặc cookie
// phiên AISDLC_SESSION. Rồi kiểm luật theo vùng và loại token:
//   - /api/ingest/** → Bearer ingest|login
//   - /api/cli/** → Bearer ingest
//   - /api/ci/** → Bearer ci
//   - POST /api/gates/** → phiên + CSRF, hoặc Bearer approve
//   - /api/control/** → chỉ phiên + CSRF (Bearer mọi loại → 403)
//   - POST/PUT/DELETE khác → phiên + CSRF; Bearer → 403
//   - GET khác → phiên hoặc Bearer login
//
// Không xác thực được → 401 {"error":"unauthorized"}; sai loại/thiếu CSRF → 403 {"error":"forbidden"}.
// Thành công thì gắn httpx.Principal vào ctx. Phải bọc router gốc (chạy trước định tuyến) để mọi dạng đường lạ đều qua đây.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := rawPath(r)
		if shouldNotFilter(r, raw) {
			next.ServeHTTP(w, r)
			return
		}
		ctx := r.Context()
		var p *httpx.Principal
		var sid string
		var err error
		if tok, ok := bearer(r); ok {
			p, err = s.authenticateToken(ctx, tok)
		} else {
			if c, cerr := r.Cookie(Cookie); cerr == nil {
				sid = c.Value
			}
			p, err = s.findSession(ctx, sid)
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if p == nil {
			reject(w, r, http.StatusUnauthorized, `{"error":"unauthorized"}`)
			return
		}
		ok, err := s.allowed(r, raw, *p, sid)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if !ok {
			reject(w, r, http.StatusForbidden, `{"error":"forbidden"}`)
			return
		}
		ctx = httpx.WithPrincipal(ctx, *p)
		if sid != "" {
			ctx = context.WithValue(ctx, sessionKey{}, sid)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) fail(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("auth: %s %s: %v", r.Method, r.URL.Path, err)
	httpx.Drain(w, r)
	httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
}

// reject đọc bỏ thân request (như Tomcat max-swallow-size -1) để client nhận được 401/403 chứ không bị reset.
func reject(w http.ResponseWriter, r *http.Request, status int, body string) {
	httpx.Drain(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// rawPath là đường thô như getRequestURI của Servlet: RequestURI bỏ query; dạng tuyệt đối (http://host/p) bỏ scheme+host.
func rawPath(r *http.Request) string {
	raw := r.RequestURI
	if raw == "" {
		raw = r.URL.RequestURI()
	}
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	if !strings.HasPrefix(raw, "/") {
		if i := strings.Index(raw, "://"); i >= 0 {
			rest := raw[i+3:]
			if j := strings.IndexByte(rest, '/'); j >= 0 {
				return rest[j:]
			}
			return "/"
		}
	}
	return raw
}

// shouldNotFilter: fail closed. Chỉ /api/login đúng nguyên văn và đường tĩnh "sạch" (bắt đầu "/", không ;params,
// %-mã, '\', "//", "/.") mà đường router thấy không nằm dưới /api được bỏ qua. Request dạng tuyệt đối luôn bị lọc.
func shouldNotFilter(r *http.Request, p string) bool {
	if r.RequestURI != "" && !strings.HasPrefix(r.RequestURI, "/") {
		return false // dạng tuyệt đối hoặc "*"
	}
	if p == "/api/login" {
		return true
	}
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "/api") || strings.HasPrefix(p, "//") {
		return false
	}
	if strings.ContainsAny(p, `;%\`) || strings.Contains(p, "//") || strings.Contains(p, "/.") {
		return false
	}
	// Đường router (chi định tuyến theo RawPath nếu có, không thì Path) nằm dưới /api thì phải xác thực.
	return !strings.HasPrefix(r.URL.Path, "/api") && !strings.HasPrefix(r.URL.RawPath, "/api")
}

// allowed kiểm luật vùng. Vùng theo r.URL.Path (đã giải mã), theo r.URL.RawPath (đường chi định tuyến khi có) và theo
// Normalize(đường thô) phải trùng nhau, để dạng mã hóa/;params/.. không lách sang vùng có luật khác.
func (s *Service) allowed(r *http.Request, raw string, p httpx.Principal, sid string) (bool, error) {
	area := AreaOf(r.URL.Path)
	if r.URL.RawPath != "" && AreaOf(r.URL.RawPath) != area {
		return false, nil
	}
	if AreaOf(Normalize(raw)) != area {
		return false, nil
	}
	safe := r.Method == http.MethodGet || r.Method == http.MethodHead
	if area == AreaGates && r.Method != http.MethodPost {
		area = AreaOther // chỉ POST /api/gates/** có luật riêng
	}
	if area == AreaLogin {
		area = AreaOther // /api/login dạng lạ lọt vào đây: coi như đường khác
	}
	if p.IsSession() {
		if area == AreaIngest || area == AreaCLI || area == AreaCI {
			return false, nil
		}
		if safe {
			return true, nil
		}
		return s.csrfMatches(r.Context(), sid, r.Header.Get(CSRFHeader))
	}
	switch area {
	case AreaIngest:
		return p.Via == "ingest" || p.Via == "login", nil
	case AreaCLI:
		return p.Via == "ingest", nil
	case AreaCI:
		return p.Via == "ci", nil
	case AreaGates:
		return p.Via == "approve", nil
	case AreaControl:
		return false, nil // chỉ phiên + CSRF
	default:
		return safe && p.Via == "login", nil
	}
}

// csrfMatches so CSRF của phiên với header bằng so sánh thời gian hằng; header thiếu → false.
func (s *Service) csrfMatches(ctx context.Context, sid, header string) (bool, error) {
	if header == "" {
		return false, nil
	}
	c, ok, err := s.sessionCSRF(ctx, sid)
	if err != nil || !ok {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(c), []byte(header)) == 1, nil
}

// bearer: "Authorization: Bearer <token>" (tiền tố không phân biệt hoa thường) → token đã trim.
func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if len(h) >= 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:]), true
	}
	return "", false
}

// remoteIP là địa chỉ IP người gọi (getRemoteAddr), không kèm cổng.
func remoteIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
