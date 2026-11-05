package stub

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/spec"
)

type req struct {
	method, path string
	headers      map[string]string
	body         string
}

func do(t *testing.T, base string, r req) (*http.Response, string) {
	t.Helper()
	m := r.method
	if m == "" {
		m = "GET"
	}
	hr, err := http.NewRequest(m, base+r.path, strings.NewReader(r.body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range r.headers {
		hr.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(hr)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func start(t *testing.T, st spec.StubSpec) *Server {
	t.Helper()
	s, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Register([]spec.StubSpec{st}, []string{st.ID}); err != nil {
		t.Fatal(err)
	}
	return s
}

func ok(extra map[string]any) map[string]any {
	m := map[string]any{"status": 200, "body": "ok"}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestStubMatching(t *testing.T) {
	type m = map[string]any
	cases := []struct {
		name    string
		request m
		hit     req
		miss    req
	}{
		{"method", m{"method": "POST", "urlPath": "/a"}, req{method: "POST", path: "/a"}, req{method: "GET", path: "/a"}},
		{"method ANY", m{"method": "ANY", "urlPath": "/a"}, req{method: "DELETE", path: "/a"}, req{path: "/b"}},
		{"url", m{"method": "GET", "url": "/a?x=1"}, req{path: "/a?x=1"}, req{path: "/a?x=2"}},
		{"urlPath", m{"method": "GET", "urlPath": "/a"}, req{path: "/a?z=9"}, req{path: "/a/b"}},
		{"urlPathPattern", m{"method": "GET", "urlPathPattern": "/a/[0-9]+"}, req{path: "/a/42"}, req{path: "/a/42/x"}},
		{"query equalTo", m{"method": "GET", "urlPath": "/a", "queryParameters": m{"x": m{"equalTo": "1"}}}, req{path: "/a?x=1"}, req{path: "/a?x=2"}},
		{"query matches", m{"method": "GET", "urlPath": "/a", "queryParameters": m{"x": m{"matches": "[0-9]+"}}}, req{path: "/a?x=12"}, req{path: "/a?x=1a"}},
		{"header equalTo", m{"method": "GET", "urlPath": "/a", "headers": m{"X-Tok": m{"equalTo": "v"}}}, req{path: "/a", headers: map[string]string{"x-tok": "v"}}, req{path: "/a", headers: map[string]string{"X-Tok": "w"}}},
		{"body equalToJson", m{"method": "POST", "urlPath": "/a", "bodyPatterns": []any{m{"equalToJson": `{"a":1,"b":[2]}`}}},
			req{method: "POST", path: "/a", body: `{ "b": [2], "a": 1 }`}, req{method: "POST", path: "/a", body: `{"a":1}`}},
		{"body contains", m{"method": "POST", "urlPath": "/a", "bodyPatterns": []any{m{"contains": "foo"}, m{"contains": "bar"}}},
			req{method: "POST", path: "/a", body: "foo bar"}, req{method: "POST", path: "/a", body: "foo"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := start(t, spec.StubSpec{ID: "s", Request: c.request, Response: ok(nil)})
			res, body := do(t, s.URL(), c.hit)
			if res.StatusCode != 200 || body != "ok" {
				t.Fatalf("hit: %d %s", res.StatusCode, body)
			}
			res, body = do(t, s.URL(), c.miss)
			if res.StatusCode != 404 || !strings.Contains(body, "không có stub khớp") {
				t.Fatalf("miss: %d %s", res.StatusCode, body)
			}
		})
	}
}

func TestStubResponse(t *testing.T) {
	r := map[string]any{"method": "GET", "urlPath": "/a"}
	s := start(t, spec.StubSpec{ID: "s", Request: r, Response: map[string]any{
		"status": float64(201), "headers": map[string]any{"X-A": "b"}, "jsonBody": map[string]any{"x": 1}}})
	res, body := do(t, s.URL(), req{path: "/a"})
	if res.StatusCode != 201 || res.Header.Get("X-A") != "b" || res.Header.Get("Content-Type") != "application/json" || body != `{"x":1}` {
		t.Fatalf("%d %v %s", res.StatusCode, res.Header, body)
	}

	// Content-Type tường minh được giữ (port explicitContentTypeIsPreserved).
	s2 := start(t, spec.StubSpec{ID: "s", Request: r, Response: map[string]any{
		"status": 200, "headers": map[string]any{"content-type": "application/vnd.x+json"}, "jsonBody": map[string]any{"x": 1}}})
	res, _ = do(t, s2.URL(), req{path: "/a"})
	if got := res.Header.Get("Content-Type"); got != "application/vnd.x+json" {
		t.Fatalf("content-type %q", got)
	}

	s3 := start(t, spec.StubSpec{ID: "s", Request: r, Response: map[string]any{"status": 200, "body": "x", "fixedDelayMilliseconds": 150}})
	t0 := time.Now()
	do(t, s3.URL(), req{path: "/a"})
	if d := time.Since(t0); d < 150*time.Millisecond {
		t.Fatalf("trễ %v", d)
	}
}

func TestLaterStubWins(t *testing.T) {
	s, _ := Start()
	defer s.Close()
	all := []spec.StubSpec{
		{ID: "a", Request: map[string]any{"method": "ANY", "urlPath": "/a"}, Response: map[string]any{"body": "A"}},
		{ID: "b", Request: map[string]any{"method": "ANY", "urlPath": "/a"}, Response: map[string]any{"body": "B"}},
	}
	if err := s.Register(all, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if _, body := do(t, s.URL(), req{path: "/a"}); body != "B" {
		t.Fatal(body)
	}
}

func TestRegisterResetsPrevious(t *testing.T) {
	s, _ := Start()
	defer s.Close()
	all := []spec.StubSpec{
		{ID: "a", Request: map[string]any{"method": "GET", "urlPath": "/a"}, Response: ok(nil)},
		{ID: "b", Request: map[string]any{"method": "GET", "urlPath": "/b"}, Response: ok(nil)},
	}
	_ = s.Register(all, []string{"a"})
	_ = s.Register(all, []string{"b"})
	if res, _ := do(t, s.URL(), req{path: "/a"}); res.StatusCode != 404 {
		t.Fatal("stub cũ còn")
	}
	if res, _ := do(t, s.URL(), req{path: "/b"}); res.StatusCode != 200 {
		t.Fatal("stub mới không khớp")
	}
}

func TestRegisterUnknownID(t *testing.T) {
	s, _ := Start()
	defer s.Close()
	err := s.Register(nil, []string{"zzz"})
	if err == nil || err.Error() != "stub không tồn tại: zzz" {
		t.Fatalf("%v", err)
	}
}
