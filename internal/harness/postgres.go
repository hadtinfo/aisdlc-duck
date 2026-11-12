package harness

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"aisdlc/internal/i18n"
)

// Postgres là Postgres tạm thời của một lần chạy.
type Postgres struct {
	URL string // postgres://... sslmode=disable
}

// StartPostgres khởi động container gắn nhãn aisdlc.run=<runID>; stop dừng và xóa container.
func StartPostgres(ctx context.Context, image, runID string) (*Postgres, func(context.Context) error, error) {
	c, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("aisdlc"), tcpostgres.WithUsername("aisdlc"), tcpostgres.WithPassword("aisdlc"),
		testcontainers.WithLabels(map[string]string{"aisdlc.run": runID}),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(90*time.Second)))
	stop := func(ctx context.Context) error { return testcontainers.TerminateContainer(c) }
	if err != nil {
		_ = stop(context.WithoutCancel(ctx))
		return nil, nil, err
	}
	url, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = stop(context.WithoutCancel(ctx))
		return nil, nil, err
	}
	return &Postgres{URL: url}, stop, nil
}

// Migrate gộp các file .sql của dirs (dir thiếu bị bỏ qua; file cùng tên thì dir sau thắng) vào một dir tạm và
// chạy goose Up; cho phép version thấp hơn bản đã áp dụng.
func Migrate(ctx context.Context, dbURL string, dirs ...string) error {
	tmp, err := os.MkdirTemp("", "aisdlc-migrate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	n := 0
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(d, e.Name()))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(tmp, e.Name()), b, 0o644); err != nil {
				return err
			}
			n++
		}
	}
	if n == 0 {
		return nil
	}
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(tmp), goose.WithAllowOutofOrder(true))
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		var pe *goose.PartialError
		if errors.As(err, &pe) && pe.Failed != nil {
			return fmt.Errorf(i18n.T("migration %s lỗi: %w"), filepath.Base(pe.Failed.Source.Path), err)
		}
		return fmt.Errorf(i18n.T("migration lỗi: %w"), err)
	}
	return nil
}
