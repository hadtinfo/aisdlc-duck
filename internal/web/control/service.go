// Package control là điều khiển agent từ web (ControlService/ControlController/ControlRepository của Java): tạm dừng,
// chỉ dẫn, lệnh chờ áp dụng, và phong bì ký GET /api/cli/control cho CLI.
package control

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/step"
	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/board"
	"aisdlc/internal/web/gates"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/signing"
	"aisdlc/internal/web/store"
)

// Câu lỗi nghiệp vụ (ControlService.java), dịch theo locale của request khi ghi ra.
var (
	MsgNotAllowed     = httpx.T("Chỉ chủ tính năng hoặc lead được điều khiển agent")
	MsgBadInstruction = httpx.T("Chỉ dẫn phải từ 1 đến 2000 ký tự")
	MsgStepChanged    = httpx.T("Bước đã đổi kể từ lúc bạn xem; tải lại trang")
)

const (
	MaxInstruction = 2000
	actionTTL      = 7 * 24 * time.Hour
)

// Service là tạm dừng, chỉ dẫn, lệnh chờ áp dụng theo tính năng. Ghi: chủ tính năng hoặc lead, ở cả hai chế độ duyệt.
// Mỗi thao tác ghi một dòng audit (không chép nội dung chỉ dẫn) và báo Hub.
type Service struct {
	Pool *pgxpool.Pool
	Keys *signing.Keys
	Auth *auth.Service // audit trong tx
	Hub  interface{ Publish(repoID, feature string) }
	Now  func() time.Time
}

var _ board.ControlView = (*Service)(nil)

// now: UTC, cắt tới micro giây (độ chính xác của timestamptz).
func (s *Service) now() time.Time {
	t := time.Now()
	if s.Now != nil {
		t = s.Now()
	}
	return t.UTC().Truncate(time.Microsecond)
}

func (s *Service) publish(repo, feature string) {
	if s.Hub != nil {
		s.Hub.Publish(repo, feature)
	}
}

func bad(msg httpx.Text) error { return httpx.Err(http.StatusBadRequest, msg) }

// ---- view

type instructionJSON struct {
	ID          int64   `json:"id"`
	By          string  `json:"by"`
	At          string  `json:"at"`
	Text        string  `json:"text"`
	Source      string  `json:"source"`
	DeliveredAt *string `json:"deliveredAt"`
}

type actionJSON struct {
	ID         int64   `json:"id"`
	Kind       string  `json:"kind"`
	Step       string  `json:"step"`
	Path       *string `json:"path"`
	By         string  `json:"by"`
	At         string  `json:"at"`
	ExpiresAt  string  `json:"expiresAt"`
	Status     string  `json:"status"`
	ResolvedAt *string `json:"resolvedAt"`
	Reason     *string `json:"reason"`
}

// ViewJSON là trạng thái cho trang web (ControlService.view).
type ViewJSON struct {
	Paused       bool              `json:"paused"`
	PausedBy     *string           `json:"pausedBy"`
	PausedAt     *string           `json:"pausedAt"`
	Instructions []instructionJSON `json:"instructions"`
	Actions      []actionJSON      `json:"actions"`
}

func instantPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := httpx.Instant(*t)
	return &s
}

