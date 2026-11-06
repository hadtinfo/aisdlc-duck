package webclient

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"testing"

	"aisdlc/internal/snapshot"
)

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// endorse: chữ ký của khóa cũ trên bytes X.509 của khóa mới.
func endorse(t *testing.T, old ed25519.PrivateKey, next ed25519.PublicKey) string {
	t.Helper()
	der, err := base64.StdEncoding.DecodeString(snapshot.EncodePublicKey(next))
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(old, der))
}

func perm(t *testing.T, f string) os.FileMode {
	t.Helper()
	st, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

// port pinFirstTimeWith600
func TestPinFirstTimeWith600(t *testing.T) {
	h := t.TempDir()
	a, _ := newKey(t)
	pinned, err := AcceptKey(h, a, "")
	if err != nil || !pinned {
		t.Fatalf("pinned=%v err=%v", pinned, err)
	}
	b, _ := os.ReadFile(PinnedKeyFile(h))
	if string(b) != snapshot.EncodePublicKey(a)+"\n" {
		t.Errorf("nội dung file: %q", b)
	}
	if p := perm(t, PinnedKeyFile(h)); p != 0o600 {
		t.Errorf("quyền %o", p)
	}
	if k := LoadPinnedKey(h); !a.Equal(k) {
		t.Errorf("LoadPinnedKey = %v", k)
	}
	// cùng khóa: giữ, không ghim lại
	if pinned, err := AcceptKey(h, a, ""); err != nil || pinned {
		t.Errorf("cùng khóa: pinned=%v err=%v", pinned, err)
	}
}

// port otherKeyNeedsEndorsementByPinnedKey
func TestOtherKeyNeedsEndorsementByPinnedKey(t *testing.T) {
	h := t.TempDir()
	a, aPriv := newKey(t)
	b, _ := newKey(t)
	_, cPriv := newKey(t)
	if err := Pin(h, a); err != nil {
		t.Fatal(err)
	}
	for name, endorsement := range map[string]string{"không": "", "khóa lạ ký": endorse(t, cPriv, b), "rác": "!!!"} {
		pinned, err := AcceptKey(h, b, endorsement)
		if err == nil || pinned || err.Error() != "Khóa server đã đổi mà không được khóa cũ ký" {
			t.Errorf("%s: pinned=%v err=%v", name, pinned, err)
		}
		if k := LoadPinnedKey(h); !a.Equal(k) {
			t.Errorf("%s: khóa đã ghim bị đổi", name)
		}
	}
	pinned, err := AcceptKey(h, b, endorse(t, aPriv, b))
	if err != nil || !pinned {
		t.Fatalf("pinned=%v err=%v", pinned, err)
	}
	if k := LoadPinnedKey(h); !b.Equal(k) {
		t.Error("khóa mới chưa được ghim")
	}
	if p := perm(t, PinnedKeyFile(h)); p != 0o600 {
		t.Errorf("quyền %o", p)
	}
}

func TestLoadPinnedKeyCorruptIsNil(t *testing.T) {
	h := t.TempDir()
	if LoadPinnedKey(h) != nil {
		t.Error("chưa ghim phải nil")
	}
	if err := os.MkdirAll(ConfigDir(h), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PinnedKeyFile(h), []byte("hỏng"), 0o600); err != nil {
		t.Fatal(err)
	}
	if LoadPinnedKey(h) != nil {
		t.Error("file hỏng phải nil")
	}
}
