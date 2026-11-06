package auth_test

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/webtest"
)

// Port UserAdminCliTest.java.

type cliResult struct {
	code int
	out  string
}

func adminEnv(t *testing.T) (*webtest.Env, func(args ...string) cliResult) {
	env, _ := newEnv(t)
	return env, func(args ...string) cliResult {
		var b bytes.Buffer
		code := auth.RunAdmin(context.Background(), env.Pool, args, &b)
		return cliResult{code, b.String()}
	}
}

var tokenOut = regexp.MustCompile(`^Token: [A-Za-z0-9_-]{43}\n$`)

func role(t *testing.T, env *webtest.Env, login string) string {
	return queryStrings(t, env, "SELECT role FROM users WHERE login = $1", login)[0]
}

func TestAddPrintsTokenOnceStoresHash(t *testing.T) {
	env, exec := adminEnv(t)
	r := exec("add", "cli.an", "An Nguyen", "--email", "an@corp")
	if r.code != 0 || !tokenOut.MatchString(r.out) {
		t.Fatalf("%d %q", r.code, r.out)
	}
	token := strings.TrimPrefix(strings.TrimSpace(r.out), "Token: ")
	user := queryStrings(t, env, "SELECT u::text FROM users u WHERE login = 'cli.an'")[0]
	if strings.Contains(user, token) {
		t.Fatal(user)
	}
	tok := queryStrings(t, env, "SELECT t::text FROM tokens t JOIN users u ON u.id = t.user_id WHERE u.login = 'cli.an'")[0]
	if strings.Contains(tok, token) || !strings.Contains(tok, auth.SHA256(token)) || !strings.Contains(tok, "login") {
		t.Fatal(tok)
	}
	if role(t, env, "cli.an") != "dev" {
		t.Fatal("role")
	}
	list := exec("list")
	if list.code != 0 || list.out != "cli.an\tAn Nguyen\tan@corp\tdev\n" {
		t.Fatalf("%q", list.out)
	}
	exec("add", "b.no-mail", "B")
	if l := exec("list").out; l != "b.no-mail\tB\t\tdev\ncli.an\tAn Nguyen\tan@corp\tdev\n" {
		t.Fatalf("%q", l)
	}
	dup := exec("add", "cli.an", "Trùng")
	if dup.code != 1 || dup.out != "Login đã tồn tại: cli.an\n" {
		t.Fatalf("%d %q", dup.code, dup.out)
	}
	if count(t, env, "SELECT count(*) FROM tokens t JOIN users u ON u.id = t.user_id WHERE u.login = 'cli.an'") != 1 {
		t.Fatal("token thừa")
	}
	if count(t, env, "SELECT count(*) FROM audit WHERE channel = 'admin' AND action = 'user.add' AND target = 'cli.an' AND login IS NULL AND detail->>'role' = 'dev' AND detail ? 'tokenId'") != 1 {
		t.Fatal("audit user.add")
	}
}

func TestAddWithRoleLeadAndChangeRole(t *testing.T) {
	env, exec := adminEnv(t)
	if r := exec("add", "cli.lead", "Lê Lead", "--role", "lead"); !strings.HasPrefix(r.out, "Token: ") {
		t.Fatal(r.out)
	}
	if role(t, env, "cli.lead") != "lead" {
		t.Fatal("lead")
	}
	usage := "Cách dùng: user add <login> \"<Tên>\" [--email <e>] [--role dev|lead] | user role <login> dev|lead" +
		" | user login-token <login> | user ci-token <tên> | user list\n"
	for _, args := range [][]string{{"add", "cli.x", "X", "--role", "boss"}, {"add", "cli.x", "X", "--bad"}, {"add", "cli.x", "X", "--email"},
		{"add", "cli.x"}, {}, {"nope"}, {"role", "cli.lead"}} {
		if r := exec(args...); r.code != 2 || r.out != usage {
			t.Fatalf("%v: %d %q", args, r.code, r.out)
		}
	}
	if r := exec("add", "cli.dev", "Dev"); !strings.HasPrefix(r.out, "Token: ") {
		t.Fatal(r.out)
	}
	r := exec("role", "cli.dev", "lead")
	if r.code != 0 || strings.Contains(r.out, "Token:") || r.out != "Đã đặt vai trò lead cho cli.dev\n" {
		t.Fatalf("%d %q", r.code, r.out)
	}
	if role(t, env, "cli.dev") != "lead" {
		t.Fatal("lead")
	}
	if exec("role", "cli.dev", "dev").code != 0 || role(t, env, "cli.dev") != "dev" {
		t.Fatal("dev")
	}
	if exec("role", "cli.dev", "boss").code != 2 {
		t.Fatal("boss")
	}
	if r := exec("role", "cli.khong-co", "lead"); r.code != 1 || r.out != "Không có login: cli.khong-co\n" {
		t.Fatalf("%d %q", r.code, r.out)
	}
	if count(t, env, "SELECT count(*) FROM audit WHERE channel = 'admin' AND action = 'user.role' AND target = 'cli.dev'") != 2 {
		t.Fatal("audit")
	}
}

