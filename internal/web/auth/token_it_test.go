package auth_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/webtest"
)

// Port TokenIT.java.

func tokenRow(t *testing.T, r webtest.Resp, id int64) map[string]any {
	t.Helper()
	var list []map[string]any
	if err := jsonUnmarshal(r.Body, &list); err != nil {
		t.Fatal(err, r.Body)
	}
	for _, m := range list {
		if int64(m["id"].(float64)) == id {
			return m
		}
	}
	return nil
}

var instantRe = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d{3}|\.\d{6}|\.\d{9})?Z$`)

func TestCreateUseListRevoke(t *testing.T) {
	env, w := newEnv(t)
	loginC := webtest.CliToken(t, env.Pool, "add", "token.c", "Chi C")
	s := w.Session(loginC)
	id, token := w.CreateToken(s, "ingest", "laptop")
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(token) {
		t.Fatal(token)
	}
	if count(t, env, "SELECT count(*) FROM tokens WHERE sha256 = $1", token) != 0 ||
		count(t, env, "SELECT count(*) FROM tokens WHERE sha256 = $1", auth.SHA256(token)) != 1 {
		t.Fatal("DB chỉ có SHA-256")
	}

	list := w.GetSession("/api/me/tokens", s)
	if list.Status != 200 || strings.Contains(list.Body, token) {
		t.Fatal(list.Status, list.Body)
	}
	row := tokenRow(t, list, id)
	if row["kind"] != "ingest" || row["label"] != "laptop" || row["createdAt"] == nil || row["lastUsedAt"] != nil || row["revokedAt"] != nil {
		t.Fatal(row)
	}
	if _, ok := row["token"]; ok {
		t.Fatal(row)
	}
	if _, ok := row["sha256"]; ok {
		t.Fatal(row)
	}
	if !instantRe.MatchString(row["createdAt"].(string)) || row["createdAt"] != "2026-11-05T10:00:00Z" {
		t.Fatal(row["createdAt"])
	}
	if !strings.Contains(list.Body, fmt.Sprintf(`{"id":%d,"kind":"ingest","label":"laptop","createdAt":"2026-11-05T10:00:00Z","lastUsedAt":null,"revokedAt":null}`, id)) {
		t.Fatal("thứ tự trường như Java:", list.Body)
	}

	if r := w.PostBearer("/api/ingest/zz", snap, token); r.Status != 200 {
		t.Fatal(r.Status)
	}
	if !queryBool(t, env, "SELECT last_used_at IS NOT NULL FROM tokens WHERE id = $1", id) {
		t.Fatal("last_used_at")
	}
	if tokenRow(t, w.GetSession("/api/me/tokens", s), id)["lastUsedAt"] == nil {
		t.Fatal("lastUsedAt")
	}
	// last_used_at cập nhật tối đa mỗi phút một lần
	*env.Now = env.Now.Add(30 * time.Second)
	w.PostBearer("/api/ingest/zz", snap, token)
	if got := tokenRow(t, w.GetSession("/api/me/tokens", s), id)["lastUsedAt"]; got != "2026-11-05T10:00:00Z" {
		t.Fatal(got)
	}
	*env.Now = env.Now.Add(31*time.Second + 500*time.Millisecond)
	w.PostBearer("/api/ingest/zz", snap, token)
	if got := tokenRow(t, w.GetSession("/api/me/tokens", s), id)["lastUsedAt"]; got != "2026-11-05T10:01:01.500Z" {
		t.Fatal(got)
	}

	rv := w.PostSession(fmt.Sprintf("/api/me/tokens/%d/revoke", id), "", s, s.CSRF)
	if rv.Status != 200 || rv.JSON(t)["revokedAt"] == nil || rv.JSON(t)["kind"] != "ingest" {
		t.Fatal(rv.Status, rv.Body)
	}
	if r := w.PostBearer("/api/ingest/zz", snap, token); r.Status != 401 {
		t.Fatal(r.Status)
	}
	if tokenRow(t, w.GetSession("/api/me/tokens", s), id)["revokedAt"] == nil {
		t.Fatal("revokedAt")
	}
	// thu hồi lại là no-op, giữ thời điểm cũ
	first := rv.JSON(t)["revokedAt"]
	*env.Now = env.Now.Add(time.Hour)
	again := w.PostSession(fmt.Sprintf("/api/me/tokens/%d/revoke", id), "", s, s.CSRF)
	if again.Status != 200 || again.JSON(t)["revokedAt"] != first {
		t.Fatal(again.Body)
	}
}

func TestCannotRevokeOthersTokenOrCreateLoginCi(t *testing.T) {
	env, w := newEnv(t)
	c := w.Session(webtest.CliToken(t, env.Pool, "add", "token.c", "Chi C"))
	d := w.Session(webtest.CliToken(t, env.Pool, "add", "token.d", "Dũng D"))
	id, _ := w.CreateToken(c, "approve", "terminal")

	if r := w.PostSession(fmt.Sprintf("/api/me/tokens/%d/revoke", id), "", d, d.CSRF); r.Status != 404 || r.Body != `{"error":"Không có token này"}` {
		t.Fatal(r.Status, r.Body)
	}
	if r := w.PostSession("/api/me/tokens/999999999/revoke", "", d, d.CSRF); r.Status != 404 {
		t.Fatal(r.Status)
	}
	if r := w.PostSession("/api/me/tokens/abc/revoke", "", d, d.CSRF); r.Status != 400 {
		t.Fatal(r.Status)
	}
	if !queryBool(t, env, "SELECT revoked_at IS NULL FROM tokens WHERE id = $1", id) {
		t.Fatal("bị thu hồi")
	}
	if tokenRow(t, w.GetSession("/api/me/tokens", d), id) != nil {
		t.Fatal("thấy token người khác")
	}
	for _, kind := range []string{"login", "ci", "nope"} {
		r := w.PostSession("/api/me/tokens", `{"kind":"`+kind+`","label":"x"}`, c, c.CSRF)
		if r.Status != 400 || r.Body != `{"error":"kind phải là ingest hoặc approve"}` {
			t.Fatal(kind, r.Status, r.Body)
		}
	}
	for _, body := range []string{`{"kind":"ingest","label":"  "}`, `{"kind":"ingest"}`, `{"kind":"ingest","label":5}`,
		`{"kind":"ingest","label":"` + strings.Repeat("ữ", 101) + `"}`} {
		r := w.PostSession("/api/me/tokens", body, c, c.CSRF)
		if r.Status != 400 || r.Body != `{"error":"label bắt buộc, tối đa 100 ký tự"}` {
			t.Fatal(body, r.Status, r.Body)
		}
	}
	// label được strip, 100 ký tự sau strip vẫn hợp lệ
	id2, _ := w.CreateToken(c, "ingest", "  "+strings.Repeat("ữ", 100)+"  ")
	if got := tokenRow(t, w.GetSession("/api/me/tokens", c), id2)["label"]; got != strings.Repeat("ữ", 100) {
		t.Fatal(got)
	}
	if r := w.PostSession("/api/me/tokens", `{"kind":"ingest","label":"x"}`, c, ""); r.Status != 403 {
		t.Fatal(r.Status)
	}
	if r := w.PostSession(fmt.Sprintf("/api/me/tokens/%d/revoke", id), "", c, ""); r.Status != 403 {
		t.Fatal(r.Status)
	}
}

func TestLoginAndCiTokensCannotBeRevokedHere(t *testing.T) {
	env, w := newEnv(t)
	loginD := webtest.CliToken(t, env.Pool, "add", "token.d", "Dũng D")
	s := w.Session(loginD)
	var userID, loginID int64
	ctx := context.Background()
	if err := env.Pool.QueryRow(ctx, "SELECT id FROM users WHERE login = 'token.d'").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := env.Pool.QueryRow(ctx, "SELECT id FROM tokens WHERE user_id = $1 AND kind = 'login' ORDER BY id LIMIT 1", userID).Scan(&loginID); err != nil {
		t.Fatal(err)
	}
	svc := &auth.Service{Pool: env.Pool, Now: func() time.Time { return *env.Now }}
	ci, err := svc.CreateToken(ctx, env.Pool, userID, "ci", "pipe")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{loginID, ci.ID} {
		r := w.PostSession(fmt.Sprintf("/api/me/tokens/%d/revoke", id), "", s, s.CSRF)
		if r.Status != 400 || r.JSON(t)["error"] != "Không thu hồi được loại token này ở đây" {
			t.Fatal(r.Status, r.Body)
		}
		if !queryBool(t, env, "SELECT revoked_at IS NULL FROM tokens WHERE id = $1", id) {
			t.Fatal("bị thu hồi")
		}
	}
	if r := w.Login(loginD); r.Status != 200 {
		t.Fatal(r.Status)
	}
}

func TestAuditRowsForCreateRevokeHaveNoTokenAndAreAppendOnly(t *testing.T) {
	env, w := newEnv(t)
	loginC := webtest.CliToken(t, env.Pool, "add", "token.c", "Chi C")
	s := w.Session(loginC)
	id, token := w.CreateToken(s, "ingest", "audit-me")
	w.PostSession(fmt.Sprintf("/api/me/tokens/%d/revoke", id), "", s, s.CSRF)

	rows, err := env.Pool.Query(context.Background(), "SELECT login, ip, channel, action, detail::text FROM audit "+
		"WHERE action IN ('token.create','token.revoke') AND target = $1 ORDER BY id", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for rows.Next() {
		var login, ip, channel, action, detail *string
		if err := rows.Scan(&login, &ip, &channel, &action, &detail); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, *action)
		if *login != "token.c" || *channel != "web" || ip == nil || !strings.Contains(*detail, "ingest") ||
			!strings.Contains(*detail, fmt.Sprintf(`"tokenId": %d`, id)) {
			t.Fatal(*login, *channel, ip, *detail)
		}
	}
	rows.Close()
	if strings.Join(actions, ",") != "token.create,token.revoke" {
		t.Fatal(actions)
	}
	for _, a := range queryStrings(t, env, "SELECT audit::text FROM audit") {
		if strings.Contains(a, token) || strings.Contains(a, s.ID) || strings.Contains(a, loginC) {
			t.Fatal("audit lộ bí mật:", a)
		}
	}
	for _, sql := range []string{"UPDATE audit SET action = 'x'", "DELETE FROM audit"} {
		if _, err := env.Pool.Exec(context.Background(), sql); err == nil || !strings.Contains(err.Error(), "audit chỉ được thêm") {
			t.Fatal(sql, err)
		}
	}
}

func TestAuditHelper(t *testing.T) {
	env, _ := newEnv(t)
	svc := &auth.Service{Pool: env.Pool, Now: func() time.Time { return *env.Now }}
	ctx := context.Background()
	if err := svc.Audit(ctx, "an", "x.do", `{"a":1}`); err != nil {
		t.Fatal(err)
	}
	if err := svc.Audit(ctx, "", "x.none", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.WriteAudit(ctx, env.Pool, auth.AuditEntry{Channel: "auto", Action: "gate.auto-approve", Repo: "r", Feature: "F-1",
		Target: "BRD", Detail: map[string]any{"risk": "low"}}); err != nil {
		t.Fatal(err)
	}
	got := queryStrings(t, env, "SELECT coalesce(login,'-')||'|'||channel||'|'||action||'|'||coalesce(repo_id,'-')||'|'||"+
		"coalesce(feature_key,'-')||'|'||coalesce(target,'-')||'|'||detail::text||'|'||(at = '2026-11-05T10:00:00Z') FROM audit ORDER BY id")
	want := []string{"an|web|x.do|-|-|-|{\"a\": 1}|true", "-|web|x.none|-|-|-|{}|true", "-|auto|gate.auto-approve|r|F-1|BRD|{\"risk\": \"low\"}|true"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%q", got)
	}
}
