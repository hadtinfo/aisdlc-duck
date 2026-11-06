package webclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

const wantEnvText = "AISDLC_WEB_TOKEN đang đặt trong môi trường. Token đăng nhập" +
	" (login) không dùng cho CLI và agent đọc được biến này: xóa nó khỏi môi trường, dùng `aisdlc web login`" +
	" với token ingest."

// port webJsonBeatsEnvToken
func TestLoadConfigWebJsonBeatsEnv(t *testing.T) {
	h := t.TempDir()
	if err := SaveConfig(h, Config{URL: "http://web.example/", Token: "ingest-tok"}); err != nil {
		t.Fatal(err)
	}
	c, ok, warns := LoadConfig(h, envOf(map[string]string{
		"AISDLC_WEB_URL": "http://127.0.0.1:1", "AISDLC_WEB_TOKEN": "login-tok-2a"}))
	if !ok || c.Token != "ingest-tok" || c.URL != "http://web.example" {
		t.Fatalf("c=%+v ok=%v", c, ok)
	}
	if len(warns) != 0 {
		t.Errorf("cảnh báo: %v", warns)
	}
}

// port envTokenOnlyStillUsedButWarned
func TestLoadConfigEnvOnlyStillUsedButWarned(t *testing.T) {
	h := t.TempDir()
	env := envOf(map[string]string{"AISDLC_WEB_URL": " http://w/// ", "AISDLC_WEB_TOKEN": " login-tok-2a "})
	c, ok, _ := LoadConfig(h, env)
	if !ok || c.Token != "login-tok-2a" || c.URL != "http://w" {
		t.Fatalf("c=%+v ok=%v", c, ok)
	}
	w, set := EnvTokenWarning(env)
	if !set || w != "aisdlc web: "+wantEnvText {
		t.Errorf("cảnh báo: %q %v", w, set)
	}
	if strings.Contains(w, "login-tok-2a") {
		t.Error("cảnh báo chứa token")
	}
	if _, set := EnvTokenWarning(envOf(nil)); set {
		t.Error("không đặt env mà vẫn cảnh báo")
	}
	if _, set := EnvTokenWarning(envOf(map[string]string{"AISDLC_WEB_TOKEN": "  "})); set {
		t.Error("env chỉ có khoảng trắng không tính là đặt")
	}
}

// Phần web login của envTokenOnlyStillUsedButWarned: cảnh báo không lộ token env lẫn token ingest.
func TestEnvTokenWarningNeverContainsEitherToken(t *testing.T) {
	w, _ := EnvTokenWarning(envOf(map[string]string{"AISDLC_WEB_TOKEN": "login-tok-2a"}))
	if strings.Contains(w, "login-tok-2a") || strings.Contains(w, "ingest-tok") {
		t.Error(w)
	}
}

func TestLoadConfigNothing(t *testing.T) {
	if _, ok, _ := LoadConfig(t.TempDir(), envOf(map[string]string{"AISDLC_WEB_URL": "http://w"})); ok {
		t.Error("thiếu token thì chưa cấu hình")
	}
}

func TestLoadConfigCorruptFileWarnsWithoutContent(t *testing.T) {
	h := t.TempDir()
	f := filepath.Join(ConfigDir(h), "web.json")
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(`{"url": "http://w", "token": "secret-tok" `), 0o600); err != nil {
		t.Fatal(err)
	}
	_, ok, warns := LoadConfig(h, envOf(nil))
	if ok || len(warns) != 1 || !strings.HasPrefix(warns[0], "aisdlc web: không đọc được "+f+": ") {
		t.Fatalf("ok=%v warns=%v", ok, warns)
	}
	if strings.Contains(warns[0], "secret-tok") {
		t.Error("cảnh báo lộ token")
	}
}

func TestSaveConfigIs0600AndRoundTripsApproveToken(t *testing.T) {
	h := t.TempDir()
	if err := SaveConfig(h, Config{URL: "http://w", Token: "t", ApproveToken: "ap"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(ConfigDir(h), "web.json"), filepath.Join(ConfigDir(h), "approve-token")} {
		if p := perm(t, f); p != 0o600 {
			t.Errorf("%s quyền %o", f, p)
		}
	}
	c, ok, _ := LoadConfig(h, envOf(nil))
	if !ok || c.ApproveToken != "ap" || c.Token != "t" {
		t.Errorf("c=%+v", c)
	}
}