func TestLoginTokenIssuesNewLoginTokenForExistingUser(t *testing.T) {
	env, exec := adminEnv(t)
	first := strings.TrimPrefix(strings.TrimSpace(exec("add", "cli.relogin", "Re Login").out), "Token: ")
	r := exec("login-token", "cli.relogin")
	if r.code != 0 || !tokenOut.MatchString(r.out) {
		t.Fatalf("%d %q", r.code, r.out)
	}
	second := strings.TrimPrefix(strings.TrimSpace(r.out), "Token: ")
	if second == first {
		t.Fatal("trùng")
	}
	if k := queryStrings(t, env, "SELECT t.kind FROM tokens t JOIN users u ON u.id = t.user_id WHERE u.login = 'cli.relogin' ORDER BY t.id"); strings.Join(k, ",") != "login,login" {
		t.Fatal(k)
	}
	if count(t, env, "SELECT count(*) FROM tokens WHERE sha256 = $1 AND kind = 'login'", auth.SHA256(second)) != 1 {
		t.Fatal("sha")
	}
	audit := queryStrings(t, env, "SELECT a::text FROM audit a WHERE action = 'token.create' AND channel = 'admin' AND detail->>'user' = 'cli.relogin'")
	if len(audit) != 1 || strings.Contains(audit[0], second) || strings.Contains(audit[0], first) || !strings.Contains(audit[0], `"login"`) {
		t.Fatal(audit)
	}
	none := exec("login-token", "cli.khong-co")
	if none.code != 1 || strings.Contains(none.out, "Token:") || none.out != "Không có login: cli.khong-co\n" {
		t.Fatalf("%d %q", none.code, none.out)
	}
	if exec("login-token").code != 2 || exec("login-token", " ").code != 2 {
		t.Fatal("usage")
	}
}

func TestCiTokenCreatesCiUserOnceAndCiKindToken(t *testing.T) {
	env, exec := adminEnv(t)
	out1 := exec("ci-token", "pipeline")
	if out1.code != 0 || !tokenOut.MatchString(out1.out) {
		t.Fatalf("%q", out1.out)
	}
	t1 := strings.TrimPrefix(strings.TrimSpace(out1.out), "Token: ")
	t2 := strings.TrimPrefix(strings.TrimSpace(exec("ci-token", "pipeline").out), "Token: ")
	if t1 == t2 {
		t.Fatal("trùng")
	}
	if count(t, env, "SELECT count(*) FROM users WHERE login = 'ci:pipeline' AND display_name = 'CI pipeline' AND role = 'dev'") != 1 {
		t.Fatal("user ci")
	}
	if k := queryStrings(t, env, "SELECT t.kind || '|' || t.label FROM tokens t JOIN users u ON u.id = t.user_id WHERE u.login = 'ci:pipeline'"); strings.Join(k, ",") != "ci|pipeline,ci|pipeline" {
		t.Fatal(k)
	}
	if count(t, env, "SELECT count(*) FROM tokens WHERE sha256 = $1", auth.SHA256(t1)) != 1 {
		t.Fatal("sha")
	}
	audit := queryStrings(t, env, "SELECT a::text FROM audit a WHERE action = 'token.create' AND channel = 'admin'")
	if len(audit) == 0 {
		t.Fatal("audit")
	}
	for _, a := range audit {
		if strings.Contains(a, t1) || strings.Contains(a, t2) {
			t.Fatal(a)
		}
	}
	if exec("ci-token").code != 2 {
		t.Fatal("usage")
	}
	addCi := exec("add", "ci:gia", "Gia")
	if addCi.code != 1 || addCi.out != "Login ci:gia dành cho user CI: dùng `user ci-token`\n" {
		t.Fatalf("%q", addCi.out)
	}
	if count(t, env, "SELECT count(*) FROM users WHERE login = 'ci:gia'") != 0 {
		t.Fatal("ci:gia")
	}
	ci := exec("login-token", "ci:pipeline")
	if ci.code != 1 || ci.out != "Không cấp token login cho user CI (ci:pipeline): dùng `user ci-token`\n" {
		t.Fatalf("%q", ci.out)
	}
	if k := queryStrings(t, env, "SELECT DISTINCT t.kind FROM tokens t JOIN users u ON u.id = t.user_id WHERE u.login = 'ci:pipeline'"); strings.Join(k, ",") != "ci" {
		t.Fatal(k)
	}
}

func TestAddRejectsReservedAutoLogin(t *testing.T) {
	env, exec := adminEnv(t)
	for _, login := range []string{"auto", "AUTO", "Auto"} {
		r := exec("add", login, "Tự động")
		if r.code != 1 || r.out != "Login "+login+" dành cho bản tự duyệt của server, chọn tên khác\n" {
			t.Fatalf("%d %q", r.code, r.out)
		}
	}
	if count(t, env, "SELECT count(*) FROM users WHERE lower(login) = 'auto'") != 0 {
		t.Fatal("auto")
	}
}
