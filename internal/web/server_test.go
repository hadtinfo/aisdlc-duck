package web_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aisdlc/internal/web"
)

func TestConfigFromEnvDefaults(t *testing.T) {
	for _, k := range []string{"AISDLC_DB_URL", "AISDLC_DB_USER", "AISDLC_DB_PASSWORD", "AISDLC_SIGNING_KEY_FILE", "AISDLC_SIGNING_PREVIOUS_KEY_FILE", "AISDLC_LISTEN"} {
		t.Setenv(k, "")
	}
	c := web.ConfigFromEnv()
	want := web.Config{Listen: ":8080", DBURL: "postgres://localhost:5432/aisdlc", KeyFile: "/data/signing-key"}
	if c != want {
		t.Fatalf("%+v", c)
	}
	t.Setenv("AISDLC_LISTEN", ":9")
	t.Setenv("AISDLC_DB_USER", "u")
	t.Setenv("AISDLC_DB_PASSWORD", "p")
	t.Setenv("AISDLC_SIGNING_PREVIOUS_KEY_FILE", "/k2")
	c = web.ConfigFromEnv()
	if c.Listen != ":9" || c.DBUser != "u" || c.DBPassword != "p" || c.PrevKeyFile != "/k2" {
		t.Fatalf("%+v", c)
	}
}

// /api lạ không xác thực → 401 (lọc chạy trước định tuyến, như AuthFilter); đã xác thực → 404 JSON
// (TestMethodNotAllowedJSON ở internal/web/auth).
func TestUnknownApiUnauthenticatedIs401JSON(t *testing.T) {
	h := web.NewHandler(web.Deps{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/nope", nil))
	if w.Code != 401 || strings.TrimSpace(w.Body.String()) != `{"error":"unauthorized"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestStaticPathNotFoundPlain(t *testing.T) {
	h := web.NewHandler(web.Deps{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/nope.js", nil))
	if w.Code != 404 || strings.Contains(w.Body.String(), "unauthorized") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestRecoverPanic500(t *testing.T) {
	srv := httptest.NewServer(web.Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") })))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 500 || strings.TrimSpace(string(b)) != `{"error":"internal"}` {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
}

// FlushError của trackingWriter để http.ResponseController.Flush trong handler thấy lỗi của writer gốc (SSE cần).
func TestRecovererFlushErrorPropagates(t *testing.T) {
	h := web.Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).Flush(); !errors.Is(err, errBoom) {
			t.Errorf("Flush = %v, muốn errBoom", err)
		}
	}))
	h.ServeHTTP(&failFlush{ResponseWriter: httptest.NewRecorder()}, httptest.NewRequest("GET", "/", nil))
}

var errBoom = errors.New("boom")

type failFlush struct{ http.ResponseWriter }

func (failFlush) FlushError() error { return errBoom }
