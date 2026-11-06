package signing_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/signing"
)

func TestGeneratesKeyFileWith0600AndReloadsSameKey(t *testing.T) { // SigningKeysTest.generatesKeyFileWith600AndReloadsSameKey
	file := filepath.Join(t.TempDir(), "sub", "signing-key")
	a, err := signing.Load(file, "")
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(file)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", st.Mode().Perm())
	}
	if len(a.KeyID()) != 16 || a.KeyID() != snapshot.KeyID(a.Public()) || a.Endorsement() != "" {
		t.Fatalf("%q", a.KeyID())
	}
	b, err := signing.Load(file, "")
	if err != nil || b.KeyID() != a.KeyID() {
		t.Fatalf("%v", err)
	}
	env := b.Sign([]byte(`{"x":1}`))
	if env.KeyID != a.KeyID() {
		t.Fatal(env.KeyID)
	}
	got, ok := env.Verify(a.Public())
	if !ok || string(got) != `{"x":1}` {
		t.Fatalf("%v %s", ok, got)
	}
}

func TestEndorsementOfNewKeyVerifiesWithPreviousKey(t *testing.T) { // endorsementOfNewKeyVerifiesWithPreviousKey
	dir := t.TempDir()
	old, err := signing.Load(filepath.Join(dir, "old-key"), "")
	if err != nil {
		t.Fatal(err)
	}
	now, err := signing.Load(filepath.Join(dir, "new-key"), filepath.Join(dir, "old-key"))
	if err != nil {
		t.Fatal(err)
	}
	if now.KeyID() == old.KeyID() || now.Endorsement() == "" {
		t.Fatal("khóa phải mới và có endorsement")
	}
	sig, _ := base64.StdEncoding.DecodeString(now.Endorsement())
	der, _ := base64.StdEncoding.DecodeString(snapshot.EncodePublicKey(now.Public()))
	if !ed25519.Verify(old.Public(), der, sig) {
		t.Fatal("endorsement không xác minh được bằng khóa cũ")
	}
	// khóa cũ khai báo mà không có file: lỗi, không tự sinh
	missing := filepath.Join(dir, "khong-co")
	if _, err := signing.Load(filepath.Join(dir, "k3"), missing); err == nil {
		t.Fatal("phải lỗi")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("không được tự sinh khóa cũ")
	}
}

func TestLoadNeverOverwritesExistingKey(t *testing.T) { // createNeverOverwritesKeyWrittenByAnotherStart
	file := filepath.Join(t.TempDir(), "race", "signing-key")
	a, _ := signing.Load(file, "")
	before, _ := os.ReadFile(file)
	b, err := signing.Load(file, "")
	after, _ := os.ReadFile(file)
	if err != nil || string(before) != string(after) || a.KeyID() != b.KeyID() {
		t.Fatalf("%v", err)
	}
	if es, _ := os.ReadDir(filepath.Dir(file)); len(es) != 1 {
		t.Fatalf("còn file thừa: %v", es)
	}
}

func TestEmptyKeyFileIsReplaced(t *testing.T) { // newKeyFileIsWrittenAtomically...EmptyFileIsReplaced
	dir := filepath.Join(t.TempDir(), "empty")
	_ = os.MkdirAll(dir, 0o755)
	file := filepath.Join(dir, "signing-key")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	k, err := signing.Load(file, "")
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(file)
	if st.Size() == 0 || st.Mode().Perm() != 0o600 {
		t.Fatalf("%d %v", st.Size(), st.Mode().Perm())
	}
	k2, _ := signing.Load(file, "")
	if k2.KeyID() != k.KeyID() {
		t.Fatal("khóa đổi sau khi nạp lại")
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Fatalf("còn file thừa: %v", es)
	}
}

func TestSigningKeyFilePerm0600(t *testing.T) {
	file := filepath.Join(t.TempDir(), "k")
	if _, err := signing.Load(file, ""); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(file)
	if st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode().Perm())
	}
}

