package auth_test

import (
	"io"
	"net/http"
	"runtime"
	"testing"
	"time"

	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/webtest"
)

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// /api/login bỏ qua xác thực nhưng body JSON bị giới hạn 1 MB: 64 MB phát trực tiếp → 413, bộ nhớ không tăng theo body.
func TestLoginLargeBody413(t *testing.T) {
	env, _ := newEnv(t)
	for _, chunked := range []bool{false, true} {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		const size = 64 << 20
		req, _ := http.NewRequest("POST", env.Server.URL+"/api/login", io.LimitReader(zeros{}, size))
		req.Header.Set("Content-Type", "application/json")
		if !chunked {
			req.ContentLength = size
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("chunked=%v: client phải nhận phản hồi: %v", chunked, err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		runtime.ReadMemStats(&after)
		if res.StatusCode != 413 || string(b) != `{"error":"Payload vượt 1 MB"}` {
			t.Fatal(chunked, res.StatusCode, string(b))
		}
		if grew := after.TotalAlloc - before.TotalAlloc; grew > 16<<20 {
			t.Fatalf("chunked=%v: cấp phát %d MB khi nhận body 64 MB", chunked, grew>>20)
		}
	}
}

// Request gửi thân nhỏ giọt: 401/403 trong giới hạn DrainTimeout, /api/login trong giới hạn BodyTimeout;
// không giữ goroutine mãi.
func TestUnauthenticatedTrickleBounded(t *testing.T) {
	oldD, oldB := httpx.DrainTimeout, httpx.BodyTimeout
	httpx.DrainTimeout, httpx.BodyTimeout = 300*time.Millisecond, 500*time.Millisecond
	defer func() { httpx.DrainTimeout, httpx.BodyTimeout = oldD, oldB }()
	env, w := newEnv(t)
	tok := webtest.CliToken(t, env.Pool, "add", "trickle.a", "A")
	for _, c := range []struct {
		path, cl, auth string
		want           int
	}{
		{"/api/zz", "100000", "", 401},
		{"/api/zz", "", "", 401},
		{"/api/zz", "100000", "Bearer " + tok, 403}, // login POST khác → 403
		{"/api/login", "", "", 400},                 // bỏ qua lọc; đọc body nhỏ giọt hết BodyTimeout → JSON hỏng
	} {
		h := map[string]string{}
		if c.auth != "" {
			h["Authorization"] = c.auth
		}
		status, elapsed := webtest.Trickle(t, w.Base, "POST", c.path, c.cl, h)
		if elapsed > 3*time.Second || status != c.want {
			t.Errorf("%s cl=%q: %d sau %v, muốn %d", c.path, c.cl, status, elapsed, c.want)
		}
	}
}
