// Package web là server web AISDLC (cổng, ingest, điều khiển agent, UI).
package web

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/store"
)

// Config là cấu hình server, đọc từ env.
type Config struct{ Listen, DBURL, DBUser, DBPassword, KeyFile, PrevKeyFile string }

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// ConfigFromEnv đọc AISDLC_LISTEN, AISDLC_DB_URL, AISDLC_DB_USER, AISDLC_DB_PASSWORD,
// AISDLC_SIGNING_KEY_FILE, AISDLC_SIGNING_PREVIOUS_KEY_FILE.
func ConfigFromEnv() Config {
	return Config{
		Listen:      getenv("AISDLC_LISTEN", ":8080"),
		DBURL:       getenv("AISDLC_DB_URL", "postgres://localhost:5432/aisdlc"),
		DBUser:      os.Getenv("AISDLC_DB_USER"),
		DBPassword:  os.Getenv("AISDLC_DB_PASSWORD"),
		KeyFile:     getenv("AISDLC_SIGNING_KEY_FILE", "/data/signing-key"),
		PrevKeyFile: os.Getenv("AISDLC_SIGNING_PREVIOUS_KEY_FILE"),
	}
}

// Deps là phụ thuộc của handler; các task sau thêm trường (khóa ký, hub SSE, service...).
type Deps struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

// services là các service dựng từ Deps, dùng chung giữa các route.
type services struct {
	auth *auth.Service
}

// registerRoutes là điểm các task sau thêm route.
func registerRoutes(r chi.Router, d Deps, s services) {
	auth.Routes(r, s.auth)
}

// NewHandler dựng router chi: recover, xác thực (bọc mọi request, trước định tuyến), 404/405 JSON cho /api/*, rồi các route.
func NewHandler(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	s := services{auth: &auth.Service{Pool: d.Pool, Now: d.Now}}
	r := chi.NewRouter()
	r.Use(Recoverer)
	r.Use(httpx.DefaultLimit(httpx.DefaultJSONLimit)) // route cần lớn hơn (ingest) tự đặt httpx.LimitBody
	r.Use(s.auth.Middleware)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if isAPI(req) {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		http.NotFound(w, req)
	})
	// Spring trả 405 kèm Allow; body JSON dạng ApiError với câu lỗi chuẩn của Spring.
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		if allow := allowed(r, req); allow != "" {
			w.Header().Set("Allow", allow)
		}
		if isAPI(req) {
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method Not Allowed"})
			return
		}
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	})
	registerRoutes(r, d, s)
	return r
}

func isAPI(req *http.Request) bool {
	return req.URL.Path == "/api" || strings.HasPrefix(req.URL.Path, "/api/")
}

// allowed liệt kê các method có route cho đường của req (header Allow).
func allowed(mux *chi.Mux, req *http.Request) string {
	path := req.URL.RawPath
	if path == "" {
		path = req.URL.Path
	}
	var out []string
	for _, m := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"} {
		if mux.Match(chi.NewRouteContext(), m, path) {
			out = append(out, m)
		}
	}
	return strings.Join(out, ",")
}

// Recoverer biến panic thành 500 {"error":"internal"} và ghi log.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Printf("web: panic %s %s: %v\n%s", r.Method, r.URL.Path, v, debug.Stack())
				httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Run mở DB, migrate, phục vụ HTTP cho đến khi ctx hủy rồi Shutdown (tối đa 10s).
func Run(ctx context.Context, cfg Config) error {
	pool, err := store.Open(ctx, cfg.DBURL, cfg.DBUser, cfg.DBPassword)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}
	srv := &http.Server{Addr: cfg.Listen, Handler: NewHandler(Deps{Pool: pool}), ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 120 * time.Second} // không đặt WriteTimeout: SSE
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return err
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
