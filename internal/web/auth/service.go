package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/web/httpx"
)

// Service gom users, tokens, phiên và audit trên Postgres. Now là đồng hồ (UTC) cho mọi mốc thời gian ghi/so trong DB.
type Service struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

// DB là *pgxpool.Pool hoặc pgx.Tx.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Kinds là các loại token.
var Kinds = map[string]bool{"login": true, "ingest": true, "approve": true, "ci": true}

const sessionTTL = 12 * time.Hour

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

// SHA256 là hex chữ thường của SHA-256(UTF-8 của s) (TokenService.sha256).
func SHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// random32 là 32 byte ngẫu nhiên (crypto/rand), base64url không đệm: 43 ký tự.
func random32() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ---- users ----

// User là một dòng users.
type User struct {
	ID          int64
	Login       string
	DisplayName string
	Email       *string
	Role        string
}

// Reserved: login "auto" (mọi kiểu chữ) là người duyệt của bản server tự duyệt, không cấp cho user thật.
func Reserved(login string) bool { return strings.EqualFold(login, "auto") }

// errDuplicate là login trùng.
var errDuplicate = errors.New("login đã tồn tại")

func (s *Service) addUser(ctx context.Context, db DB, login, displayName string, email *string, role string) (int64, error) {
	if Reserved(login) {
		return 0, fmt.Errorf("Login %s dành cho bản tự duyệt", login)
	}
	var id int64
	err := db.QueryRow(ctx, "INSERT INTO users(login, display_name, email, role, created_at) VALUES ($1,$2,$3,$4,$5) RETURNING id",
		login, displayName, email, role, s.now()).Scan(&id)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return 0, errDuplicate
	}
	return id, err
}

const userCols = "id, login, display_name, email, role"

func scanUser(row pgx.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Login, &u.DisplayName, &u.Email, &u.Role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// UserByID trả user hoặc nil.
func (s *Service) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.Pool.QueryRow(ctx, "SELECT "+userCols+" FROM users WHERE id = $1", id))
}

func userByLogin(ctx context.Context, db DB, login string) (*User, error) {
	return scanUser(db.QueryRow(ctx, "SELECT "+userCols+" FROM users WHERE login = $1", login))
}

// ---- tokens ----

// CreatedToken là token vừa tạo; Token thô chỉ trả một lần.
type CreatedToken struct {
	ID    int64
	Token string
}

// TokenInfo là thông tin token không gồm token hay SHA-256.
type TokenInfo struct {
	ID         int64
	Kind       string
	Label      string
	CreatedAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// CreateToken sinh token mới cho user, chỉ lưu SHA-256.
func (s *Service) CreateToken(ctx context.Context, db DB, userID int64, kind, label string) (CreatedToken, error) {
	if !Kinds[kind] {
		return CreatedToken{}, fmt.Errorf("kind %q", kind)
	}
	tok := random32()
	var id int64
	err := db.QueryRow(ctx, "INSERT INTO tokens(user_id, kind, label, sha256, created_at) VALUES ($1,$2,$3,$4,$5) RETURNING id",
		userID, kind, label, SHA256(tok), s.now()).Scan(&id)
	if err != nil {
		return CreatedToken{}, err
	}
	return CreatedToken{ID: id, Token: tok}, nil
}

// authenticateToken: token chưa thu hồi → Principal (Via = loại token), cập nhật last_used_at tối đa mỗi phút một lần.
// Không có → nil, nil.
func (s *Service) authenticateToken(ctx context.Context, token string) (*httpx.Principal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}
	var p httpx.Principal
	err := s.Pool.QueryRow(ctx, "SELECT t.id, t.kind, u.id, u.login, u.role FROM tokens t JOIN users u ON u.id = t.user_id "+
		"WHERE t.sha256 = $1 AND t.revoked_at IS NULL", SHA256(token)).Scan(&p.TokenID, &p.Via, &p.UserID, &p.Login, &p.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	if _, err := s.Pool.Exec(ctx, "UPDATE tokens SET last_used_at = $2 WHERE id = $1 "+
		"AND (last_used_at IS NULL OR last_used_at < $3)", p.TokenID, now, now.Add(-time.Minute)); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListTokens trả token của user theo id.
func (s *Service) ListTokens(ctx context.Context, userID int64) ([]TokenInfo, error) {
	rows, err := s.Pool.Query(ctx, "SELECT id, kind, label, created_at, last_used_at, revoked_at FROM tokens WHERE user_id = $1 ORDER BY id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TokenInfo{}
	for rows.Next() {
		var t TokenInfo
		if err := rows.Scan(&t.ID, &t.Kind, &t.Label, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// revokeToken thu hồi token của chính user; nil khi token không thuộc user. Thu hồi lại là no-op.
func (s *Service) revokeToken(ctx context.Context, userID, tokenID int64) (*TokenInfo, error) {
	tag, err := s.Pool.Exec(ctx, "UPDATE tokens SET revoked_at = coalesce(revoked_at, $3) WHERE id = $1 AND user_id = $2",
		tokenID, userID, s.now())
	if err != nil || tag.RowsAffected() == 0 {
		return nil, err
	}
	return s.ownToken(ctx, userID, tokenID)
}

func (s *Service) ownToken(ctx context.Context, userID, tokenID int64) (*TokenInfo, error) {
	list, err := s.ListTokens(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == tokenID {
			return &list[i], nil
		}
	}
	return nil, nil
}

// ---- phiên ----

// createSession mở phiên 12 giờ: id và CSRF 32 byte ngẫu nhiên, DB chỉ lưu SHA-256 của id. Dọn phiên hết hạn trước.
func (s *Service) createSession(ctx context.Context, userID int64) (id, csrf string, err error) {
	now := s.now()
	if _, err := s.Pool.Exec(ctx, "DELETE FROM sessions WHERE expires_at <= $1", now); err != nil {
		return "", "", err
	}
	id, csrf = random32(), random32()
	_, err = s.Pool.Exec(ctx, "INSERT INTO sessions(id_sha256, user_id, csrf, created_at, expires_at) VALUES ($1,$2,$3,$4,$5)",
		SHA256(id), userID, csrf, now, now.Add(sessionTTL))
	return id, csrf, err
}

// findSession: phiên còn hạn → Principal(Via = "session"); hết hạn hay không có → nil.
func (s *Service) findSession(ctx context.Context, sessionID string) (*httpx.Principal, error) {
	if javaBlank(sessionID) {
		return nil, nil
	}
	p := httpx.Principal{Via: "session"}
	err := s.Pool.QueryRow(ctx, "SELECT u.id, u.login, u.role FROM sessions s JOIN users u ON u.id = s.user_id "+
		"WHERE s.id_sha256 = $1 AND s.expires_at > $2", SHA256(sessionID), s.now()).Scan(&p.UserID, &p.Login, &p.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// sessionCSRF trả CSRF của phiên còn hạn ("" nếu không có).
func (s *Service) sessionCSRF(ctx context.Context, sessionID string) (string, bool, error) {
	if javaBlank(sessionID) {
		return "", false, nil
	}
	var c string
	err := s.Pool.QueryRow(ctx, "SELECT csrf FROM sessions WHERE id_sha256 = $1 AND expires_at > $2", SHA256(sessionID), s.now()).Scan(&c)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return c, true, nil
}

func (s *Service) deleteSession(ctx context.Context, sessionID string) error {
	if javaBlank(sessionID) {
		return nil
	}
	_, err := s.Pool.Exec(ctx, "DELETE FROM sessions WHERE id_sha256 = $1", SHA256(sessionID))
	return err
}
