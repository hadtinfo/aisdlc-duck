// Package templates nhúng các template markdown/yaml của AISDLC, mỗi locale một thư mục (vi/, en/).
package templates

import (
	"embed"

	"aisdlc/internal/i18n"
)

// FS chứa vi/ và en/, mỗi thư mục có brd.md, design.md, design.yaml, cases.full.yaml, cases.lite.yaml, config.yaml
// (init/setup) và CLAUDE.aisdlc.md, gitlab-ci.aisdlc.yml (setup).
//
//go:embed vi en
var FS embed.FS

// Read đọc template name của locale l; locale khác en dùng vi.
func Read(l i18n.Locale, name string) (string, error) {
	dir := "vi"
	if l == i18n.EN {
		dir = "en"
	}
	b, err := FS.ReadFile(dir + "/" + name)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
