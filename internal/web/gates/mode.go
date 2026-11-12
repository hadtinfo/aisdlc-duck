// Package gates là phần cổng duyệt của server web: chế độ repo, API CLI (port gates/*.java).
package gates

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/signing"
)

// Chế độ duyệt theo repo.
const (
	ModeLocal = "local"
	ModeWeb   = "web"
)

// ModeService là chế độ duyệt theo repo (bảng repo_settings): thiếu bản ghi = local.
type ModeService struct {
	Pool *pgxpool.Pool
	Keys *signing.Keys
	Now  func() time.Time
	Auth *auth.Service // ghi audit
}

// Mode là một bản ghi chế độ; At là nil khi repo chưa có bản ghi.
type Mode struct {
	Repo, Mode, SetBy string
	At                *time.Time
}

func (m *ModeService) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func find(ctx context.Context, db querier, repo string) (*Mode, error) {
	var md Mode
	var at time.Time
	err := db.QueryRow(ctx, "SELECT repo_id, approvals, set_by, set_at FROM repo_settings WHERE repo_id = $1", repo).
		Scan(&md.Repo, &md.Mode, &md.SetBy, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	at = at.UTC()
	md.At = &at
	return &md, nil
}

// Find trả bản ghi chế độ, nil khi repo chưa có.
func (m *ModeService) Find(ctx context.Context, repo string) (*Mode, error) {
	return find(ctx, m.Pool, repo)
}

// Mode trả chế độ của repo và pinned (đã có bản ghi chưa); chưa ghim = local, false.
func (m *ModeService) Mode(ctx context.Context, repoID string) (string, bool, error) {
	md, err := m.Find(ctx, repoID)
	if err != nil || md == nil {
		return ModeLocal, false, err
	}
	return md.Mode, true, nil
}

// Report: CLI báo chế độ cấu hình. Repo chưa có bản ghi mà configured = web thì ghim web (audit mode.pin, kênh cli);
// đã có thì giữ nguyên (đổi web -> local chỉ lead làm ở trang /repos).
func (m *ModeService) Report(ctx context.Context, p httpx.Principal, ip, repo, configured string) (Mode, error) {
	if configured != ModeWeb {
		md, err := m.Find(ctx, repo)
		if err != nil {
			return Mode{}, err
		}
		if md == nil {
			return Mode{Repo: repo, Mode: ModeLocal}, nil
		}
		return *md, nil
	}
	var out Mode
	err := pgx.BeginFunc(ctx, m.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "INSERT INTO repo_settings(repo_id, approvals, set_by, set_at) VALUES ($1, 'web', $2, $3) "+
			"ON CONFLICT (repo_id) DO NOTHING", repo, p.Login, m.now())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			if err := m.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Login: p.Login, IP: ip, Channel: "cli", Action: "mode.pin",
				Repo: repo, Detail: map[string]any{"mode": ModeWeb}}); err != nil {
				return err
			}
		}
		md, err := find(ctx, tx, repo)
		if err != nil {
			return err
		}
		if md == nil {
			return errors.New(httpx.OpLocale().T("repo_settings biến mất sau khi ghim"))
		}
		out = *md
		return nil
	})
	return out, err
}

// RepoRow là một dòng của GET /api/repos.
type RepoRow struct {
	ID        string  `json:"id"`
	Approvals string  `json:"approvals"`
	SetBy     *string `json:"setBy"`
	SetAt     *string `json:"setAt"`
}

// List: mọi repo đã biết (có snapshot hoặc có bản ghi chế độ); chưa có bản ghi = local.
func (m *ModeService) List(ctx context.Context) ([]RepoRow, error) {
	rows, err := m.Pool.Query(ctx, "SELECT r.repo_id, coalesce(s.approvals, 'local'), s.set_by, s.set_at FROM "+
		"(SELECT repo_id FROM features UNION SELECT repo_id FROM repo_settings) r "+
		"LEFT JOIN repo_settings s ON s.repo_id = r.repo_id ORDER BY r.repo_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RepoRow{}
	for rows.Next() {
		var r RepoRow
		var at *time.Time
		if err := rows.Scan(&r.ID, &r.Approvals, &r.SetBy, &at); err != nil {
			return nil, err
		}
		if at != nil {
			s := httpx.Instant(*at)
			r.SetAt = &s
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Set: lead đổi chế độ ở trang /repos; audit mode.set (kênh web) kèm chế độ cũ/mới.
func (m *ModeService) Set(ctx context.Context, p httpx.Principal, ip, repo, approvals string) (Mode, error) {
	var out Mode
	err := pgx.BeginFunc(ctx, m.Pool, func(tx pgx.Tx) error {
		prev, err := find(ctx, tx, repo)
		if err != nil {
			return err
		}
		from := ModeLocal
		if prev != nil {
			from = prev.Mode
		}
		if _, err := tx.Exec(ctx, "INSERT INTO repo_settings(repo_id, approvals, set_by, set_at) VALUES ($1,$2,$3,$4) ON CONFLICT (repo_id) "+
			"DO UPDATE SET approvals = excluded.approvals, set_by = excluded.set_by, set_at = excluded.set_at", repo, approvals, p.Login, m.now()); err != nil {
			return err
		}
		if err := m.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Login: p.Login, IP: ip, Channel: "web", Action: "mode.set",
			Repo: repo, Detail: map[string]any{"from": from, "to": approvals}}); err != nil {
			return err
		}
		md, err := find(ctx, tx, repo)
		if err != nil {
			return err
		}
		out = *md
		return nil
	})
	return out, err
}
