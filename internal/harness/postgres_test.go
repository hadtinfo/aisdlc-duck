package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"aisdlc/internal/harness/harnesstest"
)

const testImage = "postgres:16-alpine"

var (
	sharedOnce sync.Once
	sharedPG   *Postgres
	sharedStop func(context.Context) error
	sharedErr  error
)

// TestMain dùng chung một container Postgres cho cả package; chỉ khởi động khi có test cần (lười),
// nên AISDLC_SKIP_DOCKER=1 hay -run không đụng Docker thì không bao giờ gọi Docker.
func TestMain(m *testing.M) {
	code := m.Run()
	for _, stop := range []func(context.Context) error{sharedStop, kafkaStop} {
		if stop != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			_ = stop(ctx)
			cancel()
		}
	}
	os.Exit(code)
}

// freshDB trả URL của Postgres dùng chung với schema public rỗng.
func freshDB(t *testing.T) string {
	t.Helper()
	harnesstest.RequireDocker(t)
	sharedOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		sharedPG, sharedStop, sharedErr = StartPostgres(ctx, testImage, "harness-test")
	})
	if sharedErr != nil {
		t.Fatalf("không khởi động được Postgres: %v", sharedErr)
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, sharedPG.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	return sharedPG.URL
}

func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tableExists(t *testing.T, url, table string) bool {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var ok bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('public.'||$1) IS NOT NULL", table).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestStartPostgresLabelsContainer(t *testing.T) {
	url := freshDB(t)
	if !strings.HasPrefix(url, "postgres://") || !strings.Contains(url, "sslmode=disable") {
		t.Errorf("url=%q", url)
	}
}

func TestMigrateMainThenDrafts(t *testing.T) {
	url := freshDB(t)
	main, drafts := t.TempDir(), t.TempDir()
	writeIn(t, main, "00001_a.sql", "-- +goose Up\nCREATE TABLE a (id int);\n")
	writeIn(t, drafts, "00002_b.sql", "-- +goose Up\nCREATE TABLE b (id int);\n")
	if err := Migrate(context.Background(), url, main, drafts, filepath.Join(drafts, "khong-co")); err != nil {
		t.Fatal(err)
	}
	if !tableExists(t, url, "a") || !tableExists(t, url, "b") {
		t.Error("thiếu bảng a hoặc b")
	}
}

func TestMigrateFailureReturnsError(t *testing.T) {
	url := freshDB(t)
	dir := t.TempDir()
	writeIn(t, dir, "00001_hong.sql", "-- +goose Up\nCREATE TABEL oops (id int);\n")
	err := Migrate(context.Background(), url, dir)
	if err == nil || !strings.Contains(err.Error(), "00001_hong.sql") {
		t.Errorf("err=%v", err)
	}
}
