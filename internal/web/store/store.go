// Package store mở kết nối Postgres và chạy migration của server web.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open mở pool tới url; user/password khác rỗng thì ghi đè phần trong URL.
func Open(ctx context.Context, url, user, password string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("AISDLC_DB_URL không hợp lệ: %w", err)
	}
	if user != "" {
		cfg.ConnConfig.User = user
	}
	if password != "" {
		cfg.ConnConfig.Password = password
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("không kết nối được Postgres: %w", err)
	}
	return pool, nil
}

// Migrate chạy goose Up với các migration nhúng.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, db, sub)
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		var pe *goose.PartialError
		if errors.As(err, &pe) && pe.Failed != nil {
			return fmt.Errorf("migration %s lỗi: %w", filepath.Base(pe.Failed.Source.Path), err)
		}
		return fmt.Errorf("migration lỗi: %w", err)
	}
	return nil
}

// Execer là phần của pgx.Tx/pgxpool.Pool dùng để chạy lệnh.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// LockFeature lấy khóa giao dịch (advisory xact lock) theo tính năng: ingest snapshot/sự kiện và nộp/duyệt/trả lại của
// cùng tính năng chạy tuần tự (GateRepository.lock). Chỉ có nghĩa trong một transaction.
func LockFeature(ctx context.Context, tx Execer, repo, feature string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1 || chr(10) || $2, 0))", repo, feature)
	return err
}
