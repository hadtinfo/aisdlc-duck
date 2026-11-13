package ingest

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"aisdlc/internal/web/httpx"
)

type stored struct {
	capturedAt time.Time
	clientID   string
	attempt    int
}

// UpsertSnapshot lưu snapshot đã validate theo thứ tự capturedAt (SnapshotStore.upsert). tree là cây đã giải,
// raw là JSON nguyên văn để lưu jsonb.
func (s *Service) UpsertSnapshot(ctx context.Context, login string, tree any, raw []byte, capturedAt time.Time) (accepted, skew, stale bool, err error) {
	repoID := asText(path(path(tree, "repo"), "id"))
	key := asText(path(path(tree, "feature"), "key"))
	clientID := asText(path(tree, "clientId"))

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, false, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	now := s.now()
	if _, err = tx.Exec(ctx, "INSERT INTO repos(id, last_seen_at) VALUES ($1,$2) ON CONFLICT (id) DO UPDATE SET last_seen_at = $2",
		repoID, now); err != nil {
		return false, false, false, err
	}
	st, err := lockRow(ctx, tx, repoID, key)
	if err != nil {
		return false, false, false, err
	}
	done := false
	// lần làm cũ (clone khác hoặc lượt gửi dở sau rewind): bỏ qua, không ghi đè hàng tính năng. Thiếu attempt = 1.
	if st != nil && attemptOf(tree) < st.attempt {
		return false, false, true, nil // tx.Rollback qua defer; không có gì để ghi
	}
	if st == nil {
		tag, err := tx.Exec(ctx, "INSERT INTO features(repo_id, feature_key, snapshot, captured_at, client_id, clock_skew, "+
			"first_sender_login, last_sender_login, synced_at) VALUES ($1,$2,$3::jsonb,$4,$5,false,$6,$6,$7) ON CONFLICT DO NOTHING",
			repoID, key, string(raw), capturedAt, clientID, login, now)
		if err != nil {
			return false, false, false, err
		}
		if tag.RowsAffected() == 1 {
			if err := s.replayRisk(ctx, tx, repoID, key); err != nil {
				return false, false, false, err
			}
			accepted, skew, done = true, false, true
		} else if st, err = lockRow(ctx, tx, repoID, key); err != nil { // thua cuộc đua chèn đầu tiên
			return false, false, false, err
		} else if st == nil {
			return false, false, false, errors.New(httpx.OpLocale().T("ingest: hàng tính năng biến mất"))
		}
	}
	if !done {
		older := capturedAt.Before(st.capturedAt)
		if older && st.clientID == clientID {
			accepted = false
		} else {
			skew = older // cũ hơn nhưng từ clone khác: nhận kèm cờ
			if _, err = tx.Exec(ctx, "UPDATE features SET snapshot = $1::jsonb, captured_at = $2, client_id = $3, clock_skew = $4, "+
				"last_sender_login = $5, synced_at = $6 WHERE repo_id = $7 AND feature_key = $8",
				string(raw), capturedAt, clientID, skew, login, now, repoID, key); err != nil {
				return false, false, false, err
			}
			accepted = true
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, false, false, err
	}
	if accepted {
		s.changed(repoID, key)
	}
	return accepted, skew, false, nil
}

func lockRow(ctx context.Context, tx pgx.Tx, repo, key string) (*stored, error) {
	var st stored
	err := tx.QueryRow(ctx, "SELECT captured_at, client_id, attempt FROM features WHERE repo_id = $1 AND feature_key = $2 FOR UPDATE",
		repo, key).Scan(&st.capturedAt, &st.clientID, &st.attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// replayRisk: sự kiện rủi ro đến trước snapshot đầu tiên thì áp lại theo thứ tự để cột trên hàng mới đúng.
func (s *Service) replayRisk(ctx context.Context, tx pgx.Tx, repo, key string) error {
	rows, err := tx.Query(ctx, "SELECT type, detail FROM events WHERE repo_id = $1 AND feature_key = $2 "+
		"AND type IN ('RISK_CLASSIFIED','RISK_OVERRIDDEN') AND detail IS NOT NULL ORDER BY at, id", repo, key)
	if err != nil {
		return err
	}
	type ev struct{ typ, detail string }
	var evs []ev
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.typ, &e.detail); err != nil {
			rows.Close()
			return err
		}
		evs = append(evs, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, e := range evs {
		d, _, perr := parseTree([]byte(e.detail))
		if perr != nil {
			continue // detail hỏng
		}
		if err := applyRisk(ctx, tx, repo, key, e.typ, d); err != nil {
			return err
		}
	}
	return nil
}
