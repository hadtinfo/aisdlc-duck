package board

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/web/httpx"
)

// Chế độ duyệt của repo (bảng repo_settings; thiếu = local). Trùng gates.ModeLocal/ModeWeb; không import gates để gates
// (Task 6) dùng được board.
const (
	modeLocal = "local"
	modeWeb   = "web"
)

// ActiveWindow: phiên ACTIVE khi SESSION_START/FILE_EDIT cuối cùng mới hơn khoảng này.
const ActiveWindow = 30 * time.Minute

// PendingImport là bản duyệt nhập từ local đang chờ chủ tính năng xác nhận.
type PendingImport struct {
	Gate             string  `json:"gate"`
	Hash             string  `json:"hash"`
	OriginalApprover *string `json:"originalApprover"`
	OriginalAt       *string `json:"originalAt"`
}

// GateView là phần dữ liệu cổng của server mà board cần cho repo chế độ web (GateOverlay, GateRepository,
// ResubmissionService của Java). Task 6 cài đặt; nil = không phủ, không có đường dẫn đã nộp/bản import, nộp lại n = 0.
type GateView interface {
	// Overlay phủ trạng thái cổng của server (và đánh dấu bản tự duyệt bị thay thế) lên snapshot.
	Overlay(ctx context.Context, repoID, key string, snap json.RawMessage, now time.Time) (json.RawMessage, error)
	SubmittedPaths(ctx context.Context, repoID, key string) (map[string][]string, error)
	PendingImports(ctx context.Context, repoID, key string) ([]PendingImport, error)
	// Resubmission là GET /resubmission?gate= cho repo chế độ web (gate đã kiểm hợp lệ).
	Resubmission(ctx context.Context, repoID, key, gate string) (any, error)
}

// ControlView là GET /api/features/{repo}/{key}/control (ControlService.view, Task 8). nil = 404.
type ControlView interface {
	View(ctx context.Context, repoID, key string) (any, error)
}

// Reader đọc features/events/users cho API đọc và SSE (FeatureReader.java). Repo chế độ web: snapshot trả ra đã qua
// Gates.Overlay, nên cột, nhãn, hộp thư tính trên trạng thái đó.
type Reader struct {
	Pool    *pgxpool.Pool
	Now     func() time.Time
	Gates   GateView
	Control ControlView
}

// Row là một tính năng; ApprovalsMode là local | web.
type Row struct {
	RepoID, Key   string
	Snapshot      json.RawMessage // JSON gọn
	Tree          any             // Snapshot đã giải
	ClockSkew     bool
	FirstSender   string
	LastSender    string
	SyncedAt      time.Time
	ApprovalsMode string
	RiskLevel     *string
}

func (r *Reader) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

const rowCols = "f.repo_id, f.feature_key, f.snapshot::text, f.clock_skew, f.first_sender_login, f.last_sender_login, " +
	"f.synced_at, f.risk_level, coalesce(s.approvals, 'local') FROM features f LEFT JOIN repo_settings s ON s.repo_id = f.repo_id"

