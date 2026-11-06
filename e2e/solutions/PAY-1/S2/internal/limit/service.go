// Package limit kiểm tra hạn mức chuyển tiền theo ngày.
package limit

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Result là kết quả kiểm tra: allowed và phần hạn mức còn lại.
type Result struct {
	Allowed   bool  `json:"allowed"`
	Remaining int64 `json:"remaining"`
}

// Service đọc hạn mức từ bảng daily_limit.
type Service struct{ db *pgxpool.Pool }

// NewService tạo Service trên pool Postgres.
func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// Check trả found=false khi tài khoản chưa có hạn mức. Chỉ kiểm tra, không cộng số đã dùng.
func (s *Service) Check(ctx context.Context, accountID string, amount int64) (res Result, found bool, err error) {
	var left int64
	err = s.db.QueryRow(ctx, "SELECT limit_amount - used_amount FROM daily_limit WHERE account_id = $1", accountID).
		Scan(&left)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if amount <= left {
		return Result{Allowed: true, Remaining: left - amount}, true, nil
	}
	return Result{Allowed: false, Remaining: left}, true, nil
}
