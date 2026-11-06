package spa_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"aisdlc/internal/web/spa"
)

func fsys() fstest.MapFS {
	return fstest.MapFS{
		"index.html":       {Data: []byte(`<html><div id="root"></div></html>`)},
		"assets/app-1.js":  {Data: []byte("console.log(1)")},
		"assets/app-1.css": {Data: []byte("a{}")},
		"favicon.svg":      {Data: []byte("<svg/>")},
	}
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// Port SpaIT.spaPathsServeIndexHtml.
func TestSpaPathsServeIndexHTML(t *testing.T) {
	h := spa.Handler(fsys())
	for _, p := range []string{"/", "/login", "/me", "/repos", "/f/gitlab.corp/pay/svc/PAY-1", "/f/gitlab.corp/pay/svc/PAY-1/live",
		"/risk/gitlab.corp/pay/svc", "/risk/local/web-service"} {
		rec := get(h, "GET", p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `<div id="root">`) {
			t.Fatalf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s: content-type %q", p, ct)
		}
	}
}

// "/api" và /api/** không bao giờ là SPA; tệp thiếu là 404.
func TestSpaNeverShadowsAPIAndMissingIs404(t *testing.T) {
	h := spa.Handler(fsys())
	for _, p := range []string{"/api", "/api/board", "/assets/missing.js", "/a/b", "/board/x", "/x.txt", "/assets/"} {
		if rec := get(h, "GET", p); rec.Code != 404 {
			t.Fatalf("%s: %d", p, rec.Code)
		}
	}
	if rec := get(h, "POST", "/login"); rec.Code != 404 && rec.Code != 405 {
		t.Fatal(rec.Code)
	}
}

func TestAssetContentType(t *testing.T) {
	h := spa.Handler(fsys())
	for p, want := range map[string]string{"/assets/app-1.js": "javascript", "/assets/app-1.css": "text/css", "/favicon.svg": "image/svg+xml"} {
		rec := get(h, "GET", p)
		b, _ := io.ReadAll(rec.Body)
		if rec.Code != 200 || len(b) == 0 || !strings.Contains(rec.Header().Get("Content-Type"), want) {
			t.Fatalf("%s: %d %q %q", p, rec.Code, rec.Header().Get("Content-Type"), b)
		}
	}
	// HEAD hợp lệ
	if rec := get(h, "HEAD", "/login"); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
}

// Checkout sạch (dist chỉ có .gitkeep): Embedded phục vụ trang placeholder; có bản build thật thì phục vụ index.html đó.
func TestEmbeddedPlaceholderOrBuild(t *testing.T) {
	rec := get(spa.Embedded(), "GET", "/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if _, err := os.Stat("dist/index.html"); err != nil && !strings.Contains(rec.Body.String(), "UI chưa build: chạy make ui") {
		t.Fatal("thiếu placeholder:", rec.Body.String())
	}
}
