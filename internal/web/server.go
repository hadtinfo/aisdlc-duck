// Package web là server web AISDLC (cổng, ingest, điều khiển agent, UI).
package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/board"
	"aisdlc/internal/web/control"
	"aisdlc/internal/web/gates"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/ingest"
	"aisdlc/internal/web/policy"
	"aisdlc/internal/web/signing"
	"aisdlc/internal/web/spa"
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
	Keys *signing.Keys // khóa ký; Run nạp trước khi listen
	// Changed được gọi (thêm vào Hub.Publish) sau mỗi lần snapshot/sự kiện làm đổi một tính năng (nil-safe).
	Changed func(repoID, feature string)
}

// services là các service dựng từ Deps, dùng chung giữa các route.
type services struct {
	auth    *auth.Service
	modes   *gates.ModeService
	gates   *gates.Service
	ingest  *ingest.Service
	reader  *board.Reader
	hub     *board.Hub
	policy  *policy.Service
	control *control.Service
}

// registerRoutes là điểm các task sau thêm route.
func registerRoutes(r chi.Router, d Deps, s services) {
	auth.Routes(r, s.auth)
	gates.Routes(r, &gates.Handlers{Modes: s.modes, Keys: d.Keys, Gates: s.gates})
	ingest.Routes(r, s.ingest)
	board.Routes(r, s.reader, s.hub)
	policy.Routes(r, s.policy)
	control.Routes(r, s.control)
}

// NewHandler dựng router chi: recover, xác thực (bọc mọi request, trước định tuyến), 404/405 JSON cho /api/*, rồi các route.
func NewHandler(d Deps) http.Handler {
	h, _ := build(d)
	return h
}

// build dựng handler và trả kèm hub SSE (Run đóng hub khi Shutdown).
func build(d Deps) (*chi.Mux, *board.Hub) {
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	a := &auth.Service{Pool: d.Pool, Now: d.Now}
	reader := &board.Reader{Pool: d.Pool, Now: d.Now} // Control (Task 8) gắn sau
	hub := board.NewHub(reader)
	modes := &gates.ModeService{Pool: d.Pool, Keys: d.Keys, Now: d.Now, Auth: a}
	gs := &gates.Service{Pool: d.Pool, Keys: d.Keys, Modes: modes, Auth: a, Hub: hub, Now: d.Now}
	gs.AfterSubmit = gs.AutoApprove // tự duyệt theo mức rủi ro, trong tx nộp
	reader.Gates = gs
	changed := func(repoID, feature string) {
		hub.Publish(repoID, feature)
		if d.Changed != nil {
			d.Changed(repoID, feature)
		}
	}
	ctl := &control.Service{Pool: d.Pool, Keys: d.Keys, Auth: a, Hub: publishFunc(changed), Now: d.Now}
	reader.Control = ctl
	s := services{auth: a, modes: modes, gates: gs, control: ctl,
		ingest: &ingest.Service{Pool: d.Pool, Now: d.Now, Changed: changed, RiskChanged: gs.OnRiskChanged}, // thay thế bản tự duyệt, trong tx ingest
		reader: reader, hub: hub,
		policy: &policy.Service{Pool: d.Pool, Keys: d.Keys, Auth: a, Now: d.Now}}
	ui := spa.Embedded()
	r := chi.NewRouter()
	r.Use(Recoverer)
	r.Use(httpx.DefaultLimit(httpx.DefaultJSONLimit)) // route cần lớn hơn (ingest) tự đặt httpx.LimitBody
	r.Use(s.auth.Middleware)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if isAPI(req) {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		ui.ServeHTTP(w, req) // SPA fallback + tệp tĩnh; không có thì 404
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
	return r, hub
}

// publishFunc đưa một hàm thành giao diện Publish của service.
type publishFunc func(repoID, feature string)

func (f publishFunc) Publish(repoID, feature string) { f(repoID, feature) }

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

// Recoverer biến panic thành 500 {"error":"internal"} và ghi log. Phản hồi đã bắt đầu (header đã gửi, vd. SSE) thì
// không ghi thêm 500 mà hủy kết nối (http.ErrAbortHandler), để client thấy phản hồi bị cắt chứ không nhận body lẫn lộn.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tw := &trackingWriter{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Printf("web: panic %s %s: %v\n%s", r.Method, r.URL.Path, v, debug.Stack())
				if tw.started {
					panic(http.ErrAbortHandler)
				}
				httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			}
		}()
		next.ServeHTTP(tw, r)
	})
}

// trackingWriter ghi nhận phản hồi đã bắt đầu; Unwrap/Flush để http.ResponseController và SSE vẫn tới writer gốc.
type trackingWriter struct {
	http.ResponseWriter
	started bool
}

func (t *trackingWriter) WriteHeader(code int) {
	if code >= 200 || code == http.StatusSwitchingProtocols {
		t.started = true
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *trackingWriter) Write(b []byte) (int, error) {
	t.started = true
	return t.ResponseWriter.Write(b)
}

func (t *trackingWriter) Flush() {
	t.started = true
	_ = http.NewResponseController(t.ResponseWriter).Flush()
}

func (t *trackingWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }

// Run mở DB, migrate, phục vụ HTTP cho đến khi ctx hủy rồi Shutdown (tối đa 10s).
func Run(ctx context.Context, cfg Config) error {
	// Nạp khóa ký trước mọi thứ khác: file hỏng/không đọc được thì từ chối khởi động, không ghi đè.
	keys, err := signing.Load(cfg.KeyFile, cfg.PrevKeyFile)
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, cfg.DBURL, cfg.DBUser, cfg.DBPassword)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}
	h, hub := build(Deps{Pool: pool, Keys: keys})
	go SchedulePurge(ctx, time.Now, time.After, (&ingest.Service{Pool: pool, Now: func() time.Time { return time.Now().UTC() }}).PurgeOld) // dừng khi ctx hủy
	srv := &http.Server{Addr: cfg.Listen, Handler: h, ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 120 * time.Second} // không đặt WriteTimeout: SSE
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	return serve(ctx, srv, ln, hub, ShutdownTimeout)
}

// ShutdownTimeout là thời gian tối đa chờ request đang chạy khi tắt.
const ShutdownTimeout = 10 * time.Second

// serve phục vụ trên ln cho đến khi ctx hủy, rồi Shutdown: hub đóng mọi stream SSE ngay (RegisterOnShutdown), request
// thường được chờ tối đa timeout; quá hạn thì Close cưỡng bức.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, hub *board.Hub, timeout time.Duration) error {
	if hub != nil {
		srv.RegisterOnShutdown(hub.Close)
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		if hub != nil {
			hub.Close()
		}
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
		<-errc
		return fmt.Errorf("web: tắt server quá hạn %v, đã đóng cưỡng bức: %w", timeout, err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