func TestSigningKeyCorruptFailsStartup(t *testing.T) {
	good := filepath.Join(t.TempDir(), "good")
	if _, err := signing.Load(good, ""); err != nil {
		t.Fatal(err)
	}
	goodText, _ := os.ReadFile(good)
	other := filepath.Join(t.TempDir(), "other")
	_, _ = signing.Load(other, "")
	otherText, _ := os.ReadFile(other)
	pubOf := func(s string) string { return s[strings.Index(s, "-----BEGIN PUBLIC KEY-----"):] }
	privOf := func(s string) string { return s[:strings.Index(s, "-----BEGIN PUBLIC KEY-----")] }
	cases := map[string]string{
		"rác":            "đây không phải PEM",
		"thiếu public":   privOf(string(goodText)),
		"thiếu private":  pubOf(string(goodText)),
		"cắt cụt":        string(goodText)[:len(goodText)/2],
		"không cùng cặp": privOf(string(goodText)) + pubOf(string(otherText)),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "signing-key")
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := signing.Load(file, "")
			if err == nil || !strings.Contains(err.Error(), "Không nạp được khóa ký") {
				t.Fatalf("err = %v", err)
			}
			if after, _ := os.ReadFile(file); string(after) != content {
				t.Fatal("file hỏng bị ghi đè")
			}
		})
	}
	// khóa cũ hỏng cũng từ chối
	prev := filepath.Join(t.TempDir(), "prev")
	_ = os.WriteFile(prev, []byte("hỏng"), 0o600)
	if _, err := signing.Load(filepath.Join(t.TempDir(), "n"), prev); err == nil {
		t.Fatal("phải lỗi")
	}
}

func TestSigningKeyUnwritableDirFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ghi được mọi nơi")
	}
	dir := t.TempDir()
	_ = os.Chmod(dir, 0o500)
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, err := signing.Load(filepath.Join(dir, "sub", "k"), ""); err == nil {
		t.Fatal("phải lỗi")
	}
}

func TestPemInteropWithJava(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("không có openssl")
	}
	dir := t.TempDir()
	privPEM := filepath.Join(dir, "p.pem")
	if out, err := exec.Command("openssl", "genpkey", "-algorithm", "ed25519", "-out", privPEM).CombinedOutput(); err != nil {
		t.Skipf("openssl không sinh được ed25519: %v %s", err, out)
	}
	pubPEM, err := exec.Command("openssl", "pkey", "-in", privPEM, "-pubout").Output()
	if err != nil {
		t.Fatal(err)
	}
	priv, _ := os.ReadFile(privPEM)
	file := filepath.Join(dir, "signing-key")
	// cùng bố cục với SigningKeys.java: PRIVATE KEY (PKCS#8) rồi PUBLIC KEY (X.509), base64 64 cột
	if err := os.WriteFile(file, append(priv, pubPEM...), 0o600); err != nil {
		t.Fatal(err)
	}
	k, err := signing.Load(file, "")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(pubPEM)),
		"-----BEGIN PUBLIC KEY-----"), "-----END PUBLIC KEY-----")), "")
	if got := snapshot.EncodePublicKey(k.Public()); got != want {
		t.Fatalf("%s != %s", got, want)
	}
	// và file Go sinh ra openssl đọc được
	gen := filepath.Join(dir, "go-key")
	if _, err := signing.Load(gen, ""); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("openssl", "pkey", "-in", gen, "-noout").CombinedOutput(); err != nil {
		t.Fatalf("openssl không đọc được khóa Go: %v %s", err, out)
	}
}

func TestPubkeyResponseShape(t *testing.T) {
	dir := t.TempDir()
	k, _ := signing.Load(filepath.Join(dir, "a"), "")
	r := k.PubkeyResponse()
	b, _ := jsonMarshal(r)
	if strings.Contains(b, "endorsement") || !strings.HasPrefix(b, `{"publicKey":"`) || !strings.Contains(b, `"keyId":"`+k.KeyID()+`"`) {
		t.Fatal(b)
	}
	k2, _ := signing.Load(filepath.Join(dir, "b"), filepath.Join(dir, "a"))
	b2, _ := jsonMarshal(k2.PubkeyResponse())
	if !strings.Contains(b2, `"endorsement":"`) {
		t.Fatal(b2)
	}
}

func jsonMarshal(v any) (string, error) { b, err := json.Marshal(v); return string(b), err }

func TestConcurrentFirstStartAllAgreeWithDisk(t *testing.T) {
	file := filepath.Join(t.TempDir(), "d", "signing-key")
	const n = 16
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			k, err := signing.Load(file, "")
			errs[i] = err
			if err == nil {
				ids[i] = k.KeyID()
			}
		}()
	}
	close(start)
	wg.Wait()
	disk, err := signing.Load(file, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := range ids {
		if errs[i] != nil || ids[i] != disk.KeyID() {
			t.Fatalf("goroutine %d: %v %s != %s", i, errs[i], ids[i], disk.KeyID())
		}
	}
	if es, _ := os.ReadDir(filepath.Dir(file)); len(es) != 1 {
		t.Fatalf("còn file thừa: %v", es)
	}
}
