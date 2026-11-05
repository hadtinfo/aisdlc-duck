// Package snapshot chứa phong bì ký Ed25519 và các cache đã ký của web.
package snapshot

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// SignedEnvelope là phong bì ký Ed25519: Payload = base64 của chính bytes được ký, Sig = base64 chữ ký trên các
// bytes đó. Bên nhận xác minh trên bytes nhận được rồi mới parse, nên không cần chuẩn hóa JSON.
type SignedEnvelope struct {
	Payload string `json:"payload"`
	Sig     string `json:"sig"`
	KeyID   string `json:"keyId"`
}

// Sign ký payload.
func Sign(payload []byte, key ed25519.PrivateKey, keyID string) SignedEnvelope {
	return SignedEnvelope{
		Payload: base64.StdEncoding.EncodeToString(payload),
		Sig:     base64.StdEncoding.EncodeToString(ed25519.Sign(key, payload)),
		KeyID:   keyID,
	}
}

// Verify trả bytes payload khi chữ ký hợp lệ với key; false khi sai chữ ký, thiếu trường hoặc base64 hỏng.
func (e SignedEnvelope) Verify(key ed25519.PublicKey) ([]byte, bool) {
	if len(key) != ed25519.PublicKeySize || e.Payload == "" || e.Sig == "" {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(e.Payload)
	if err != nil {
		return nil, false
	}
	sig, err := base64.StdEncoding.DecodeString(e.Sig)
	if err != nil || !ed25519.Verify(key, data, sig) {
		return nil, false
	}
	return data, true
}

func der(key ed25519.PublicKey) []byte {
	b, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil
	}
	return b
}

// KeyID là 16 ký tự hex đầu của SHA-256(dạng X.509 của khóa).
func KeyID(key ed25519.PublicKey) string {
	d := sha256.Sum256(der(key))
	return hex.EncodeToString(d[:])[:16]
}

// PublicKeyFromBase64 đọc khóa công khai Ed25519 từ base64 X.509.
func PublicKeyFromBase64(x509b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(x509b64))
	if err != nil {
		return nil, fmt.Errorf("Khóa công khai không hợp lệ: %v", err)
	}
	k, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("Khóa công khai không hợp lệ: %v", err)
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("Khóa công khai không hợp lệ: không phải Ed25519")
	}
	return pub, nil
}

// EncodePublicKey trả base64 dạng X.509 của khóa công khai.
func EncodePublicKey(key ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(der(key))
}
