package main

import (
	"bytes"
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/web"
	"aisdlc/internal/web/webtest"
)

func TestUserSubcommand(t *testing.T) {
	env := webtest.New(t, func(d web.Deps) http.Handler { return web.NewHandler(d) })
	t.Setenv("AISDLC_DB_URL", env.URL)
	t.Setenv("AISDLC_DB_USER", "")
	t.Setenv("AISDLC_DB_PASSWORD", "")
	exec := func(args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		code := run(context.Background(), args, &out, &errb)
		return code, out.String(), errb.String()
	}
	code, out, _ := exec("user", "add", "cmd.a", "Cmd A", "--role", "lead")
	if code != 0 || !regexp.MustCompile(`^Token: [A-Za-z0-9_-]{43}\n$`).MatchString(out) {
		t.Fatalf("%d %q", code, out)
	}
	tok := strings.TrimSpace(strings.TrimPrefix(out, "Token: "))
	if code, out, _ := exec("user", "list"); code != 0 || out != "cmd.a\tCmd A\t\tlead\n" {
		t.Fatalf("%d %q", code, out)
	}
	if code, out, _ := exec("user"); code != 2 || !strings.HasPrefix(out, "Cách dùng: user add") {
		t.Fatalf("%d %q", code, out)
	}
	if code, _, _ := exec("user", "add", "cmd.a", "Lại"); code != 1 {
		t.Fatal(code)
	}
	// token in ra đăng nhập được trên server
	c := env.Client(t)
	if r := c.Login(tok); r.Status != 200 || r.JSON(t)["role"] != "lead" {
		t.Fatal(r.Status, r.Body)
	}
	if code, _, errs := exec("nope"); code != 2 || !strings.Contains(errs, "chưa được hỗ trợ") {
		t.Fatal(code, errs)
	}
}

func TestUserSubcommandBadDB(t *testing.T) {
	t.Setenv("AISDLC_DB_URL", "postgres://127.0.0.1:1/x?connect_timeout=1")
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"user", "list"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "aisdlc-web:") || out.Len() != 0 {
		t.Fatal(code, out.String(), errb.String())
	}
}
