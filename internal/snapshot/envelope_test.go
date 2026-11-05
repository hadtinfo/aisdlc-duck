package snapshot

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"testing"
)

func pair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSignThenVerifyReturnsSameBytes(t *testing.T) {
	pub, priv := pair(t)
	payload := []byte(`{"a":"ế"}`)
	se := Sign(payload, priv, KeyID(pub))
	dec, err := base64.StdEncoding.DecodeString(se.Payload)
	if err != nil || !bytes.Equal(dec, payload) {
		t.Fatalf("payload %v %v", dec, err)
	}
	if b, ok := se.Verify(pub); !ok || !bytes.Equal(b, payload) {
		t.Fatalf("verify %q %v", b, ok)
	}
}

func TestTamperedPayloadFails(t *testing.T) {
	pub, priv := pair(t)
	payload := []byte(`{"mode":"web"}`)
	se := Sign(payload, priv, "k")
	changed := bytes.Clone(payload)
	changed[3] ^= 1
	bad := SignedEnvelope{base64.StdEncoding.EncodeToString(changed), se.Sig, se.KeyID}
	if _, ok := bad.Verify(pub); ok {
		t.Fatal("phải thất bại")
	}
}

func TestOtherKeyFails(t *testing.T) {
	_, priv := pair(t)
	se := Sign([]byte{1, 2, 3}, priv, "k")
	other, _ := pair(t)
	if _, ok := se.Verify(other); ok {
		t.Fatal("phải thất bại")
	}
}

func TestBrokenBase64Fails(t *testing.T) {
	pub, priv := pair(t)
	se := Sign([]byte{1, 2, 3}, priv, "k")
	for _, bad := range []SignedEnvelope{
		{se.Payload, "không phải base64!", "k"},
		{"%%%", se.Sig, "k"},
		{},
	} {
		if _, ok := bad.Verify(pub); ok {
			t.Errorf("%v phải thất bại", bad)
		}
	}
	if _, ok := se.Verify(nil); ok {
		t.Error("khóa nil phải thất bại")
	}
	if _, ok := se.Verify(ed25519.PublicKey{1, 2}); ok {
		t.Error("khóa sai độ dài phải thất bại")
	}
}

func TestKeyIdIsStable16Hex(t *testing.T) {
	pub, _ := pair(t)
	id := KeyID(pub)
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(id) {
		t.Fatal(id)
	}
	back, err := PublicKeyFromBase64(EncodePublicKey(pub))
	if err != nil || KeyID(back) != id {
		t.Fatalf("%v", err)
	}
	other, _ := pair(t)
	if KeyID(other) == id {
		t.Fatal("khác khóa phải khác id")
	}
}

func TestPublicKeyRoundTripAndRejectsGarbage(t *testing.T) {
	pub, _ := pair(t)
	back, err := PublicKeyFromBase64(EncodePublicKey(pub))
	if err != nil || !bytes.Equal(back, pub) {
		t.Fatalf("%v", err)
	}
	for _, g := range []string{"abc", "!!", "", base64.StdEncoding.EncodeToString([]byte("not der"))} {
		if _, err := PublicKeyFromBase64(g); err == nil {
			t.Errorf("%q phải lỗi", g)
		}
	}
}

func TestEnvelopeJSONFieldNames(t *testing.T) {
	_, priv := pair(t)
	b, _ := json.Marshal(Sign([]byte("x"), priv, "k"))
	var m map[string]string
	_ = json.Unmarshal(b, &m)
	if len(m) != 3 || m["payload"] == "" || m["sig"] == "" || m["keyId"] != "k" {
		t.Fatalf("%s", b)
	}
}

// Phong bì do SignedEnvelope.java ký (jshell, classpath aisdlc-core-0.1.0-SNAPSHOT.jar).
func TestEnvelopeJavaInterop(t *testing.T) {
	const (
		pubB64   = "MCowBQYDK2VwAyEAs1+CkW5Y3PjoqRsNfSARF7UzVwfJjHEEJ+6CPCaIkPM="
		javaKey  = "fe4e348f427b6393"
		javaJSON = `{"payload":"eyJhIjoxfQ==","sig":"i7VXBtH3/7eU5/LV7LPZwTMWHtDdv3XNJuPrfoEutSkni4B8PdAe7B/OgcyZk3435N2FB01poEWOzbjCR/MGCQ==","keyId":"fe4e348f427b6393"}`
	)
	pub, err := PublicKeyFromBase64(pubB64)
	if err != nil {
		t.Fatal(err)
	}
	if got := KeyID(pub); got != javaKey {
		t.Fatalf("keyId = %s, Java = %s", got, javaKey)
	}
	if EncodePublicKey(pub) != pubB64 {
		t.Error("EncodePublicKey không khớp Java")
	}
	var se SignedEnvelope
	if err := json.Unmarshal([]byte(javaJSON), &se); err != nil {
		t.Fatal(err)
	}
	if se.KeyID != javaKey {
		t.Errorf("keyId trường = %s", se.KeyID)
	}
	b, ok := se.Verify(pub)
	if !ok || string(b) != `{"a":1}` {
		t.Fatalf("verify %q %v", b, ok)
	}
}
