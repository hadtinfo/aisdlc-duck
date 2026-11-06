package auth_test

import (
	"strings"
	"testing"
	"time"

	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/webtest"
)

// Port AuthIT.java. (legacyTokenFromBeforeV2StillLogsIn không áp dụng: một migration gộp, không có schema V1.)

type authUsers struct{ loginA, loginB, ci string }

func addAuthUsers(t *testing.T, env *webtest.Env) authUsers {
	return authUsers{
		loginA: webtest.CliToken(t, env.Pool, "add", "auth.a", "An A", "--email", "a@corp"),
		loginB: webtest.CliToken(t, env.Pool, "add", "auth.b", "Bình B"),
		ci:     webtest.CliToken(t, env.Pool, "ci-token", "build"),
	}
}

func TestLoginTokenOpensSessionCookieIsNotToken(t *testing.T) {
	env, w := newEnv(t)
	u := addAuthUsers(t, env)
	r := w.Login(u.loginA)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	cookie := r.Header.Get("Set-Cookie")
	if !strings.HasPrefix(cookie, "AISDLC_SESSION=") || strings.Contains(cookie, u.loginA) ||
		!strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Strict") ||
		!strings.Contains(cookie, "Path=/") || strings.Contains(cookie, "Secure") || strings.Contains(cookie, "Max-Age") {
		t.Fatalf("cookie %q", cookie)
	}
	body := r.JSON(t)
	if body["login"] != "auth.a" || body["displayName"] != "An A" || body["role"] != "dev" || body["csrf"] == "" || body["csrf"] == nil {
		t.Fatal(body)
	}
	if !strings.HasPrefix(r.Body, `{"login":"auth.a","displayName":"An A","role":"dev","csrf":"`) {
		t.Fatal("thứ tự trường như Java:", r.Body)
	}

	s := w.Session(u.loginA)
	if s.ID == u.loginA || len(s.ID) != 43 || len(s.CSRF) != 43 {
		t.Fatal(s)
	}
	// DB chỉ lưu SHA-256 của id phiên
	if count(t, env, "SELECT count(*) FROM sessions WHERE id_sha256 = $1", auth.SHA256(s.ID)) != 1 ||
		count(t, env, "SELECT count(*) FROM sessions WHERE id_sha256 = $1", s.ID) != 0 {
		t.Fatal("sessions phải lưu SHA-256")
	}
	if !queryBool(t, env, "SELECT expires_at - created_at = interval '12 hours' FROM sessions WHERE id_sha256 = $1", auth.SHA256(s.ID)) {
		t.Fatal("hết hạn 12 giờ")
	}

	me := w.GetSession("/api/me", s)
	if me.Status != 200 || me.JSON(t)["login"] != "auth.a" || me.JSON(t)["csrf"] != s.CSRF {
		t.Fatal(me.Status, me.Body)
	}
	if r := w.GetSession("/api/zz", s); r.Status != 200 {
		t.Fatal(r.Status)
	}
	// hết hạn 12 giờ theo đồng hồ tiêm vào
	*env.Now = env.Now.Add(12*time.Hour - time.Second)
	if r := w.GetSession("/api/zz", s); r.Status != 200 {
		t.Fatal("còn hạn", r.Status)
	}
	*env.Now = env.Now.Add(time.Second)
	if r := w.GetSession("/api/zz", s); r.Status != 401 || r.Body != `{"error":"unauthorized"}` {
		t.Fatal("hết hạn", r.Status, r.Body)
	}
}

func TestOnlyLoginKindCanLogIn(t *testing.T) {
	env, w := newEnv(t)
	u := addAuthUsers(t, env)
	s := w.Session(u.loginA)
	_, ingest := w.CreateToken(s, "ingest", "may")
	_, approve := w.CreateToken(s, "approve", "may")
	for _, tok := range []string{ingest, approve, u.ci, "nope", ""} {
		r := w.Login(tok)
		if r.Status != 401 || !strings.Contains(r.Body, "Token không hợp lệ") || r.Header.Get("Set-Cookie") != "" {
			t.Fatal(tok, r.Status, r.Body)
		}
	}
	if r := w.Do("POST", "/api/login", "{", map[string]string{"Content-Type": "application/json"}); r.Status != 400 {
		t.Fatal("JSON hỏng", r.Status)
	}
}

func TestLogoutNeedsCsrfThenSessionGone(t *testing.T) {
	env, w := newEnv(t)
	u := addAuthUsers(t, env)
	s := w.Session(u.loginA)
	if r := w.PostSession("/api/logout", "", s, ""); r.Status != 403 || r.Body != `{"error":"forbidden"}` {
		t.Fatal(r.Status, r.Body)
	}
	if r := w.PostSession("/api/logout", "", s, "sai"); r.Status != 403 {
		t.Fatal(r.Status)
	}
	if r := w.GetSession("/api/zz", s); r.Status != 200 {
		t.Fatal(r.Status)
	}
	out := w.PostSession("/api/logout", "", s, s.CSRF)
	if out.Status != 200 || out.Body != `{"ok":true}` {
		t.Fatal(out.Status, out.Body)
	}
	if c := out.Header.Get("Set-Cookie"); c != "AISDLC_SESSION=; Path=/; Max-Age=0; Expires=Thu, 01 Jan 1970 00:00:00 GMT; HttpOnly; SameSite=Strict" {
		t.Fatalf("cookie xóa %q", c)
	}
	if r := w.GetSession("/api/zz", s); r.Status != 401 {
		t.Fatal(r.Status)
	}
	if r := w.PostSession("/api/logout", "", s, s.CSRF); r.Status != 401 {
		t.Fatal(r.Status)
	}
}

