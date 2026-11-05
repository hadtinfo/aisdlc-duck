package snapshot

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"aisdlc/internal/model"
)

type cacheFixture struct {
	t    *testing.T
	tmp  string
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	key  model.FeatureKey
	file string
}

func newCacheFixture(t *testing.T) *cacheFixture {
	pub, priv := pair(t)
	tmp := t.TempDir()
	key := model.FeatureKey("PAY-123")
	f := ControlCacheFile(tmp, key)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	return &cacheFixture{t, tmp, pub, priv, key, f}
}

func (e *cacheFixture) store(c FeatureControl, priv ed25519.PrivateKey) {
	b, err := c.ToJSON()
	if err != nil {
		e.t.Fatal(err)
	}
	e.write(Sign(b, priv, "k"))
}

func (e *cacheFixture) write(se SignedEnvelope) {
	b, _ := json.Marshal(se)
	if err := os.WriteFile(e.file, b, 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func TestFileLocation(t *testing.T) {
	e := newCacheFixture(t)
	if e.file != filepath.Join(e.tmp, ".aisdlc/web/control/PAY-123.json") {
		t.Fatal(e.file)
	}
}

func TestValidEnvelopeReads(t *testing.T) {
	e := newCacheFixture(t)
	e.store(sampleControl(), e.priv)
	c, ok := ReadControlCache(e.file, e.pub, "repo1", e.key)
	if !ok || !reflect.DeepEqual(c, sampleControl()) {
		t.Fatalf("%v %+v", ok, c)
	}
}

func TestTamperedPayloadEmpty(t *testing.T) {
	e := newCacheFixture(t)
	e.store(sampleControl(), e.priv)
	raw, _ := os.ReadFile(e.file)
	var se SignedEnvelope
	_ = json.Unmarshal(raw, &se)
	p, _ := base64.StdEncoding.DecodeString(se.Payload)
	p[5] ^= 1
	e.write(SignedEnvelope{base64.StdEncoding.EncodeToString(p), se.Sig, se.KeyID})
	if _, ok := ReadControlCache(e.file, e.pub, "repo1", e.key); ok {
		t.Fatal("phải rỗng")
	}
}

func TestOtherKeyEmpty(t *testing.T) {
	e := newCacheFixture(t)
	e.store(sampleControl(), e.priv)
	other, _ := pair(t)
	if _, ok := ReadControlCache(e.file, other, "repo1", e.key); ok {
		t.Fatal("phải rỗng")
	}
}

func TestOtherRepoOrFeatureEmpty(t *testing.T) {
	e := newCacheFixture(t)
	e.store(sampleControl(), e.priv)
	if _, ok := ReadControlCache(e.file, e.pub, "repo2", e.key); ok {
		t.Error("sai repo")
	}
	if _, ok := ReadControlCache(e.file, e.pub, "repo1", "PAY-999"); ok {
		t.Error("sai feature")
	}
}

func TestMissingNullKeyOrGarbageEmpty(t *testing.T) {
	e := newCacheFixture(t)
	if _, ok := ReadControlCache(e.file, e.pub, "repo1", e.key); ok {
		t.Error("thiếu file")
	}
	e.store(sampleControl(), e.priv)
	if _, ok := ReadControlCache(e.file, nil, "repo1", e.key); ok {
		t.Error("khóa nil")
	}
	if err := os.WriteFile(e.file, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadControlCache(e.file, e.pub, "repo1", e.key); ok {
		t.Error("rác")
	}
}
