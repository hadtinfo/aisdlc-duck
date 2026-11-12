package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/risk"
	"aisdlc/internal/webclient"
	"aisdlc/internal/webclient/fakeweb"
)

// WebLoginTest, CliFlowTest.webStatusShowsStateAndPending, PolicyCommandTest (phần pull) và
// TestNoTokenInAnyErrorOutput.

// loginConsole như console giả của WebLoginTest: URL qua ReadLine (hiện chữ), token chỉ qua ReadSecret (ẩn chữ).
type loginConsole struct{ url, token string }

func (c loginConsole) ReadLine(prompt string) (string, bool) {
	if strings.HasPrefix(prompt, "URL") {
		return c.url, true
	}
	return "token-gõ-hiện-chữ", true
}

func (c loginConsole) ReadSecret(string) (string, bool) { return c.token, true }

func loginRepo(t *testing.T) (*testRepo, *fakeweb.Server, string) {
	r := newTestRepo(t)
	web := fakeweb.New(t)
	web.SetFeature("local/x", "PAY-1")
	home := t.TempDir()
	r.env["HOME"] = home
	return r, web, home
}

func cfgDir(home string) string { return filepath.Join(home, ".config", "aisdlc") }

func perm(t *testing.T, f string) os.FileMode {
	st, err := os.Stat(f)
	must(t, err)
	return st.Mode().Perm()
}

func TestWithoutTtyExitsThree(t *testing.T) {
	r, _, home := loginRepo(t)
	eq(t, r.run("web", "login"), 3)
	eq(t, r.run("web", "approve-token"), 3)
	// console chỉ đọc được chữ hiện (không ẩn được token) cũng như không có terminal
	eq(t, r.runC(typed("x"), "web", "approve-token"), 3)
	_, err := os.Stat(cfgDir(home))
	eq(t, os.IsNotExist(err), true)
}

func TestForbiddenTokenSaysIngestTokenNeeded(t *testing.T) {
	r, web, home := loginRepo(t)
	web.Update(func(s *fakeweb.Server) {
		s.Override["GET /api/cli/pubkey"] = fakeweb.JSONResp(403, `{"error":"Sai loại token"}`)
	})
	eq(t, r.runC(loginConsole{web.URL(), "login-tok"}, "web", "login"), 2)
	contains(t, r.err, "Token không dùng được cho CLI (cần token ingest)")
	_, err := os.Stat(webclient.ConfigFile(home))
	eq(t, os.IsNotExist(err), true)
	eq(t, webclient.LoadPinnedKey(home) == nil, true)
}

func TestSuccessWritesWebJson600AndPinsKey(t *testing.T) {
	r, web, home := loginRepo(t)
	if code := r.runC(loginConsole{web.URL() + "/", "ingest-tok"}, "web", "login"); code != 0 {
		t.Fatalf("exit %d\n%s", code, r.err)
	}
	f := webclient.ConfigFile(home)
	var n map[string]string
	b, err := os.ReadFile(f)
	must(t, err)
	must(t, json.Unmarshal(b, &n))
	eq(t, n["url"], web.URL())
	eq(t, n["token"], "ingest-tok")
	eq(t, perm(t, f), os.FileMode(0o600))
	eq(t, webclient.LoadPinnedKey(home).Equal(web.Pub), true)
	eq(t, web.RequestsFor("GET", "/api/cli/pubkey")[0].Auth, "Bearer ingest-tok")
	notContains(t, r.out+r.err, "ingest-tok")
	contains(t, r.out, "Đã lưu "+f+" và ghim khóa web "+web.KeyID()+".")
	// lần sau: URL mặc định là giá trị đã lưu (dev chỉ Enter)
	if code := r.runC(loginConsole{"", "ingest-tok-2"}, "web", "login"); code != 0 {
		t.Fatalf("exit %d\n%s", code, r.err)
	}
	b, err = os.ReadFile(f)
	must(t, err)
	must(t, json.Unmarshal(b, &n))
	eq(t, n["url"], web.URL())
	eq(t, n["token"], "ingest-tok-2")
}

func TestRotatedKeyWithoutEndorsementIsRefused(t *testing.T) {
	r, web, home := loginRepo(t)
	eq(t, r.runC(loginConsole{web.URL(), "ingest-tok"}, "web", "login"), 0)
	other := fakeweb.New(t)
	eq(t, r.runC(loginConsole{other.URL(), "ingest-tok"}, "web", "login"), 2)
	contains(t, r.err, "Khóa server đã đổi mà không được khóa cũ ký")
	eq(t, webclient.LoadPinnedKey(home).Equal(web.Pub), true)
	var n map[string]string
	b, err := os.ReadFile(webclient.ConfigFile(home))
	must(t, err)
	must(t, json.Unmarshal(b, &n))
	eq(t, n["url"], web.URL())
}

