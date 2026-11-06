package auth_test

import (
	"net/http"
	"strings"
	"testing"

	"aisdlc/internal/web/webtest"
)

type principals struct {
	sess                                webtest.Session
	login, ingest, approve, ci, unknown string
}

func setupPrincipals(t *testing.T, env *webtest.Env, w *webtest.Client) principals {
	login := webtest.CliToken(t, env.Pool, "add", "rules.a", "A")
	s := w.Session(login)
	_, ingest := w.CreateToken(s, "ingest", "i")
	_, approve := w.CreateToken(s, "approve", "a")
	return principals{sess: s, login: login, ingest: ingest, approve: approve,
		ci: webtest.CliToken(t, env.Pool, "ci-token", "rules"), unknown: "khong-co"}
}

// send gửi method tới path bằng một loại principal:
// none | bad | session+csrf | session | session+wrongcsrf | login | ingest | approve | ci.
func send(w *webtest.Client, p principals, who, method, path string) webtest.Resp {
	h := map[string]string{}
	switch who {
	case "none":
	case "bad":
		h["Authorization"] = "Bearer " + p.unknown
	case "session+csrf", "session", "session+wrongcsrf":
		h["Cookie"] = "AISDLC_SESSION=" + p.sess.ID
		if who == "session+csrf" {
			h["X-CSRF-Token"] = p.sess.CSRF
		} else if who == "session+wrongcsrf" {
			h["X-CSRF-Token"] = strings.Repeat("x", len(p.sess.CSRF))
		}
	default:
		tok := map[string]string{"login": p.login, "ingest": p.ingest, "approve": p.approve, "ci": p.ci}[who]
		h["Authorization"] = "Bearer " + tok
	}
	body := ""
	if method != "GET" && method != "HEAD" {
		body = "{}"
		h["Content-Type"] = "application/json"
	}
	return w.Do(method, path, body, h)
}

// Ma trận trong javadoc AuthFilter: vùng × principal × method → 200/401/403.
func TestAuthRulesMatrix(t *testing.T) {
	env, w := newEnv(t)
	p := setupPrincipals(t, env, w)
	whos := []string{"none", "bad", "session+csrf", "session", "session+wrongcsrf", "login", "ingest", "approve", "ci"}
	// mã theo thứ tự whos
	table := []struct {
		path, method string
		want         [9]int
	}{
		{"/api/ingest/zz", "GET", [9]int{401, 401, 403, 403, 403, 200, 200, 403, 403}},
		{"/api/ingest/zz", "POST", [9]int{401, 401, 403, 403, 403, 200, 200, 403, 403}},
		{"/api/cli/zz", "GET", [9]int{401, 401, 403, 403, 403, 403, 200, 403, 403}},
		{"/api/cli/zz", "POST", [9]int{401, 401, 403, 403, 403, 403, 200, 403, 403}},
		{"/api/ci/zz", "GET", [9]int{401, 401, 403, 403, 403, 403, 403, 403, 200}},
		{"/api/ci/zz", "POST", [9]int{401, 401, 403, 403, 403, 403, 403, 403, 200}},
		{"/api/gates/zz", "POST", [9]int{401, 401, 200, 403, 403, 403, 403, 200, 403}},
		{"/api/gates/zz", "GET", [9]int{401, 401, 200, 200, 200, 200, 403, 403, 403}},
		{"/api/gates/zz", "PUT", [9]int{401, 401, 200, 403, 403, 403, 403, 403, 403}},
		{"/api/control/zz", "GET", [9]int{401, 401, 200, 200, 200, 403, 403, 403, 403}},
		{"/api/control/zz", "POST", [9]int{401, 401, 200, 403, 403, 403, 403, 403, 403}},
		{"/api/zz", "GET", [9]int{401, 401, 200, 200, 200, 200, 403, 403, 403}},
		{"/api/zz", "HEAD", [9]int{401, 401, 200, 200, 200, 200, 403, 403, 403}},
		{"/api/zz", "POST", [9]int{401, 401, 200, 403, 403, 403, 403, 403, 403}},
		{"/api/zz", "PUT", [9]int{401, 401, 200, 403, 403, 403, 403, 403, 403}},
		{"/api/zz", "DELETE", [9]int{401, 401, 200, 403, 403, 403, 403, 403, 403}},
		{"/api/zz", "PATCH", [9]int{401, 401, 200, 403, 403, 403, 403, 403, 403}},
	}
	for _, c := range table {
		for i, who := range whos {
			r := send(w, p, who, c.method, c.path)
			if r.Status != c.want[i] {
				t.Errorf("%s %s bằng %s: %d %s, muốn %d", c.method, c.path, who, r.Status, r.Body, c.want[i])
			}
			if r.Status == 200 && c.method != "HEAD" && !strings.Contains(r.Body, `"login":"rules.a"`) && !strings.Contains(r.Body, `"login":"ci:rules"`) {
				t.Errorf("%s %s bằng %s: principal không gắn vào ctx: %s", c.method, c.path, who, r.Body)
			}
		}
	}
}

