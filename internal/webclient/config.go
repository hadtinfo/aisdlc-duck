// Package webclient là web client của CLI: gọi HTTP có Bearer, cấu hình web của máy (~/.config/aisdlc), khóa công
// khai đã ghim của server. Không có gì ở đây ghi vào repo; outbox, cache và lệnh nằm ở các gói gọi nó.
package webclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"aisdlc/internal/fsx"
	"aisdlc/internal/i18n"
)

// Config là cấu hình web của máy: ~/.config/aisdlc/web.json (do `aisdlc web login` ghi, token ingest) nếu có; chỉ
// khi không có file đó mới dùng env AISDLC_WEB_URL + AISDLC_WEB_TOKEN (cách cấu hình cũ của 2a). ApproveToken đọc từ
// file approve-token cùng thư mục (rỗng nếu chưa lưu).
//
// Lý do web.json luôn thắng env: token đăng nhập (loại login) bị /api/cli/** từ chối và nằm trong môi trường mà
// agent đọc được; lệnh web login, web status, submit nhắc xóa biến này (EnvTokenWarning).
type Config struct {
	URL, Token, ApproveToken string
}

func envTokenText() string {
	return i18n.T("AISDLC_WEB_TOKEN đang đặt trong môi trường. Token đăng nhập" +
		" (login) không dùng cho CLI và agent đọc được biến này: xóa nó khỏi môi trường, dùng `aisdlc web login`" +
		" với token ingest.")
}

// ConfigDir là thư mục cấu hình web của dev: <home>/.config/aisdlc.
func ConfigDir(home string) string { return filepath.Join(home, ".config", "aisdlc") }

// ConfigFile là <home>/.config/aisdlc/web.json.
func ConfigFile(home string) string { return filepath.Join(ConfigDir(home), "web.json") }

// ApproveTokenFile là <home>/.config/aisdlc/approve-token.
func ApproveTokenFile(home string) string { return filepath.Join(ConfigDir(home), "approve-token") }

// LoadConfig đọc cấu hình web như WebConfig.load của Java; ok=false khi chưa cấu hình. env trả "" khi không đặt
// (giá trị chỉ có khoảng trắng coi như không đặt). Cảnh báo (đã có tiền tố "aisdlc web: ", không bao giờ chứa
// token) để người gọi in ra stderr.
func LoadConfig(home string, env func(string) string) (c Config, ok bool, warnings []string) {
	if home != "" {
		cfg, ok, warn := fromFile(home)
		if warn != "" {
			warnings = append(warnings, warn)
		}
		if ok {
			cfg.ApproveToken = LoadApproveToken(home)
			return cfg, true, warnings
		}
	}
	u, t := strings.TrimSpace(env("AISDLC_WEB_URL")), strings.TrimSpace(env("AISDLC_WEB_TOKEN"))
	if u != "" && t != "" {
		c = normalize(u, t)
		if home != "" {
			c.ApproveToken = LoadApproveToken(home)
		}
		return c, true, warnings
	}
	return Config{}, false, warnings
}

// EnvTokenWarning là dòng cảnh báo (đủ tiền tố) khi AISDLC_WEB_TOKEN đang đặt; set=false khi không đặt. Không bao
// giờ chứa giá trị token.
func EnvTokenWarning(env func(string) string) (line string, set bool) {
	if strings.TrimSpace(env("AISDLC_WEB_TOKEN")) == "" {
		return "", false
	}
	return "aisdlc web: " + envTokenText(), true
}

// SaveConfig ghi web.json {"url","token"} (0600, thư mục 0700) và approve-token nếu c.ApproveToken khác rỗng.
func SaveConfig(home string, c Config) error {
	b, err := json.Marshal(map[string]string{"url": c.URL, "token": c.Token})
	if err != nil {
		return err
	}
	if err := writePrivate(ConfigFile(home), append(b, '\n')); err != nil {
		return err
	}
	if c.ApproveToken != "" {
		return writePrivate(ApproveTokenFile(home), []byte(c.ApproveToken+"\n"))
	}
	return nil
}

// SaveApproveToken ghi token approve (0600, thư mục 0700).
func SaveApproveToken(home, token string) error {
	return writePrivate(ApproveTokenFile(home), []byte(strings.TrimSpace(token)+"\n"))
}

// LoadApproveToken đọc token approve đã lưu; "" khi chưa có hoặc không đọc được.
func LoadApproveToken(home string) string {
	f := ApproveTokenFile(home)
	if st, err := os.Stat(f); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// writePrivate ghi atomic với quyền 0600; thư mục cha mới tạo là 0700.
func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fsx.WriteAtomic(path, data, 0o600)
}

func fromFile(home string) (c Config, ok bool, warn string) {
	f := ConfigFile(home)
	if st, err := os.Stat(f); err != nil || !st.Mode().IsRegular() {
		return Config{}, false, ""
	}
	b, err := os.ReadFile(f)
	var n map[string]any
	if err == nil {
		err = json.Unmarshal(b, &n)
	}
	if err != nil {
		return Config{}, false, i18n.Tf("aisdlc web: không đọc được %s: %s", f, err.Error())
	}
	u, t := strings.TrimSpace(JSONText(n["url"])), strings.TrimSpace(JSONText(n["token"]))
	if u == "" || t == "" {
		return Config{}, false, ""
	}
	return normalize(u, t), true, ""
}

// JSONText như JsonNode.asText(""): chuỗi, số và bool thành chữ; còn lại rỗng.
func JSONText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func normalize(url, token string) Config {
	return Config{URL: strings.TrimRight(strings.TrimSpace(url), "/"), Token: strings.TrimSpace(token)}
}