func TestApproveTokenStored600(t *testing.T) {
	r, web, home := loginRepo(t)
	eq(t, r.runC(loginConsole{"x", "appr-tok"}, "web", "approve-token"), 0)
	f := webclient.ApproveTokenFile(home)
	b, err := os.ReadFile(f)
	must(t, err)
	eq(t, strings.TrimSpace(string(b)), "appr-tok")
	eq(t, perm(t, f), os.FileMode(0o600))
	notContains(t, r.out+r.err, "appr-tok")
	eq(t, len(web.Requests()), 0)
}

const envWarning = "AISDLC_WEB_TOKEN đang đặt trong môi trường. Token đăng nhập (login) không dùng" +
	" cho CLI và agent đọc được biến này: xóa nó khỏi môi trường, dùng `aisdlc web login` với token ingest."

// Phần lệnh của webJsonBeatsEnvToken (phần đọc cấu hình ở webclient TestLoadConfigWebJsonBeatsEnv).
func TestWebJsonBeatsEnvToken(t *testing.T) {
	r, web, _ := loginRepo(t)
	if code := r.runC(loginConsole{web.URL(), "ingest-tok"}, "web", "login"); code != 0 {
		t.Fatalf("exit %d\n%s", code, r.err)
	}
	r.env["AISDLC_WEB_URL"] = "http://127.0.0.1:1"
	r.env["AISDLC_WEB_TOKEN"] = "login-tok-2a"
	c, ok := LoadWebConfig(r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}))
	eq(t, ok, true)
	eq(t, c.Token, "ingest-tok")
	eq(t, c.URL, web.URL())
}

// Phần lệnh của envTokenOnlyStillUsedButWarned: web status và web login nhắc, không in token.
func TestEnvTokenOnlyStillUsedButWarned(t *testing.T) {
	r, web, _ := loginRepo(t)
	r.setupInit()
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "login-tok-2a"
	c, ok := LoadWebConfig(r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}))
	eq(t, ok, true)
	eq(t, c.Token, "login-tok-2a")
	eq(t, r.run("web", "status"), 0)
	contains(t, r.err, envWarning)
	notContains(t, r.out+r.err, "login-tok-2a")
	// web login cũng nhắc (và vẫn lưu web.json bằng token ingest)
	if code := r.runC(loginConsole{web.URL(), "ingest-tok"}, "web", "login"); code != 0 {
		t.Fatalf("exit %d\n%s", code, r.err)
	}
	contains(t, r.err, envWarning)
	notContains(t, r.out+r.err, "login-tok-2a", "ingest-tok")
}

func TestNoWarningWithoutEnvToken(t *testing.T) {
	r, _, _ := loginRepo(t)
	r.setupInit()
	eq(t, r.run("web", "status"), 0)
	notContains(t, r.err, "AISDLC_WEB_TOKEN")
}

// CliFlowTest.webStatusShowsStateAndPending.
func TestWebStatusShowsStateAndPending(t *testing.T) {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	if code := r.run("web", "status"); code != 0 {
		t.Fatalf("exit %d\n%s", code, r.err)
	}
	contains(t, r.out, "Web: tắt", "Đang chờ gửi: 0", "Bị từ chối: 0", "Gửi thành công lần cuối: chưa")
	r.env["AISDLC_WEB_URL"] = "http://127.0.0.1:1"
	r.env["AISDLC_WEB_TOKEN"] = "x"
	r.ok(noConsole(), "init", "PAY-1")
	eq(t, r.run("web", "status"), 0)
	contains(t, r.out, "Web: http://127.0.0.1:1", "Đang chờ gửi: 2")
	r.write(".aisdlc/web/rejected/event-1-a.json", "{}")
	eq(t, r.run("web", "status"), 0)
	contains(t, r.out, "Bị từ chối: 1")
}

func TestWebWithoutSubcommandPrintsUsage(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	eq(t, r.run("web"), 2)
	contains(t, r.err, "login", "approve-token", "migrate")
}

