package store_test

import (
	"net/url"

	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"aisdlc/internal/web"
	"aisdlc/internal/web/store"
	"aisdlc/internal/web/webtest"
)

// Schema cuối của Java V1..V6 (viết tay).
var want = map[string]string{
	"users":                "id login display_name email created_at role locale",
	"repos":                "id last_seen_at",
	"features":             "repo_id feature_key snapshot captured_at client_id clock_skew first_sender_login last_sender_login synced_at risk_level risk_policy_version attempt",
	"events":               "id repo_id feature_key type at step path out_of_scope sender_login detail received_at session_id",
	"tokens":               "id user_id kind label sha256 created_at last_used_at revoked_at",
	"sessions":             "id_sha256 user_id csrf created_at expires_at",
	"repo_settings":        "repo_id approvals set_by set_at",
	"submissions":          "id repo_id feature_key gate hash findings artifacts submitted_by submitted_at rewind_id",
	"approvals":            "id submission_id repo_id feature_key gate hash approver channel at evaluated_tree flags assumptions_seen original_approver original_at confirmed_at revoked_at revoked_by rule",
	"feedback":             "id repo_id feature_key gate kind by_login at comment rewind_id",
	"audit":                "id at login ip channel action repo_id feature_key target detail",
	"feature_control":      "repo_id feature_key paused paused_by paused_at",
	"instructions":         "id repo_id feature_key by_login at text source feedback_id delivered_at rewind_id",
	"pending_actions":      "id repo_id feature_key kind step path end_tree created_by created_at expires_at status resolved_at reason rewind_id",
	"feature_rewinds":      "id repo_id feature_key from_attempt to_attempt checkpoint by_login at snapshot request_id",
	"risk_policy_versions": "repo_id version body hash author reason source created_at",
}

func cols(s string) []string { c := strings.Fields(s); sort.Strings(c); return c }

func TestMigrateCreatesFinalSchema(t *testing.T) {
	env := webtest.New(t, func(d web.Deps) http.Handler { return web.NewHandler(d) })
	rows, err := env.Pool.Query(context.Background(),
		`SELECT table_name, column_name FROM information_schema.columns
		 WHERE table_schema='public' AND table_name <> 'goose_db_version'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string][]string{}
	for rows.Next() {
		var tb, c string
		if err := rows.Scan(&tb, &c); err != nil {
			t.Fatal(err)
		}
		got[tb] = append(got[tb], c)
	}
	for k := range got {
		sort.Strings(got[k])
	}
	exp := map[string][]string{}
	for k, v := range want {
		exp[k] = cols(v)
	}
	if !reflect.DeepEqual(got, exp) {
		t.Fatalf("schema khác Java V6:\n got=%v\nwant=%v", got, exp)
	}
	// Ràng buộc kênh duyệt cuối và audit chỉ thêm.
	ctx := context.Background()
	if _, err := env.Pool.Exec(ctx, `INSERT INTO audit(channel, action) VALUES ('web','x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Pool.Exec(ctx, `UPDATE audit SET action='y'`); err == nil || !strings.Contains(err.Error(), "audit chỉ được thêm") {
		t.Fatalf("audit phải chỉ thêm: %v", err)
	}
	if _, err := env.Pool.Exec(ctx, `TRUNCATE audit`); err == nil {
		t.Fatal("truncate audit phải bị chặn")
	}
	var def string
	if err := env.Pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='approvals_channel_check'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def, "claude-code") || !strings.Contains(def, "auto") {
		t.Fatalf("approvals_channel_check: %s", def)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	env := webtest.New(t, func(d web.Deps) http.Handler { return web.NewHandler(d) })
	if err := store.Migrate(context.Background(), env.Pool); err != nil {
		t.Fatalf("migrate lần 2: %v", err)
	}
}

// Nhiều bản server khởi động cùng lúc trên DB trắng: khóa phiên của goose làm chúng lần lượt migrate, không bản nào lỗi.
func TestMigrateConcurrentOnFreshDB(t *testing.T) {
	env := webtest.New(t, func(d web.Deps) http.Handler { return web.NewHandler(d) })
	ctx := context.Background()
	name := "t_conc_" + strings.ToLower(t.Name()[len(t.Name())-4:])
	if _, err := env.Pool.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = env.Pool.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") })
	u, err := url.Parse(env.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	const n = 4
	pools := make([]*pgxpool.Pool, n)
	for i := range pools {
		if pools[i], err = store.Open(ctx, u.String(), "", ""); err != nil {
			t.Fatal(err)
		}
		defer pools[i].Close()
	}
	errs := make(chan error, n)
	start := make(chan struct{})
	for _, p := range pools {
		go func() { <-start; errs <- store.Migrate(ctx, p) }()
	}
	close(start)
	for range pools {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	var cnt int
	if err := pools[0].QueryRow(ctx, "SELECT count(*) FROM users").Scan(&cnt); err != nil {
		t.Fatal(err)
	}
}
