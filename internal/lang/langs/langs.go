// Package langs là registry adapter; tách riêng để tránh vòng import lang <-> golang.
package langs

import (
	"fmt"

	"aisdlc/internal/lang"
	"aisdlc/internal/lang/golang"
)

// For trả adapter theo tên ngôn ngữ.
func For(language string) (lang.Adapter, error) {
	switch language {
	case "go":
		return golang.Adapter{}, nil
	}
	return nil, fmt.Errorf("Ngôn ngữ chưa hỗ trợ: %s (hiện có: go)", language)
}