func TestWebPushStagesEveryFeatureAndSendsAll(t *testing.T) {
	web := fakeweb.New(t)
	r := newTestRepo(t)
	r.setupInit()
	r.ok(noConsole(), "init", "PAY-2")
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "t0k"
	eq(t, r.run("web", "push"), 0)
	contains(t, r.out, "Đã gửi 2, còn chờ 0")
	eq(t, len(web.RequestsFor("POST", "/api/ingest/snapshot")), 2)
	r.webOff()
	eq(t, r.run("web", "push"), 2)
	contains(t, r.err, "Web chưa bật: chạy `aisdlc web login`")
}

// ---- PolicyCommandTest (pull) ----

func policyWeb(t *testing.T, setupArgs ...string) (*testRepo, *fakeweb.Server, string) {
	r := newTestRepo(t)
	home := t.TempDir()
	r.env["HOME"] = home
	r.ok(noConsole(), append([]string{"setup"}, setupArgs...)...)
	repoID := NewWebCache(r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), r.root).RepoID()
	web := fakeweb.New(t)
	web.SetFeature(repoID, "PAY-1")
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "ingest-tok"
	must(t, webclient.Pin(home, web.Pub))
	return r, web, repoID
}

func TestPullImportsWhen404ThenCaches(t *testing.T) {
	r, web, repoID := policyWeb(t, "--preset", "banking")
	if code := r.run("policy", "pull"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	contains(t, r.out, "Chính sách v1 (web)")
	gets := web.RequestsFor("GET", "/api/cli/policy")
	eq(t, len(gets), 2)
	for _, q := range gets {
		eq(t, q.Auth, "Bearer ingest-tok")
	}
	imp := web.RequestsFor("POST", "/api/cli/policy/import")[0]
	eq(t, imp.Field("repo"), repoID)
	eq(t, imp.Field("yaml"), r.read("aisdlc/risk-policy.yaml"))
	eq(t, r.exists(".aisdlc/web/policy.json"), true)
	l, err := LoadPolicy(r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), r.root, true)
	must(t, err)
	eq(t, l.Origin, "web")
	eq(t, l.Policy.Preset.Name, "banking")
	// web đã có chính sách: pull không import lại
	web.Update(func(s *fakeweb.Server) { s.PolicyVersion = 3 })
	eq(t, r.run("policy", "pull"), 0)
	contains(t, r.out, "Chính sách v3 (web)")
	eq(t, len(web.RequestsFor("POST", "/api/cli/policy/import")), 1)
}

func TestPullNoRepoFileImportsFallbackWithoutFlow(t *testing.T) {
	r, web, _ := policyWeb(t)
	must(t, os.Remove(filepath.Join(r.root, "aisdlc/risk-policy.yaml")))
	if code := r.run("policy", "pull"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	yaml := web.RequestsFor("POST", "/api/cli/policy/import")[0].Field("yaml")
	// thiếu file chính sách là duyệt tay: mẫu dự phòng gửi lên web không có flow
	p, err := risk.ParsePolicy(yaml)
	must(t, err)
	eq(t, len(p.Flow), 0)
	eq(t, p.Preset.Name, "generic")
	eq(t, len(p.Rules) > 0, true)
	l, err := LoadPolicy(r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), r.root, true)
	must(t, err)
	eq(t, len(l.Policy.Flow), 0)
}

func TestPullRefusesEnvelopeSignedByOtherKey(t *testing.T) {
	r, web, _ := policyWeb(t)
	_, other := newKey(t)
	web.Update(func(s *fakeweb.Server) { s.Signer = other })
	eq(t, r.run("policy", "pull"), 2)
	contains(t, r.err, "Không lấy được chính sách từ web: phản hồi không xác minh được bằng khóa đã ghim (hoặc sai repo).")
	eq(t, r.exists(".aisdlc/web/policy.json"), false)
}

// ---- token không bao giờ ra output ----