// View là GET /api/features/{repo}/{key}/control: 50 chỉ dẫn và 50 lệnh mới nhất, mới trước; lệnh PENDING quá hạn
// hiện EXPIRED.
func (s *Service) View(ctx context.Context, repo, feature string) (any, error) {
	return s.view(ctx, s.Pool, repo, feature)
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Service) view(ctx context.Context, d querier, repo, feature string) (ViewJSON, error) {
	paused, by, at, err := pauseOf(ctx, d, repo, feature)
	if err != nil {
		return ViewJSON{}, err
	}
	v := ViewJSON{Paused: paused, Instructions: []instructionJSON{}, Actions: []actionJSON{}}
	if paused {
		v.PausedBy = by
		v.PausedAt = instantPtr(at)
	}
	rows, err := d.Query(ctx, "SELECT id, by_login, at, text, source, delivered_at FROM instructions WHERE repo_id = $1 "+
		"AND feature_key = $2 AND (rewind_id IS NULL OR delivered_at IS NOT NULL) ORDER BY id DESC LIMIT 50", repo, feature)
	if err != nil {
		return ViewJSON{}, err
	}
	for rows.Next() {
		var i instructionJSON
		var at time.Time
		var del *time.Time
		if err := rows.Scan(&i.ID, &i.By, &at, &i.Text, &i.Source, &del); err != nil {
			rows.Close()
			return ViewJSON{}, err
		}
		i.At = httpx.Instant(at)
		i.DeliveredAt = instantPtr(del)
		if i.Source == "feedback" {
			i.Text = gates.LocalizeFeedback(i.Text, httpx.LocaleFrom(ctx))
		}
		v.Instructions = append(v.Instructions, i)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ViewJSON{}, err
	}
	rows, err = d.Query(ctx, "SELECT id, kind, step, path, created_by, created_at, expires_at, "+
		"CASE WHEN status = 'PENDING' AND expires_at <= $3 THEN 'EXPIRED' ELSE status END, resolved_at, reason "+
		"FROM pending_actions WHERE repo_id = $1 AND feature_key = $2 ORDER BY id DESC LIMIT 50", repo, feature, s.now())
	if err != nil {
		return ViewJSON{}, err
	}
	for rows.Next() {
		var a actionJSON
		var at, exp time.Time
		var res *time.Time
		if err := rows.Scan(&a.ID, &a.Kind, &a.Step, &a.Path, &a.By, &at, &exp, &a.Status, &res, &a.Reason); err != nil {
			rows.Close()
			return ViewJSON{}, err
		}
		a.At = httpx.Instant(at)
		a.ExpiresAt = httpx.Instant(exp)
		a.ResolvedAt = instantPtr(res)
		if a.Reason != nil { // lý do do CLI gửi lên, lưu nguyên văn: dịch được khi là câu trong catalog
			r := httpx.LocaleFrom(ctx).T(*a.Reason)
			a.Reason = &r
		}
		v.Actions = append(v.Actions, a)
	}
	rows.Close()
	return v, rows.Err()
}

func pauseOf(ctx context.Context, d querier, repo, feature string) (bool, *string, *time.Time, error) {
	var paused bool
	var by *string
	var at *time.Time
	err := d.QueryRow(ctx, "SELECT paused, paused_by, paused_at FROM feature_control WHERE repo_id = $1 AND feature_key = $2",
		repo, feature).Scan(&paused, &by, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil, nil
	}
	if at != nil {
		u := at.UTC()
		at = &u
	}
	return paused, by, at, err
}

// ---- ghi

// inTx chạy fn trong một tx đã giữ khóa của tính năng và đã kiểm quyền; fn nhận hàng tính năng. Mọi truy vấn trong tx
// đi qua tx (không dùng pool trong tx).
func (s *Service) inTx(ctx context.Context, p httpx.Principal, repo, feature string,
	fn func(tx pgx.Tx, snap any, now time.Time) error) error {
	emails, err := (&board.Reader{Pool: s.Pool}).EmailToLogin(ctx) // đọc trước tx: không giữ hai kết nối
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := store.LockFeature(ctx, tx, repo, feature); err != nil {
			return err
		}
		var raw, first string
		err := tx.QueryRow(ctx, "SELECT snapshot::text, first_sender_login FROM features WHERE repo_id = $1 AND feature_key = $2",
			repo, feature).Scan(&raw, &first)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.Err(http.StatusNotFound, httpx.T("Không tìm thấy tính năng"))
		}
		if err != nil {
			return err
		}
		tree, err := board.ParseTree([]byte(raw))
		if err != nil {
			return err
		}
		owner := board.Owner(tree, emails, &first)
		if !gates.IsOwnerOrLead(p, owner) {
			return httpx.Err(http.StatusForbidden, MsgNotAllowed)
		}
		return fn(tx, tree, s.now())
	})
}

