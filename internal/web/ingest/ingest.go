// Package ingest nhận snapshot và lô sự kiện từ CLI (port ingest/*.java).
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/store"
)

// MaxBytes là giới hạn thân request ingest.
const MaxBytes = 8 << 20

// RetentionDays là thời gian giữ sự kiện.
const RetentionDays = 180

// Service lưu snapshot mới nhất của mỗi tính năng và ghi lô sự kiện.
type Service struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
	// Changed được gọi sau khi commit, với mỗi (repo, tính năng) vừa đổi (nil-safe). Task 5 nối vào SSE.
	Changed func(repoID, feature string)
	// RiskChanged được gọi trong giao dịch khi mức rủi ro của tính năng đổi (AutoApprover.onRiskChanged); from rỗng = chưa biết.
	// Mọi truy vấn của nó phải đi qua tx, không dùng pool (tránh cạn pool khi đang giữ tx).
	// Lỗi trả về hủy cả lô. nil = không làm gì.
	RiskChanged func(ctx context.Context, tx pgx.Tx, repoID, feature, from, to string) error
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) changed(repo, feature string) {
	if s.Changed != nil {
		s.Changed(repo, feature)
	}
}

// Routes đăng ký POST /api/ingest/snapshot và /api/ingest/events (thân tối đa 8 MB, thay DefaultLimit của router gốc).
func Routes(r chi.Router, s *Service) {
	lim := r.With(httpx.LimitBody(MaxBytes))
	lim.Post("/api/ingest/snapshot", s.handleSnapshot)
	lim.Post("/api/ingest/events", s.handleEvents)
}

// readBody đọc tối đa 8 MB (quá thì *ApiError 413), giải JSON và chặn NUL.
func readBody(r *http.Request) (any, []byte, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var ae *httpx.ApiError
		if errors.As(err, &ae) {
			return nil, nil, ae
		}
		return nil, nil, badRequest(httpx.T("JSON không hợp lệ"))
	}
	tree, raw, err := parseTree(data)
	if err != nil {
		return nil, nil, err
	}
	if err := rejectNul(tree); err != nil {
		return nil, nil, err
	}
	return tree, raw, nil
}

func login(r *http.Request) string {
	p, _ := httpx.PrincipalFrom(r.Context())
	return p.Login
}

func (s *Service) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	tree, raw, err := readBody(r)
	if err == nil {
		var capturedAt time.Time
		if capturedAt, err = validateSnapshot(tree); err == nil {
			var accepted, skew, stale bool
			accepted, skew, stale, err = s.UpsertSnapshot(r.Context(), login(r), tree, raw, capturedAt)
			if err == nil {
				code := ""
				if stale {
					code = snapshot.ErrCodeStaleAttempt
				}
				httpx.WriteJSON(w, 200, struct {
					Accepted  bool   `json:"accepted"`
					ClockSkew bool   `json:"clockSkew"`
					Code      string `json:"code,omitempty"`
				}{accepted, skew, code})
				return
			}
		}
	}
	httpx.Fail(w, r, err)
}

func (s *Service) handleEvents(w http.ResponseWriter, r *http.Request) {
	tree, _, err := readBody(r)
	if err == nil {
		var n int
		if n, err = s.AppendEvents(r.Context(), login(r), tree); err == nil {
			httpx.WriteJSON(w, 200, struct {
				Appended int `json:"appended"`
			}{n})
			return
		}
	}
	httpx.Fail(w, r, err)
}

// LatestSnapshot trả snapshot mới nhất của tính năng và captured_at của nó; ok=false khi chưa có.
func (s *Service) LatestSnapshot(ctx context.Context, repoID, feature string) (json.RawMessage, time.Time, bool, error) {
	var snap []byte
	var at time.Time
	err := s.Pool.QueryRow(ctx, "SELECT snapshot, captured_at FROM features WHERE repo_id = $1 AND feature_key = $2",
		repoID, feature).Scan(&snap, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}
	return snap, at.UTC(), true, nil
}

// PurgeOld xóa sự kiện cũ hơn 180 ngày; trả số hàng đã xóa. (Java: @Scheduled 03:00 hằng ngày.)
func (s *Service) PurgeOld(ctx context.Context) (int64, error) {
	tag, err := s.Pool.Exec(ctx, "DELETE FROM events WHERE at < $1", s.now().AddDate(0, 0, -RetentionDays))
	return tag.RowsAffected(), err
}

// lockFeature là khóa giao dịch theo tính năng, cùng khóa với nộp/duyệt (GateRepository.lock).
func lockFeature(ctx context.Context, tx pgx.Tx, repo, feature string) error {
	return store.LockFeature(ctx, tx, repo, feature)
}
