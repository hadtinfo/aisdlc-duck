// Package webtest dựng môi trường test cho server web: DB Postgres mới + httptest.Server.
package webtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/harness"
	"aisdlc/internal/harness/harnesstest"
	"aisdlc/internal/web"
	"aisdlc/internal/web/store"
)

// Env là môi trường của một test.
type Env struct {
	Pool   *pgxpool.Pool
	Server *httptest.Server
	Now    *time.Time // đồng hồ giả, đổi được giữa các bước
}

var (
	once    sync.Once
	baseURL string
	baseErr error
)

// shared khởi động (lười) một container Postgres chung cho cả binary test. Container được Ryuk dọn khi tiến trình thoát.
func shared() (string, error) {
	once.Do(func() {
		pg, _, err := harness.StartPostgres(context.Background(), "postgres:16-alpine", "webtest-"+randHex(4))
		if err != nil {
			baseErr = err
			return
		}
		baseURL = pg.URL
	})
	return baseURL, baseErr
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// New tạo DB mới t_<rand>, migrate, mở pool, chạy server với handler do build trả về. Dọn dẹp qua t.Cleanup.
func New(t *testing.T, build func(web.Deps) http.Handler) *Env {
	t.Helper()
	harnesstest.RequireDocker(t)
	ctx := context.Background()
	base, err := shared()
	if err != nil {
		t.Fatalf("không khởi động được Postgres: %v", err)
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := "t_" + randHex(6)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := store.Open(ctx, u.String(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, err := pgx.Connect(context.Background(), base)
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	env := &Env{Pool: pool, Now: &now}
	h := build(web.Deps{Pool: pool, Now: func() time.Time { return *env.Now }})
	env.Server = httptest.NewServer(h)
	t.Cleanup(env.Server.Close) // chạy trước cleanup đóng pool (LIFO)
	return env
}
