package webclient

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"aisdlc/internal/snapshot"
)

// ErrKeyChanged: khóa server đổi mà không được khóa đang ghim ký xác nhận.
var ErrKeyChanged = errors.New("Khóa server đã đổi mà không được khóa cũ ký")

// PinnedKeyFile là file khóa công khai đã ghim của server web: <home>/.config/aisdlc/web-pubkey (base64 X.509 và
// xuống dòng, quyền 0600).
func PinnedKeyFile(home string) string { return filepath.Join(ConfigDir(home), "web-pubkey") }

// LoadPinnedKey đọc khóa đã ghim; nil khi chưa ghim hoặc file hỏng. Không bao giờ lỗi.
func LoadPinnedKey(home string) ed25519.PublicKey {
	b, err := os.ReadFile(PinnedKeyFile(home))
	if err != nil {
		return nil
	}
	k, err := snapshot.PublicKeyFromBase64(string(b))
	if err != nil {
		return nil
	}
	return k
}

// Pin ghi key làm khóa đã ghim, không kiểm tra gì (dùng cho lần đầu hoặc sau khi AcceptKey đã duyệt).
func Pin(home string, key ed25519.PublicKey) error {
	return writePrivate(PinnedKeyFile(home), []byte(snapshot.EncodePublicKey(key)+"\n"))
}

// AcceptKey: chưa ghim thì ghim offered; trùng khóa đang ghim thì giữ (pinned=false); khác thì chỉ nhận khi
// endorsementB64 là chữ ký hợp lệ của khóa đang ghim trên bytes X.509 của khóa mới, không thì ErrKeyChanged và
// không ghi gì (PinnedKey.pinOrRotate của Java).
func AcceptKey(home string, offered ed25519.PublicKey, endorsementB64 string) (pinned bool, err error) {
	cur := LoadPinnedKey(home)
	if cur != nil && cur.Equal(offered) {
		return false, nil
	}
	if cur != nil && !endorsed(cur, offered, endorsementB64) {
		return false, ErrKeyChanged
	}
	if err := Pin(home, offered); err != nil {
		return false, err
	}
	return true, nil
}

func endorsed(old, next ed25519.PublicKey, endorsementB64 string) bool {
	e := strings.TrimSpace(endorsementB64)
	if e == "" {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(e)
	if err != nil {
		return false
	}
	der, err := base64.StdEncoding.DecodeString(snapshot.EncodePublicKey(next))
	if err != nil {
		return false
	}
	return ed25519.Verify(old, der, sig)
}