// Đường thô qua server Go phải ra cùng vùng như Java: vùng theo r.URL.Path, theo đường chi định tuyến (RawPath)
// và theo Normalize(RequestURI) phải trùng, nếu không 403; dạng lạ không bao giờ bỏ qua xác thực.
func TestAuthAreaNormalization(t *testing.T) {
	env, w := newEnv(t)
	p := setupPrincipals(t, env, w)
	tok := map[string]string{"login": p.login, "ingest": p.ingest, "approve": p.approve, "ci": p.ci}
	cases := []struct {
		method, target, who string
		want                int
	}{
		// không xác thực: mọi dạng lạ đều bị lọc (401), chỉ /api/login đúng nguyên văn và đường tĩnh sạch được bỏ qua
		{"GET", "/api/zz", "", 401},
		{"GET", "/apix", "", 401},
		{"GET", "/api", "", 401},
		{"GET", "/api/cli;x/zz", "", 401},
		{"GET", "/api/%63li/zz", "", 401},
		{"POST", "/api/./gates/zz", "", 401},
		{"GET", "/api/x/../control/zz", "", 401},
		{"GET", "//api/zz", "", 401},
		{"GET", `/api\cli\zz`, "", 401},
		{"GET", "/%61pi/zz", "", 401},
		{"GET", "/x/../api/zz", "", 401},
		{"GET", "/static/./x", "", 401},
		{"GET", "/static/x;y", "", 401},
		{"GET", "/static//x", "", 401},
		{"GET", "/static%2Fx", "", 401},
		{"GET", `/static\x`, "", 401},
		{"POST", "/api/login;x", "", 401},
		{"POST", "/api/login/", "", 401},
		{"GET", "http://h/api/zz", "", 401},
		{"GET", "http://h/index.html", "", 401},
		{"GET", "/index.html", "", 404}, // tĩnh sạch: qua lọc
		{"GET", "/a/b.js?x=/api/zz", "", 404},
		// Bearer: vùng sau chuẩn hóa áp luật, lệch vùng → 403
		{"GET", "/api/cli;x/zz", "login", 403},
		{"GET", "/api/cli;x/zz", "ingest", 403},
		{"GET", "/api/%63li/zz", "login", 403},
		{"GET", "/api/%63li/zz", "ingest", 403}, // chi định tuyến RawPath (/api/%63li) khác vùng → từ chối
		{"GET", "/api/c%69/zz", "ci", 403},
		{"POST", "/api/./gates/zz", "login", 403},
		{"POST", "/api/./gates/zz", "approve", 403},
		{"GET", "/api/x/../control/zz", "login", 403},
		{"GET", "/api/zz/../cli/zz", "login", 403},
		{"GET", "/api/zz/../cli/zz", "ingest", 403},
		{"GET", "//api/zz", "ingest", 403},
		{"GET", "//api/cli/zz", "login", 403},
		{"GET", "//api/cli/zz", "ingest", 403},
		{"GET", `/api\cli\zz`, "login", 403},
		{"GET", `/api\cli\zz`, "ingest", 403},
		{"GET", "/api/x/..%2Fci/zz", "login", 403},
		{"GET", "/api/x/..%2Fci/zz", "ci", 403},
		{"POST", "/api/ingest/..%2F..%2Fzz", "ingest", 403},
		{"POST", "/api/ingest/%2E%2E/zz", "ingest", 403},
		{"POST", "/api/ingest/%2e%2e/control/zz", "login", 403},
		{"POST", "/api/ingest;x/../zz", "ingest", 403},
		{"GET", "/%61pi/zz", "ingest", 403},
		{"GET", "/%61pi/cli/zz", "login", 403},
		{"POST", "/api/login;x", "login", 403},
		{"GET", "/api/%zz", "login", 400}, // server Go từ chối %-mã sai trước khi tới handler
		// dạng sạch vẫn đúng luật
		{"GET", "/api/cli/zz", "ingest", 200},
		{"GET", "/api/cli/zz?x=/../zz", "ingest", 200},
		{"GET", "/api/zz?a=%2F..%2Fcli", "login", 200},
		{"GET", "http://h/api/ci/zz?q=1", "ci", 200},
		{"GET", "http://h/api/ci/zz", "login", 403},
	}
	for _, c := range cases {
		h := map[string]string{}
		if c.who != "" {
			h["Authorization"] = "Bearer " + tok[c.who]
		}
		body := ""
		if c.method == "POST" {
			body = "{}"
		}
		r := w.Raw(c.method, c.target, body, h)
		if r.Status != c.want {
			t.Errorf("%s %s bằng %q: %d %s, muốn %d", c.method, c.target, c.who, r.Status, r.Body, c.want)
		}
	}
	// /api/login?x=1 đúng đường: bỏ qua lọc, tới handler đăng nhập
	if r := w.Raw("POST", "/api/login?x=1", `{"token":"sai"}`, nil); r.Status != 401 || !strings.Contains(r.Body, "Token không hợp lệ") {
		t.Fatal(r.Status, r.Body)
	}
	if r := w.Raw("POST", "/api/login", `{"token":"`+p.login+`"}`, nil); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
}