func TestSessionOpenAndCloseAreAudited(t *testing.T) {
	env, w := newEnv(t)
	u := addAuthUsers(t, env)
	q := "SELECT count(*) FROM audit WHERE login = 'auth.b' AND channel = 'web' AND action = $1"
	open0, close0 := count(t, env, q, "session.open"), count(t, env, q, "session.close")
	s := w.Session(u.loginB)
	if count(t, env, q, "session.open") != open0+1 {
		t.Fatal("session.open")
	}
	if r := w.Login("sai-token"); r.Status != 401 {
		t.Fatal(r.Status)
	}
	if count(t, env, q, "session.open") != open0+1 {
		t.Fatal("login hỏng không ghi session.open")
	}
	if r := w.PostSession("/api/logout", "", s, s.CSRF); r.Status != 200 {
		t.Fatal(r.Status)
	}
	if count(t, env, q, "session.close") != close0+1 {
		t.Fatal("session.close")
	}
	rows := queryStrings(t, env, "SELECT a::text FROM audit a WHERE action LIKE 'session.%'")
	if len(rows) == 0 {
		t.Fatal("không có audit")
	}
	for _, a := range rows {
		if strings.Contains(a, u.loginB) || strings.Contains(a, s.ID) || strings.Contains(a, s.CSRF) {
			t.Fatal("audit lộ bí mật:", a)
		}
	}
	if ip := queryStrings(t, env, "SELECT ip FROM audit WHERE action = 'session.open'"); ip[0] != "127.0.0.1" {
		t.Fatal("ip không kèm cổng:", ip)
	}
}

func TestCsrfOfSessionAIsRejectedForSessionB(t *testing.T) {
	env, w := newEnv(t)
	u := addAuthUsers(t, env)
	a, b := w.Session(u.loginA), w.Session(u.loginB)
	if r := w.PostSession("/api/logout", "", b, a.CSRF); r.Status != 403 {
		t.Fatal(r.Status)
	}
	if r := w.PostSession("/api/me/tokens", `{"kind":"ingest","label":"x"}`, b, a.CSRF); r.Status != 403 {
		t.Fatal(r.Status)
	}
	if r := w.GetSession("/api/zz", b); r.Status != 200 {
		t.Fatal(r.Status)
	}
}

func TestTokenKindRulesPerPath(t *testing.T) {
	env, w := newEnv(t)
	u := addAuthUsers(t, env)
	s := w.Session(u.loginA)
	_, ingest := w.CreateToken(s, "ingest", "may")
	_, approve := w.CreateToken(s, "approve", "may")
	expect := func(name string, r webtest.Resp, want ...int) {
		t.Helper()
		for _, c := range want {
			if r.Status == c {
				return
			}
		}
		t.Errorf("%s: %d %s, muốn %v", name, r.Status, r.Body, want)
	}
	// /api/zz thay /api/board; /api/ingest/zz thay /api/ingest/snapshot
	board := w.GetBearer("/api/zz", ingest)
	expect("ingest đọc", board, 403)
	if !strings.Contains(board.Body, `"error":"forbidden"`) {
		t.Fatal(board.Body)
	}
	expect("ingest ingest", w.PostBearer("/api/ingest/zz", snap, ingest), 200)
	expect("login ingest", w.PostBearer("/api/ingest/zz", snap, u.loginA), 200)
	expect("login đọc", w.GetBearer("/api/zz", u.loginA), 200)
	expect("approve ingest", w.PostBearer("/api/ingest/zz", snap, approve), 403)
	expect("approve đọc", w.GetBearer("/api/zz", approve), 403)
	expect("ci đọc", w.GetBearer("/api/zz", u.ci), 403)
	expect("ci ingest", w.PostBearer("/api/ingest/zz", snap, u.ci), 403)
	expect("login cli", w.GetBearer("/api/cli/x", u.loginA), 403)
	expect("ingest cli", w.GetBearer("/api/cli/x", ingest), 404)
	expect("login ci", w.GetBearer("/api/ci/x", u.loginA), 403)
	expect("ci ci", w.GetBearer("/api/ci/x", u.ci), 404)
	expect("mã hóa", w.GetBearer("/api/c%69/x", u.loginA), 403)
	expect("..%2F", w.GetBearer("/api/x/..%2Fci/x", u.loginA), 400, 403)
	expect("login gates", w.PostBearer("/api/gates/x", "{}", u.loginA), 403)
	expect("ingest gates", w.PostBearer("/api/gates/x", "{}", ingest), 403)
	expect("approve gates", w.PostBearer("/api/gates/x", "{}", approve), 404)
	expect("phiên gates không CSRF", w.PostSession("/api/gates/x", "{}", s, ""), 403)
	expect("phiên gates CSRF", w.PostSession("/api/gates/x", "{}", s, s.CSRF), 404)
	expect("Bearer logout", w.PostBearer("/api/logout", "", u.loginA), 403)
	expect("Bearer tạo token", w.PostBearer("/api/me/tokens", `{"kind":"ingest","label":"x"}`, u.loginA), 403)
	expect("phiên ingest", w.PostSession("/api/ingest/zz", snap, s, s.CSRF), 403)
	r := w.GetBearer("/api/zz", "nope")
	expect("token sai", r, 401)
	if r.Body != `{"error":"unauthorized"}` || r.Header.Get("Content-Type") != "application/json" {
		t.Fatal(r.Body, r.Header)
	}
	me := w.GetBearer("/api/me", u.loginA)
	expect("me Bearer", me, 200)
	if v, ok := me.JSON(t)["csrf"]; !ok || v != nil {
		t.Fatal("csrf phải null:", me.Body)
	}
}
