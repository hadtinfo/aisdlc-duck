// Package signing là khóa ký Ed25519 của server web (port gates/SigningKeys.java). File khóa chứa hai khối PEM:
// PRIVATE KEY (PKCS#8) và PUBLIC KEY (X.509), cùng định dạng với bản Java.
package signing

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/httpx"
)

// Keys là khóa ký hiện tại và (nếu đang đổi khóa) khóa cũ.
type Keys struct {
	Current  ed25519.PrivateKey
	Previous ed25519.PrivateKey // nil nếu không đổi khóa

	pub         ed25519.PublicKey
	endorsement string
}

// Load nạp khóa từ file. Chưa có file (hoặc file rỗng) thì sinh khóa mới, ghi nguyên tử quyền 0600. File có nhưng hỏng
// thì trả lỗi tiếng Việt và không bao giờ ghi đè. prevFile rỗng = không đổi khóa; khác rỗng thì file đó phải có và
// hợp lệ (không tự sinh), và endorsement là chữ ký của khóa cũ trên bytes X.509 của khóa mới.
func Load(file, prevFile string) (*Keys, error) {
	priv, err := loadOrCreate(file)
	if err != nil {
		return nil, fmt.Errorf(httpx.OpLocale().T("Không nạp được khóa ký %s: %w"), file, err)
	}
	k := &Keys{Current: priv, pub: priv.Public().(ed25519.PublicKey)}
	if prevFile != "" {
		st, err := os.Stat(prevFile)
		if err != nil || !st.Mode().IsRegular() {
			return nil, fmt.Errorf(httpx.OpLocale().T("Không nạp được khóa ký %s: Không có file khóa ký cũ: %s"), file, prevFile)
		}
		prev, err := readKey(prevFile)
		if err != nil {
			return nil, fmt.Errorf(httpx.OpLocale().T("Không nạp được khóa ký cũ %s: %w"), prevFile, err)
		}
		k.Previous = prev
		k.endorsement = base64.StdEncoding.EncodeToString(ed25519.Sign(prev, derOf(k.pub)))
	}
	return k, nil
}

func derOf(pub ed25519.PublicKey) []byte {
	b, _ := x509.MarshalPKIXPublicKey(pub)
	return b
}

func loadOrCreate(file string) (ed25519.PrivateKey, error) {
	st, err := os.Stat(file)
	switch {
	case err == nil && st.Size() > 0:
		if st.Mode().Perm()&0o077 != 0 {
			log.Print(httpx.OpLocale().Tf("CẢNH BÁO: file khóa ký %s có quyền %v, rộng hơn 0600", file, st.Mode().Perm()))
		}
		return readKey(file)
	case err == nil || errors.Is(err, os.ErrNotExist):
		return create(file)
	default:
		return nil, err
	}
}

// create sinh cặp khóa và ghi nguyên tử như bản Java: file tạm 0600 cùng thư mục (ghi, Sync, đóng), rồi os.Link vào đích
// (hỏng nếu đích đã có, nên hai tiến trình khởi động đầu không ghi đè khóa của nhau: bên thua đọc khóa đã có). Đích tồn
// tại nhưng rỗng thì thay bằng os.Rename. File tạm luôn bị xóa.
func create(file string) (ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	pk, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	data := append(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}),
		pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: derOf(pub)})...)
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".signing-key-*.tmp") // CreateTemp tạo với quyền 0600
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if st, err := os.Stat(file); err == nil && st.Size() == 0 {
		if err := os.Rename(tmp, file); err != nil { // file rỗng không chứa khóa nào
			return nil, err
		}
		return priv, nil
	}
	if err := os.Link(tmp, file); err != nil {
		if errors.Is(err, os.ErrExist) {
			return readKey(file) // tiến trình khác vừa ghi khóa: dùng khóa đó
		}
		return nil, err
	}
	return priv, nil
}

func readKey(file string) (ed25519.PrivateKey, error) {
	text, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var privDER, pubDER []byte
	for rest := text; ; {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		switch b.Type {
		case "PRIVATE KEY":
			privDER = b.Bytes
		case "PUBLIC KEY":
			pubDER = b.Bytes
		}
	}
	if privDER == nil || pubDER == nil {
		return nil, errors.New(httpx.OpLocale().T("file khóa cần hai khối PEM PRIVATE KEY và PUBLIC KEY"))
	}
	pk, err := x509.ParsePKCS8PrivateKey(privDER)
	if err != nil {
		return nil, fmt.Errorf(httpx.OpLocale().T("PRIVATE KEY không hợp lệ: %v"), err)
	}
	priv, ok := pk.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New(httpx.OpLocale().T("PRIVATE KEY không phải Ed25519"))
	}
	pubAny, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		return nil, fmt.Errorf(httpx.OpLocale().T("PUBLIC KEY không hợp lệ: %v"), err)
	}
	pub, ok := pubAny.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New(httpx.OpLocale().T("PUBLIC KEY không phải Ed25519"))
	}
	if !bytes.Equal(priv.Public().(ed25519.PublicKey), pub) {
		return nil, errors.New(httpx.OpLocale().T("PRIVATE KEY và PUBLIC KEY không cùng một cặp"))
	}
	return priv, nil
}

// Public là khóa công khai hiện tại.
func (k *Keys) Public() ed25519.PublicKey { return k.pub }

// KeyID là 16 ký tự hex định danh khóa hiện tại.
func (k *Keys) KeyID() string { return snapshot.KeyID(k.pub) }

// Sign ký payload bằng khóa hiện tại.
func (k *Keys) Sign(payload []byte) snapshot.SignedEnvelope {
	return snapshot.Sign(payload, k.Current, k.KeyID())
}

// SignJSON serialize v thành JSON (không escape HTML, như Jackson) rồi ký.
func (k *Keys) SignJSON(v any) (snapshot.SignedEnvelope, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return snapshot.SignedEnvelope{}, fmt.Errorf(httpx.OpLocale().T("Không serialize được: %w"), err)
	}
	return k.Sign(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

// Endorsement là chữ ký (base64) của khóa cũ trên X.509 của khóa mới; rỗng khi không đổi khóa.
func (k *Keys) Endorsement() string { return k.endorsement }

type pubkeyResponse struct {
	PublicKey   string `json:"publicKey"`
	KeyID       string `json:"keyId"`
	Endorsement string `json:"endorsement,omitempty"`
}

// PubkeyResponse là body JSON của GET /api/cli/pubkey: publicKey, keyId và endorsement khi có khóa cũ.
func (k *Keys) PubkeyResponse() any {
	return pubkeyResponse{PublicKey: snapshot.EncodePublicKey(k.pub), KeyID: k.KeyID(), Endorsement: k.endorsement}
}
