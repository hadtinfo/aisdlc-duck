package webtest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/web/auth"
)

// Resp là phản hồi đã đọc hết body.
type Resp struct {
	Status int
	Header http.Header
	Body   string
}

// JSON giải body thành map (fail test nếu không phải object JSON).
func (r Resp) JSON(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.Body), &m); err != nil {
		t.Fatalf("body không phải JSON object: %v: %s", err, r.Body)
	}
	return m
}

// Session là phiên trình duyệt: id phiên (giá trị cookie AISDLC_SESSION) và CSRF.
type Session struct{ ID, CSRF string }

// Client là trợ giúp HTTP cho test xác thực (như WebHttp.java): Bearer, phiên (cookie + CSRF), tạo token.
type Client struct {
	t    *testing.T
	Base string
	HTTP *http.Client
}

// Client tạo client tới server của env (không theo redirect, không giữ cookie).
func (e *Env) Client(t *testing.T) *Client {
	return &Client{t: t, Base: e.Server.URL, HTTP: &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Do gửi request; path được gửi nguyên dạng mã hóa (RawPath giữ nguyên).
func (c *Client) Do(method, path, body string, hdr map[string]string) Resp {
	c.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, c.Base+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return c.send(req)
}

func (c *Client) send(req *http.Request) Resp {
	c.t.Helper()
	res, err := c.HTTP.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return Resp{Status: res.StatusCode, Header: res.Header, Body: string(b)}
}

// DoHeader gửi request với header đầy đủ (cho header lặp).
func (c *Client) DoHeader(method, path, body string, hdr http.Header) Resp {
	c.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, c.Base+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header = hdr
	return c.send(req)
}

// Raw gửi request-target thô (không qua url.Parse của client), ví dụ `/api\cli\x` hay `//api/x`.
func (c *Client) Raw(method, target, body string, hdr map[string]string) Resp {
	c.t.Helper()
	host := strings.TrimPrefix(c.Base, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		c.t.Fatal(err)
	}
	defer conn.Close()
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\nContent-Length: %d\r\n", method, target, host, len(body))
	for k, v := range hdr {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	b.WriteString("\r\n")
	b.WriteString(body)
	if _, err := conn.Write(b.Bytes()); err != nil {
		c.t.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	rb, _ := io.ReadAll(res.Body)
	return Resp{Status: res.StatusCode, Header: res.Header, Body: string(rb)}
}

// GetBearer: GET với Authorization: Bearer.
func (c *Client) GetBearer(path, token string) Resp {
	return c.Do("GET", path, "", map[string]string{"Authorization": "Bearer " + token})
}

// PostBearer: POST JSON với Authorization: Bearer.
func (c *Client) PostBearer(path, body, token string) Resp {
	return c.Do("POST", path, body, map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"})
}

// GetSession: GET với cookie phiên.
func (c *Client) GetSession(path string, s Session) Resp {
	return c.Do("GET", path, "", map[string]string{"Cookie": "AISDLC_SESSION=" + s.ID})
}

// PostSession: POST JSON với cookie phiên; csrf rỗng thì không gửi X-CSRF-Token.
func (c *Client) PostSession(path, body string, s Session, csrf string) Resp {
	h := map[string]string{"Cookie": "AISDLC_SESSION=" + s.ID, "Content-Type": "application/json"}
	if csrf != "" {
		h["X-CSRF-Token"] = csrf
	}
	return c.Do("POST", path, body, h)
}

// Login: POST /api/login {"token": token}.
func (c *Client) Login(token string) Resp {
	b, _ := json.Marshal(map[string]string{"token": token})
	return c.Do("POST", "/api/login", string(b), map[string]string{"Content-Type": "application/json"})
}

var sessionCookie = regexp.MustCompile(`AISDLC_SESSION=([^;]*)`)

// Session đăng nhập bằng token login, trả id phiên (từ Set-Cookie) và CSRF (từ body).
func (c *Client) Session(loginToken string) Session {
	c.t.Helper()
	r := c.Login(loginToken)
	if r.Status != 200 {
		c.t.Fatalf("login %d %s", r.Status, r.Body)
	}
	m := sessionCookie.FindStringSubmatch(r.Header.Get("Set-Cookie"))
	if m == nil {
		c.t.Fatalf("không có cookie phiên: %q", r.Header.Get("Set-Cookie"))
	}
	csrf, _ := r.JSON(c.t)["csrf"].(string)
	return Session{ID: m[1], CSRF: csrf}
}

// CreateToken tạo token ingest/approve qua /api/me/tokens; trả id và token thô.
func (c *Client) CreateToken(s Session, kind, label string) (int64, string) {
	c.t.Helper()
	b, _ := json.Marshal(map[string]string{"kind": kind, "label": label})
	r := c.PostSession("/api/me/tokens", string(b), s, s.CSRF)
	if r.Status != 200 {
		c.t.Fatalf("create token %d %s", r.Status, r.Body)
	}
	m := r.JSON(c.t)
	return int64(m["id"].(float64)), m["token"].(string)
}

var tokenLine = regexp.MustCompile(`Token: (\S+)`)

// CliToken chạy lệnh quản trị user (như `aisdlc-web user ...`) và trả token in ra.
func CliToken(t *testing.T, pool *pgxpool.Pool, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if code := auth.RunAdmin(context.Background(), pool, args, &out); code != 0 {
		t.Fatalf("cli %v exit %d: %s", args, code, out.String())
	}
	m := tokenLine.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("không thấy Token: %s", out.String())
	}
	return m[1]
}