// Pause bật/tắt tạm dừng. Bật khi đã bật thì giữ người/giờ cũ.
func (s *Service) Pause(ctx context.Context, p httpx.Principal, ip, repo, feature string, paused bool) (ViewJSON, error) {
	err := s.inTx(ctx, p, repo, feature, func(tx pgx.Tx, _ any, now time.Time) error {
		var err error
		if paused {
			_, err = tx.Exec(ctx, "INSERT INTO feature_control(repo_id, feature_key, paused, paused_by, paused_at) "+
				"VALUES ($1,$2,true,$3,$4) ON CONFLICT (repo_id, feature_key) DO UPDATE SET "+
				"paused_by = CASE WHEN feature_control.paused THEN feature_control.paused_by ELSE excluded.paused_by END, "+
				"paused_at = CASE WHEN feature_control.paused THEN feature_control.paused_at ELSE excluded.paused_at END, "+
				"paused = true", repo, feature, p.Login, now)
		} else {
			_, err = tx.Exec(ctx, "UPDATE feature_control SET paused = false, paused_by = NULL, paused_at = NULL "+
				"WHERE repo_id = $1 AND feature_key = $2", repo, feature)
		}
		if err != nil {
			return err
		}
		action := "control.resume"
		if paused {
			action = "control.pause"
		}
		return s.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Login: p.Login, IP: ip, Channel: "web", Action: action,
			Repo: repo, Feature: feature})
	})
	if err != nil {
		return ViewJSON{}, err
	}
	s.publish(repo, feature)
	return s.view(ctx, s.Pool, repo, feature)
}

func utf16Len(str string) int {
	n := 0
	for _, r := range str {
		n += utf16.RuneLen(r)
	}
	return n
}

// javaTrim như String.trim: bỏ ký tự <= U+0020 ở hai đầu.
func javaTrim(str string) string {
	return strings.TrimFunc(str, func(r rune) bool { return r <= ' ' })
}

// Instruct thêm một chỉ dẫn (nguồn web); audit chỉ ghi id và độ dài.
func (s *Service) Instruct(ctx context.Context, p httpx.Principal, ip, repo, feature, raw string) (ViewJSON, error) {
	text := javaTrim(raw)
	n := utf16Len(text)
	if n == 0 || n > MaxInstruction {
		return ViewJSON{}, bad(MsgBadInstruction)
	}
	err := s.inTx(ctx, p, repo, feature, func(tx pgx.Tx, _ any, now time.Time) error {
		var id int64
		if err := tx.QueryRow(ctx, "INSERT INTO instructions(repo_id, feature_key, by_login, at, text, source) "+
			"VALUES ($1,$2,$3,$4,$5,'web') RETURNING id", repo, feature, p.Login, now, text).Scan(&id); err != nil {
			return err
		}
		return s.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Login: p.Login, IP: ip, Channel: "web", Action: "control.instruction",
			Repo: repo, Feature: feature, Detail: map[string]any{"id": id, "length": n}})
	})
	if err != nil {
		return ViewJSON{}, err
	}
	s.publish(repo, feature)
	return s.view(ctx, s.Pool, repo, feature)
}

// Action thêm lệnh ACCEPT_STEP (kèm endTree trang đang hiện) hoặc ALLOW_SCOPE (kèm path). path nil = không có.
func (s *Service) Action(ctx context.Context, p httpx.Principal, ip, repo, feature, kind, stepID string, path, seenEndTree *string) (ViewJSON, error) {
	if kind != "ACCEPT_STEP" && kind != "ALLOW_SCOPE" {
		return ViewJSON{}, bad(httpx.Tf("Loại lệnh không hợp lệ: %s", kind))
	}
	accept := kind == "ACCEPT_STEP"
	if !accept && (path == nil || !step.ValidScopePath(*path)) {
		return ViewJSON{}, bad(httpx.T("Đường dẫn không hợp lệ: cần đường dẫn file cụ thể, tương đối trong repo " +
			"(không glob, '..', '\\', .aisdlc/), tối đa 500 ký tự"))
	}
	if accept && (seenEndTree == nil || strings.TrimSpace(*seenEndTree) == "") {
		return ViewJSON{}, bad(httpx.T("Thiếu endTree của bước đang xem; tải lại trang"))
	}
	err := s.inTx(ctx, p, repo, feature, func(tx pgx.Tx, tree any, now time.Time) error {
		st := findStep(tree, stepID)
		status := ""
		if st != nil {
			status, _ = st["status"].(string)
		}
		var endTree *string
		var storedPath *string
		if accept {
			if status != "AWAITING_DEV" {
				return httpx.Err(http.StatusConflict, httpx.Tf("Bước %s không ở trạng thái chờ accept (AWAITING_DEV)", stepID))
			}
			rec, _ := st["record"].(map[string]any)
			t, _ := rec["endTree"].(string)
			if t == "" {
				return httpx.Err(http.StatusConflict, httpx.Tf("Bước %s chưa có cây code kết thúc để accept", stepID))
			}
			// dev accept đúng cây code đã xem: agent làm lại bước sau khi trang được tải thì từ chối
			if t != *seenEndTree {
				return httpx.Err(http.StatusConflict, MsgStepChanged)
			}
			endTree = &t
		} else {
			if status != "IN_PROGRESS" && status != "AWAITING_DEV" {
				return httpx.Err(http.StatusConflict, httpx.Tf("Bước %s không ở trạng thái đang làm hoặc chờ accept (IN_PROGRESS|AWAITING_DEV)", stepID))
			}
			storedPath = path
		}
		var dup int64
		err := tx.QueryRow(ctx, "SELECT id FROM pending_actions WHERE repo_id = $1 AND feature_key = $2 AND kind = $3 AND step = $4 "+
			"AND path IS NOT DISTINCT FROM $5 AND status = 'PENDING' AND expires_at > $6 ORDER BY id LIMIT 1",
			repo, feature, kind, stepID, storedPath, now).Scan(&dup)
		if err == nil {
			return nil // bấm lại: giữ lệnh cũ
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var id int64
		if err := tx.QueryRow(ctx, "INSERT INTO pending_actions(repo_id, feature_key, kind, step, path, end_tree, created_by, "+
			"created_at, expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id",
			repo, feature, kind, stepID, storedPath, endTree, p.Login, now, now.Add(actionTTL)).Scan(&id); err != nil {
			return err
		}
		return s.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Login: p.Login, IP: ip, Channel: "web", Action: "control.action",
			Repo: repo, Feature: feature, Detail: map[string]any{"id": id, "kind": kind, "step": stepID, "path": storedPath}})
	})
	if err != nil {
		return ViewJSON{}, err
	}
	s.publish(repo, feature)
	return s.view(ctx, s.Pool, repo, feature)
}

