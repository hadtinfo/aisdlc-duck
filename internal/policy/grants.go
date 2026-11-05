package policy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aisdlc/internal/fsx"
	"aisdlc/internal/model"
)

// GrantTTL là hạn của phiếu kể từ lúc hook hỏi dev.
const GrantTTL = 60 * time.Second

// Grant là phiếu cho phép một lần (spec §3.2): hook PreToolUse ghi khi hỏi dev Yes/No cho một lệnh chỉ dev,
// CLI không có console tiêu thụ. Command là DevCommand.GrantKey; Hash là hash cổng (approve/reject/reopen),
// rỗng với lệnh khác (ghi ra file là null).
type Grant struct {
	SessionID string
	Feature   string
	Command   string
	Hash      string
	ExpiresAt time.Time
}

// grantJSON giữ tên trường và thứ tự như Jackson ghi; null khi rỗng.
type grantJSON struct {
	SessionID string  `json:"sessionId"`
	Feature   *string `json:"feature"`
	Command   *string `json:"command"`
	Hash      *string `json:"hash"`
	ExpiresAt string  `json:"expiresAt"`
}

func nstr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func sval(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// GrantFile là .aisdlc/grants/<session>.json (vùng agent không ghi được qua hook).
func GrantFile(root, sessionID string) string {
	return filepath.Join(root, ".aisdlc", "grants", model.SessionFileName(sessionID)+".json")
}

// IssueGrant ghi (thay) phiếu của session, atomic để người đọc không thấy file dở.
func IssueGrant(root string, g Grant) error {
	if g.SessionID == "" {
		return errors.New("Không ghi được phiếu cho phép: thiếu sessionId")
	}
	b, err := json.Marshal(grantJSON{g.SessionID, nstr(g.Feature), nstr(g.Command), nstr(g.Hash),
		g.ExpiresAt.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return fmt.Errorf("Không ghi được phiếu cho phép: %v", err)
	}
	if err := fsx.WriteAtomic(GrantFile(root, g.SessionID), b, 0o644); err != nil {
		return fmt.Errorf("Không ghi được phiếu cho phép: %v", err)
	}
	return nil
}

// ConsumeGrant: phiếu của session khớp đủ tính năng, lệnh, hash và còn hạn (now trước ExpiresAt) thì xóa và trả
// true. Mọi trường hợp khác trả false (phiếu không khớp vẫn bị xóa). Tiêu thụ trước hết "giành" file bằng một
// lần đổi tên nguyên tử, nên mỗi phiếu dùng được đúng một lần kể cả khi nhiều tiến trình cùng thử.
func ConsumeGrant(root, sessionID, feature, command, hash string, now time.Time) (bool, error) {
	if strings.TrimSpace(sessionID) == "" {
		return false, nil
	}
	f := GrantFile(root, sessionID)
	var rnd [16]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return false, err
	}
	claimed := f + "." + hex.EncodeToString(rnd[:]) + ".claimed"
	if err := os.Rename(f, claimed); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, nil // Java: mọi lỗi IO khi giành file đều coi là không có phiếu
	}
	defer os.Remove(claimed) // file đã giành là của riêng tiến trình này
	b, err := os.ReadFile(claimed)
	if err != nil {
		return false, nil
	}
	var j grantJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return false, nil
	}
	exp, err := time.Parse(time.RFC3339Nano, j.ExpiresAt)
	if err != nil {
		return false, nil
	}
	return j.SessionID == sessionID && sval(j.Feature) == feature && sval(j.Command) == command &&
		sval(j.Hash) == hash && now.Before(exp), nil
}

// ClearGrant xóa phiếu chưa dùng của session (mọi PreToolUse khác của session); im lặng khi lỗi.
func ClearGrant(root, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	_ = os.Remove(GrantFile(root, sessionID))
	return nil
}
