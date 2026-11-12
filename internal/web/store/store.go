// Package store mở kết nối Postgres và chạy migration của server web.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	neturl "net/url"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"aisdlc/internal/web/httpx"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DefaultUser là user DB mặc định khi cả URL lẫn AISDLC_DB_USER đều không đặt.
const DefaultUser = "aisdlc"

// poolConfig phân tích url và áp user/password ghi đè.
func poolConfig(url, user, password string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf(httpx.OpLocale().T("AISDLC_DB_URL không hợp lệ: %w"), err)
	}
	if user == "" {
		// Mặc định như application.yml của Java: URL không có user (và không có AISDLC_DB_USER) thì dùng "aisdlc".
		if u, perr := neturl.Parse(url); perr == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") && u.User.Username() == "" {
			user = DefaultUser
		}
	}
	if user != "" {
		cfg.ConnConfig.User = user
	}
	if password != "" {
		cfg.ConnConfig.Password = password
	}
	return cfg, nil
}

// Open mở pool tới url; user/password khác rỗng thì ghi đè phần trong URL (user mặc định: DefaultUser).
func Open(ctx context.Context, url, user, password string) (*pgxpool.Pool, error) {
	cfg, err := poolConfig(url, user, password)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf(httpx.OpLocale().T("không kết nối được Postgres: %w"), err)
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
	// Khóa phiên Postgres (advisory): nhiều bản server khởi động cùng lúc thì lần lượt migrate, không đua nhau.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, sub, goose.WithSessionLocker(locker))
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		var pe *goose.PartialError
		if errors.As(err, &pe) && pe.Failed != nil {
			return fmt.Errorf(httpx.OpLocale().T("migration %s lỗi: %w"), filepath.Base(pe.Failed.Source.Path), err)
		}
		return fmt.Errorf(httpx.OpLocale().T("migration lỗi: %w"), err)
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