func findStep(tree any, id string) map[string]any {
	m, _ := tree.(map[string]any)
	steps, _ := m["steps"].([]any)
	for _, e := range steps {
		if sm, ok := e.(map[string]any); ok {
			if sid, _ := sm["id"].(string); sid == id {
				return sm
			}
		}
	}
	return nil
}

// ForCLI là dữ liệu chưa giao cho CLI; tính năng chưa có thì rỗng (CLI xóa trạng thái cũ).
func (s *Service) ForCLI(ctx context.Context, repo, feature string) (snapshot.FeatureControl, error) {
	now := s.now()
	c := snapshot.FeatureControl{Repo: repo, Feature: feature, IssuedAt: now,
		Instructions: []snapshot.Instruction{}, Actions: []snapshot.Action{}}
	paused, by, at, err := pauseOf(ctx, s.Pool, repo, feature)
	if err != nil {
		return c, err
	}
	if paused {
		pa := snapshot.Pause{}
		if by != nil {
			pa.By = *by
		}
		if at != nil {
			pa.At = *at
		}
		c.Pause = &pa
	}
	rows, err := s.Pool.Query(ctx, "SELECT id, by_login, at, text FROM instructions WHERE repo_id = $1 AND feature_key = $2 "+
		"AND delivered_at IS NULL AND rewind_id IS NULL ORDER BY id LIMIT 100", repo, feature)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var i snapshot.Instruction
		if err := rows.Scan(&i.ID, &i.By, &i.At, &i.Text); err != nil {
			rows.Close()
			return c, err
		}
		i.At = i.At.UTC()
		c.Instructions = append(c.Instructions, i)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return c, err
	}
	rows, err = s.Pool.Query(ctx, "SELECT id, kind, step, path, end_tree, created_by, created_at, expires_at FROM pending_actions "+
		"WHERE repo_id = $1 AND feature_key = $2 AND status = 'PENDING' AND expires_at > $3 ORDER BY id LIMIT 100", repo, feature, now)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var a snapshot.Action
		var path, end *string
		var exp time.Time
		if err := rows.Scan(&a.ID, &a.Kind, &a.Step, &path, &end, &a.By, &a.At, &exp); err != nil {
			return c, err
		}
		if path != nil {
			a.Path = *path
		}
		if end != nil {
			a.EndTree = *end
		}
		a.At = a.At.UTC()
		exp = exp.UTC()
		a.ExpiresAt = &exp
		c.Actions = append(c.Actions, a)
	}
	return c, rows.Err()
}
