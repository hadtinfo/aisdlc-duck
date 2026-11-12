package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"aisdlc/internal/i18n"
	"aisdlc/internal/yamlx"
)

// repoLocale chọn locale của lần chạy: trong repo có aisdlc/config.yaml thì ngôn ngữ chỉ lấy từ khóa locale của nó
// (thiếu, null hoặc rỗng là vi; giá trị sai là lỗi) và AISDLC_LOCALE không được xét; chỉ khi không có config mới
// lấy AISDLC_LOCALE rồi mới đến vi. YAML hỏng không làm lỗi bước này (lỗi cấu hình khác được nêu ở chỗ dùng
// cấu hình) và coi như config không có khóa.
func repoLocale(ctx *Context) (i18n.Locale, error) {
	if root, ok := FindRepo(ctx.cwd); ok {
		var n map[string]any
		file := filepath.Join(root, "aisdlc", "config.yaml")
		if _, err := os.Stat(file); err == nil {
			if yamlx.Read(file, &n) == nil {
				if v, ok := n["locale"]; ok && v != nil {
					s, isStr := v.(string)
					if !isStr {
						s = fmt.Sprint(v)
					}
					return i18n.Parse(s)
				}
			}
			return i18n.VI, nil
		}
	}
	return i18n.FromEnv(ctx.Env), nil
}
