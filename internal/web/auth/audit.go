package auth

import (
	"context"
	"encoding/json"
	"fmt"
)

// AuditEntry là một dòng audit (AuditLog.write). Login rỗng → NULL (thao tác quản trị/tự động không có người đăng nhập).
// Channel: web|terminal|cli|admin|auto. Không bao giờ đưa token, id phiên hay CSRF vào đây: chỉ id token.
type AuditEntry struct {
	Login, IP, Channel, Action, Repo, Feature, Target string
	Detail                                            map[string]any
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// WriteAudit ghi e qua db (pool hoặc tx). Bảng audit chỉ thêm (trigger chặn UPDATE/DELETE).
func (s *Service) WriteAudit(ctx context.Context, db DB, e AuditEntry) error {
	detail := e.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("audit detail: %w", err)
	}
	return s.insertAudit(ctx, db, e, string(b))
}

func (s *Service) insertAudit(ctx context.Context, db DB, e AuditEntry, detailJSON string) error {
	_, err := db.Exec(ctx, "INSERT INTO audit(at, login, ip, channel, action, repo_id, feature_key, target, detail) "+
		"VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)",
		s.now(), nullable(e.Login), nullable(e.IP), e.Channel, e.Action, nullable(e.Repo), nullable(e.Feature), nullable(e.Target), detailJSON)
	return err
}

// Audit ghi một dòng kênh "web" với detail là JSON object ("" → {}). Cần ip/kênh/repo/feature/target thì dùng WriteAudit.
func (s *Service) Audit(ctx context.Context, login, action, detail string) error {
	if detail == "" {
		detail = "{}"
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(detail), &m); err != nil || m == nil {
		return fmt.Errorf("audit detail phải là JSON object: %q", detail)
	}
	return s.insertAudit(ctx, s.Pool, AuditEntry{Login: login, Channel: "web", Action: action}, detail)
}
