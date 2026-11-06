package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/web"
	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/webtest"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func do(t *testing.T, c *http.Client, method, url, body string, hdr map[string]string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(b)
}

// TestServerEndToEnd: web.Run trên cổng ngẫu nhiên với Postgres thật; tạo user bằng RunAdmin, đăng nhập,
// /api/me 200, SPA và 404 JSON qua server thật; hủy ctx thì Run trả nil (Shutdown).
func TestServerEndToEnd(t *testing.T) {
	env := webtest.New(t, func(d web.Deps) http.Handler { return web.NewHandler(d) })
	var out bytes.Buffer
	if code := auth.RunAdmin(context.Background(), env.Pool, []string{"add", "e2e.a", "E2E A", "--role", "lead"}, &out); code != 0 {
		t.Fatal(code, out.String())
	}
	token := strings.TrimSpace(strings.TrimPrefix(out.String(), "Token: "))

	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- web.Run(ctx, web.Config{Listen: addr, DBURL: env.URL, KeyFile: filepath.Join(t.TempDir(), "signing-key")})
	}()
	base := "http://" + addr
	hc := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if res, err := hc.Get(base + "/"); err == nil {
			res.Body.Close()
			break
		}
		select {
		case err := <-errc:
			t.Fatal("Run kết thúc sớm:", err)
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("server không lên")
		}
	}

	if st, h, body := do(t, hc, "GET", base+"/login", "", nil); st != 200 || !strings.HasPrefix(h.Get("Content-Type"), "text/html") || !strings.Contains(body, `id="root"`) {
		t.Fatal(st, h, body)
	}
	if st, _, body := do(t, hc, "GET", base+"/api/khong-co", "", nil); st != 401 && st != 404 {
		t.Fatal(st, body)
	}
	if st, _, _ := do(t, hc, "GET", base+"/api/me", "", nil); st != 401 {
		t.Fatal(st)
	}
	st, h, body := do(t, hc, "POST", base+"/api/login", `{"token":"`+token+`"}`, map[string]string{"Content-Type": "application/json"})
	if st != 200 {
		t.Fatal(st, body)
	}
	var cookie string
	for _, c := range h.Values("Set-Cookie") {
		if strings.HasPrefix(c, "AISDLC_SESSION=") {
			cookie = strings.SplitN(c, ";", 2)[0]
		}
	}
	if cookie == "" {
		t.Fatal(h)
	}
	st, _, body = do(t, hc, "GET", base+"/api/me", "", map[string]string{"Cookie": cookie})
	var me map[string]any
	if err := json.Unmarshal([]byte(body), &me); err != nil || st != 200 || me["login"] != "e2e.a" {
		t.Fatal(st, body)
	}
	// đăng nhập rồi, đường /api lạ là 404 JSON, không phải SPA
	st, h, body = do(t, hc, "GET", base+"/api/khong-co", "", map[string]string{"Cookie": cookie})
	if st != 404 || !strings.HasPrefix(h.Get("Content-Type"), "application/json") || body != `{"error":"not found"}`+"\n" && body != `{"error":"not found"}` {
		t.Fatal(st, h, body)
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run không dừng sau khi hủy ctx")
	}
	if res, err := hc.Get(base + "/"); err == nil {
		res.Body.Close()
		t.Fatal("cổng vẫn mở sau Shutdown")
	}
}