// All trả mọi tính năng theo repo, key.
func (r *Reader) All(ctx context.Context) ([]Row, error) {
	rows, err := r.Pool.Query(ctx, "SELECT "+rowCols+" ORDER BY f.repo_id, f.feature_key")
	if err != nil {
		return nil, err
	}
	raw, err := pgx.CollectRows(rows, scanRow)
	if err != nil {
		return nil, err
	}
	out := make([]Row, 0, len(raw))
	for _, row := range raw {
		o, err := r.overlay(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// Find trả một tính năng; nil khi không có.
func (r *Reader) Find(ctx context.Context, repoID, key string) (*Row, error) {
	rows, err := r.Pool.Query(ctx, "SELECT "+rowCols+" WHERE f.repo_id = $1 AND f.feature_key = $2", repoID, key)
	if err != nil {
		return nil, err
	}
	row, err := pgx.CollectOneRow(rows, scanRow)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	o, err := r.overlay(ctx, row)
	return &o, err
}

func scanRow(rs pgx.CollectableRow) (Row, error) {
	var row Row
	var snap string
	if err := rs.Scan(&row.RepoID, &row.Key, &snap, &row.ClockSkew, &row.FirstSender, &row.LastSender, &row.SyncedAt,
		&row.RiskLevel, &row.ApprovalsMode); err != nil {
		return row, err
	}
	row.SyncedAt = row.SyncedAt.UTC()
	return row, row.setSnapshot([]byte(snap))
}

func (row *Row) setSnapshot(raw []byte) error {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return fmt.Errorf("snapshot %s/%s: %w", row.RepoID, row.Key, err)
	}
	tree, err := ParseTree(b.Bytes())
	if err != nil {
		return fmt.Errorf("snapshot %s/%s: %w", row.RepoID, row.Key, err)
	}
	row.Snapshot, row.Tree = b.Bytes(), tree
	return nil
}

// overlay: chế độ local giữ nguyên snapshot; chế độ web phủ trạng thái cổng của server (nếu có Gates).
func (r *Reader) overlay(ctx context.Context, row Row) (Row, error) {
	if row.ApprovalsMode != modeWeb || r.Gates == nil {
		return row, nil
	}
	snap, err := r.Gates.Overlay(ctx, row.RepoID, row.Key, row.Snapshot, r.now())
	if err != nil {
		return row, err
	}
	return row, row.setSnapshot(snap)
}

// Owner là chủ tính năng (luật chung cho duyệt cổng và điều khiển agent).
func (r *Reader) Owner(ctx context.Context, row Row) (*string, error) {
	m, err := r.EmailToLogin(ctx)
	if err != nil {
		return nil, err
	}
	first := row.FirstSender
	return Owner(row.Tree, m, &first), nil
}

// EmailToLogin: email (đã trim, chữ thường) → login.
func (r *Reader) EmailToLogin(ctx context.Context) (map[string]string, error) {
	rows, err := r.Pool.Query(ctx, "SELECT email, login FROM users WHERE email IS NOT NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var email, login string
		if err := rows.Scan(&email, &login); err != nil {
			return nil, err
		}
		m[strings.ToLower(javaTrim(email))] = login
	}
	return m, rows.Err()
}

// SessionKey là khóa của Sessions.
func SessionKey(repoID, key string) string { return repoID + "\n" + key }

// Sessions: SessionKey → ACTIVE | STOPPED; thiếu khóa nghĩa là NONE.
func (r *Reader) Sessions(ctx context.Context) (map[string]string, error) {
	cutoff := r.now().Add(-ActiveWindow)
	rows, err := r.Pool.Query(ctx, "SELECT repo_id, feature_key, "+
		"max(at) FILTER (WHERE type IN ('SESSION_START','FILE_EDIT')) AS last_up, "+
		"max(at) FILTER (WHERE type = 'SESSION_STOP') AS last_stop "+
		"FROM events WHERE feature_key IS NOT NULL AND type IN ('SESSION_START','FILE_EDIT','SESSION_STOP') "+
		"GROUP BY repo_id, feature_key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var repo, key string
		var up, stop *time.Time
		if err := rows.Scan(&repo, &key, &up, &stop); err != nil {
			return nil, err
		}
		active := up != nil && up.After(cutoff) && (stop == nil || !stop.After(*up))
		state := "STOPPED"
		if active {
			state = "ACTIVE"
		}
		m[SessionKey(repo, key)] = state
	}
	return m, rows.Err()
}

// Event là một dòng event trả cho UI (GET /events và SSE `event`).
type Event struct {
	ID         int64   `json:"id"`
	Type       string  `json:"type"`
	At         string  `json:"at"`
	Step       *string `json:"step"`
	Path       *string `json:"path"`
	OutOfScope bool    `json:"outOfScope"`
	Sender     *string `json:"sender"`
	Detail     *string `json:"detail"`
}

const eventCols = "SELECT id, type, at, step, path, out_of_scope, sender_login, detail FROM events "

func (r *Reader) events(ctx context.Context, sql string, args ...any) ([]Event, error) {
	rows, err := r.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(rs pgx.CollectableRow) (Event, error) {
		var e Event
		var at time.Time
		err := rs.Scan(&e.ID, &e.Type, &at, &e.Step, &e.Path, &e.OutOfScope, &e.Sender, &e.Detail)
		e.At = httpx.Instant(at)
		return e, err
	})
}

// Events: event mới nhất trước, tối đa limit.
func (r *Reader) Events(ctx context.Context, repoID, key string, limit int) ([]Event, error) {
	return r.events(ctx, eventCols+"WHERE repo_id = $1 AND feature_key = $2 ORDER BY at DESC, id DESC LIMIT $3", repoID, key, limit)
}

// EventsAfter: event có id lớn hơn afterID, cũ trước.
func (r *Reader) EventsAfter(ctx context.Context, repoID, key string, afterID int64) ([]Event, error) {
	return r.events(ctx, eventCols+"WHERE repo_id = $1 AND feature_key = $2 AND id > $3 ORDER BY id", repoID, key, afterID)
}

// MaxEventID là id event lớn nhất của tính năng (0 khi chưa có).
func (r *Reader) MaxEventID(ctx context.Context, repoID, key string) (int64, error) {
	var v int64
	err := r.Pool.QueryRow(ctx, "SELECT coalesce(max(id), 0) FROM events WHERE repo_id = $1 AND feature_key = $2", repoID, key).Scan(&v)
	return v, err
}

// SyncedAt là lúc đồng bộ cuối của tính năng; ok=false khi không có.
func (r *Reader) SyncedAt(ctx context.Context, repoID, key string) (time.Time, bool, error) {
	var t time.Time
	err := r.Pool.QueryRow(ctx, "SELECT synced_at FROM features WHERE repo_id = $1 AND feature_key = $2", repoID, key).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	return t.UTC(), err == nil, err
}