// TestNoTokenInAnyErrorOutput chạy mọi lệnh web (và lệnh gọi web ở chế độ web) khi server giả trả 401, 403, 500
// (body phản chiếu token) hoặc lỗi mạng; stdout và stderr không bao giờ chứa token ingest hay token approve.
func TestNoTokenInAnyErrorOutput(t *testing.T) {
	const ingest, approve = "ingest-SECRET-tok-1", "appr-SECRET-tok-2"
	g := newGatesFx(t)
	delete(g.env, "AISDLC_WEB_TOKEN")
	delete(g.env, "AISDLC_WEB_URL")
	must(t, webclient.SaveConfig(g.home, webclient.Config{URL: g.web.URL(), Token: ingest, ApproveToken: approve}))
	g.designAwaiting()
	g.saveLocalApproval(gate.BRD) // web migrate có bản duyệt cục bộ để gửi lên /api/cli/import
	eq(t, g.run("status"), 0)     // cache cổng: DESIGN chờ duyệt
	paths := []string{"GET /api/cli/pubkey", "POST /api/cli/mode", "GET /api/cli/gates", "GET /api/cli/control",
		"GET /api/cli/policy", "POST /api/cli/policy/import", "POST /api/cli/import", "POST /api/cli/submissions",
		"POST /api/gates/approve", "POST /api/gates/reject", "POST /api/gates/reopen", "POST /api/ingest/events",
		"POST /api/ingest/snapshot"}
	echo := `{"error":"token sai: ` + ingest + ` / ` + approve + ` / Bearer ` + ingest + `"}`
	scenarios := []struct {
		name  string
		apply func()
	}{
		{"401", func() { g.overrideAll(paths, 401, echo) }},
		{"403", func() { g.overrideAll(paths, 403, `{"error":"forbidden"}`) }},
		{"403 có câu", func() { g.overrideAll(paths, 403, echo) }},
		{"500", func() { g.overrideAll(paths, 500, echo) }},
		{"500 không JSON", func() { g.overrideAll(paths, 500, "Bearer "+ingest) }},
		{"mạng (đóng kết nối)", func() {
			g.overrideAll(paths, 0, "")
			g.web.Update(func(s *fakeweb.Server) { s.Drop = true })
		}},
		{"mạng (cổng đóng)", func() {
			g.web.Update(func(s *fakeweb.Server) { s.Drop = false })
			must(t, webclient.SaveConfig(g.home, webclient.Config{URL: closedPort(t), Token: ingest}))
		}},
	}
	commands := []struct {
		console DevConsole
		args    []string
	}{
		{noConsole(), []string{"web", "status"}},
		{noConsole(), []string{"web", "push"}},
		{noConsole(), []string{"web", "migrate"}},
		{noConsole(), []string{"web", "flush"}},
		{noConsole(), []string{"policy", "pull"}},
		{noConsole(), []string{"status"}},
		{noConsole(), []string{"submit", "brd"}},
		{typed("PAY-1"), []string{"approve", "design"}},
		{typed("PAY-1"), []string{"reject", "design", "-m", "sửa"}},
		{typed("PAY-1"), []string{"reopen", "design"}},
		{loginConsole{g.web.URL(), ingest}, []string{"web", "login"}},
	}
	for _, s := range scenarios {
		s.apply()
		for _, c := range commands {
			_ = os.Remove(filepath.Join(g.root, ".aisdlc/web/backoff-until")) // mỗi lệnh đều thử mạng
			code := g.runC(c.console, c.args...)
			all := g.out + g.err
			t.Logf("%s | %s → exit %d: %s", s.name, strings.Join(c.args, " "), code, lastLine(g.err))
			for _, tok := range []string{ingest, approve} {
				if strings.Contains(all, tok) {
					t.Fatalf("%s, %s (exit %d): lộ token\n%s", s.name, strings.Join(c.args, " "), code, all)
				}
			}
		}
	}
}

func (g *gatesFx) overrideAll(paths []string, status int, body string) {
	g.web.Update(func(s *fakeweb.Server) {
		for _, p := range paths {
			if status == 0 {
				delete(s.Override, p)
			} else {
				s.Override[p] = fakeweb.JSONResp(status, body)
			}
		}
	})
}

// `web status` không in userinfo (user:mật khẩu@) của URL web.
func TestWebStatusStripsUserinfo(t *testing.T) {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	r.env["AISDLC_WEB_URL"] = "http://alice:s3cret@127.0.0.1:1"
	r.env["AISDLC_WEB_TOKEN"] = "x"
	eq(t, r.run("web", "status"), 0)
	contains(t, r.out, "Web: http://127.0.0.1:1")
	notContains(t, r.out+r.err, "alice", "s3cret")
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func TestPullImportRejectedShowsServerMessage(t *testing.T) {
	r, web, _ := policyWeb(t)
	msg := "Chính sách dùng schema 1 (luật so khớp); chạy `aisdlc policy migrate` để chuyển sang schema 2."
	web.Update(func(s *fakeweb.Server) {
		s.Override["POST /api/cli/policy/import"] = fakeweb.JSONResp(400, `{"error":"`+msg+`","errors":["`+msg+`"]}`)
	})
	eq(t, r.run("policy", "pull"), 2)
	contains(t, r.err, "web không nhận chính sách (HTTP 400): "+msg)
}
