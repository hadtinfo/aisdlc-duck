package httpx_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/web/httpx"
)

func TestLimitBodyDrainsAnd413(t *testing.T) {
	called := false
	srv := httptest.NewServer(httpx.LimitBody(8 << 20)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })))
	defer srv.Close()
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(make([]byte, 9<<20)))
	if err != nil {
		t.Fatalf("client phải nhận phản hồi, không bị reset: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 413 || !strings.Contains(string(b), `"error"`) || called {
		t.Fatalf("status=%d body=%s called=%v", resp.StatusCode, b, called)
	}
}

func TestLimitBodyChunkedOver(t *testing.T) {
	h := httpx.LimitBody(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		httpx.WriteError(w, httpx.DecodeJSON(r, &v))
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()
	pr, pw := io.Pipe()
	go func() { _, _ = pw.Write([]byte(`{"a":"` + strings.Repeat("x", 5000) + `"}`)); pw.Close() }()
	resp, err := http.Post(srv.URL, "application/json", pr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestApiErrorShape(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.WriteError(w, httpx.NewError(409, "Cổng đã đổi"))
	if w.Code != 409 || strings.TrimSpace(w.Body.String()) != `{"error":"Cổng đã đổi"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatal(ct)
	}
	w = httptest.NewRecorder()
	httpx.WriteError(w, io.ErrUnexpectedEOF)
	if w.Code != 500 || strings.TrimSpace(w.Body.String()) != `{"error":"internal"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestDecodeJSONBadRequest(t *testing.T) {
	var v map[string]any
	r := httptest.NewRequest("POST", "/", strings.NewReader("{oops"))
	err := httpx.DecodeJSON(r, &v)
	ae, ok := err.(*httpx.ApiError)
	if !ok || ae.Status != 400 || ae.Message != "JSON không hợp lệ" {
		t.Fatalf("%v", err)
	}
	if err := httpx.DecodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(`{"a":1}`)), &v); err != nil {
		t.Fatal(err)
	}
}

func TestInstantFormat(t *testing.T) {
	loc := time.FixedZone("x", 7*3600)
	if got := httpx.Instant(time.Date(2026, 11, 5, 17, 0, 0, 0, loc)); got != "2026-11-05T10:00:00Z" {
		t.Fatal(got)
	}
	if got := httpx.Instant(time.Date(2026, 11, 5, 10, 0, 0, 500_000_000, time.UTC)); got != "2026-11-05T10:00:00.5Z" {
		t.Fatal(got)
	}
}

func TestPrincipalContext(t *testing.T) {
	ctx := httpx.WithPrincipal(httptest.NewRequest("GET", "/", nil).Context(), httpx.Principal{Login: "a", Via: "session"})
	p, ok := httpx.PrincipalFrom(ctx)
	if !ok || !p.IsSession() || p.Login != "a" {
		t.Fatal(p)
	}
	if _, ok := httpx.PrincipalFrom(httptest.NewRequest("GET", "/", nil).Context()); ok {
		t.Fatal("không có principal")
	}
}
