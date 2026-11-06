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
	"aisdlc/internal/web/webtest"
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
	for _, c := range []struct {
		ns   int
		want string
	}{
		{500_000_000, "2026-11-05T10:00:00.500Z"},
		{123_000_000, "2026-11-05T10:00:00.123Z"},
		{123_456_000, "2026-11-05T10:00:00.123456Z"},
		{100_000, "2026-11-05T10:00:00.000100Z"},
		{123_456_789, "2026-11-05T10:00:00.123456789Z"},
		{1, "2026-11-05T10:00:00.000000001Z"},
	} {
		if got := httpx.Instant(time.Date(2026, 11, 5, 10, 0, 0, c.ns, time.UTC)); got != c.want {
			t.Errorf("%d: %s, muốn %s", c.ns, got, c.want)
		}
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

func postJSON(t *testing.T, url string, body io.Reader) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", body)
	if err != nil {
		t.Fatalf("client phải nhận phản hồi: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func decodeHandler(w http.ResponseWriter, r *http.Request) {
	var v map[string]any
	if err := httpx.DecodeJSON(r, &v); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]int{"n": len(v)})
}

func bigJSON(n int) string { return `{"a":"` + strings.Repeat("x", n) + `"}` }

func TestDecodeJSONDefaultCap(t *testing.T) {
	// không có middleware giới hạn: DecodeJSON tự chặn 1 MB
	srv := httptest.NewServer(http.HandlerFunc(decodeHandler))
	defer srv.Close()
	if code, body := postJSON(t, srv.URL, strings.NewReader(bigJSON(2<<20))); code != 413 || body != `{"error":"Payload vượt 1 MB"}` {
		t.Fatal(code, body)
	}
	if code, _ := postJSON(t, srv.URL, strings.NewReader(bigJSON(1<<19))); code != 200 {
		t.Fatal(code)
	}
}

func TestDefaultLimitAndRouteOverride(t *testing.T) {
	def := httpx.DefaultLimit(httpx.DefaultJSONLimit)
	small := httptest.NewServer(def(http.HandlerFunc(decodeHandler)))
	defer small.Close()
	// body chunked (không Content-Length) lớn: 413, đọc bỏ phần thừa nên client nhận được phản hồi
	pr, pw := io.Pipe()
	go func() { _, _ = pw.Write([]byte(bigJSON(3 << 20))); pw.Close() }()
	if code, body := postJSON(t, small.URL, pr); code != 413 || body != `{"error":"Payload vượt 1 MB"}` {
		t.Fatal(code, body)
	}
	// route đặt LimitBody 8 MB sau DefaultLimit: 3 MB qua được
	big := httptest.NewServer(def(httpx.LimitBody(8 << 20)(http.HandlerFunc(decodeHandler))))
	defer big.Close()
	if code, body := postJSON(t, big.URL, strings.NewReader(bigJSON(3<<20))); code != 200 {
		t.Fatal(code, body)
	}
	if code, body := postJSON(t, big.URL, strings.NewReader(bigJSON(9<<20))); code != 413 || body != `{"error":"Payload vượt 8 MB"}` {
		t.Fatal(code, body)
	}
}

// Client gửi nhỏ giọt: việc đọc bỏ thân request có hạn giờ, client nhận phản hồi trong giới hạn.
func TestLimitBodyDrainTimeBounded(t *testing.T) {
	old := httpx.DrainTimeout
	httpx.DrainTimeout = 300 * time.Millisecond
	defer func() { httpx.DrainTimeout = old }()
	srv := httptest.NewServer(httpx.LimitBody(10)(http.HandlerFunc(decodeHandler)))
	defer srv.Close()
	for _, cl := range []string{"1000", ""} { // theo Content-Length, và chunked vượt khi đọc
		status, elapsed := webtest.Trickle(t, srv.URL, "POST", "/", cl, nil)
		if status != 413 || elapsed > 3*time.Second {
			t.Fatalf("cl=%q status=%d sau %v", cl, status, elapsed)
		}
	}
}

// Đọc body xong thì bỏ hạn đọc: request có body vẫn giữ được kết nối lâu hơn BodyTimeout mà ctx không bị hủy.
func TestBodyDeadlineClearedAfterRead(t *testing.T) {
	old := httpx.BodyTimeout
	httpx.BodyTimeout = 200 * time.Millisecond
	defer func() { httpx.BodyTimeout = old }()
	h := httpx.DefaultLimit(1 << 20)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		select {
		case <-r.Context().Done():
			w.WriteHeader(499)
		case <-time.After(600 * time.Millisecond):
			w.WriteHeader(200)
		}
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()
	res, err := http.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("ctx bị hủy do hạn đọc còn sót:", res.StatusCode)
	}
}
