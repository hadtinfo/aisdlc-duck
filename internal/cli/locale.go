package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"aisdlc/internal/i18n"
	"aisdlc/internal/yamlx"
)

// repoLocale chọn locale của lần chạy: trong repo thì đọc chỉ khóa locale của aisdlc/config.yaml (giá trị sai là
// lỗi); ngoài repo, hoặc config không có khóa, thì lấy AISDLC_LOCALE rồi mới đến vi. YAML hỏng không làm lỗi bước
// này (lỗi cấu hình khác được nêu ở chỗ dùng cấu hình).
func repoLocale(ctx *Context) (i18n.Locale, error) {
	if root, ok := FindRepo(ctx.cwd); ok {
		var n map[string]any
		file := filepath.Join(root, "aisdlc", "config.yaml")
		if _, err := os.Stat(file); err == nil && yamlx.Read(file, &n) == nil {
			if v, ok := n["locale"]; ok && v != nil {
				s, isStr := v.(string)
				if !isStr {
					s = fmt.Sprint(v)
				}
				return i18n.Parse(s)
			}
		}
	}
	return i18n.FromEnv(ctx.Env), nil
}