func TestCsrfConstantTime(t *testing.T) {
	env, w := newEnv(t)
	p := setupPrincipals(t, env, w)
	other := w.Session(webtest.CliToken(t, env.Pool, "add", "rules.b", "B"))
	c := p.sess.CSRF
	flip := []byte(c)
	flip[len(flip)-1] ^= 1
	for _, bad := range []string{"", "x", c[:len(c)-1], c + "x", string(flip), strings.ToUpper(c), other.CSRF, p.sess.ID} {
		r := w.PostSession("/api/zz", "{}", p.sess, bad)
		if r.Status != 403 || r.Body != `{"error":"forbidden"}` {
			t.Errorf("CSRF %q: %d %s", bad, r.Status, r.Body)
		}
	}
	if r := w.PostSession("/api/zz", "{}", p.sess, c); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	// header lặp: chỉ giá trị đầu được xét (như getHeader của Servlet)
	dup := func(first, second string) int {
		h := http.Header{}
		h.Set("Cookie", "AISDLC_SESSION="+p.sess.ID)
		h.Set("Content-Type", "application/json")
		h.Add("X-CSRF-Token", first)
		h.Add("X-CSRF-Token", second)
		return w.DoHeader("POST", "/api/zz", "{}", h).Status
	}
	if got := dup("sai", c); got != 403 {
		t.Fatal("sai trước, đúng sau:", got)
	}
	if got := dup(c, "sai"); got != 200 {
		t.Fatal("đúng trước, sai sau:", got)
	}
}

func TestMethodNotAllowedJSON(t *testing.T) {
	env, w := newEnv(t)
	p := setupPrincipals(t, env, w)
	// /api/login sai method: bỏ qua lọc như Java, 405
	r := w.Do("GET", "/api/login", "", nil)
	if r.Status != 405 || r.Body != `{"error":"Method Not Allowed"}` || r.Header.Get("Allow") != "POST" {
		t.Fatal(r.Status, r.Body, r.Header)
	}
	r = send(w, p, "session+csrf", "DELETE", "/api/me")
	if r.Status != 405 || r.Body != `{"error":"Method Not Allowed"}` || !strings.Contains(r.Header.Get("Allow"), "GET") {
		t.Fatal(r.Status, r.Body, r.Header)
	}
	// không xác thực thì 401 trước (lọc chạy trước định tuyến)
	if r := w.Do("DELETE", "/api/me", "", nil); r.Status != 401 {
		t.Fatal(r.Status)
	}
	// /api lạ đã xác thực → 404 JSON
	if r := w.GetSession("/api/nope", p.sess); r.Status != 404 || r.Body != `{"error":"not found"}` {
		t.Fatal(r.Status, r.Body)
	}
}
