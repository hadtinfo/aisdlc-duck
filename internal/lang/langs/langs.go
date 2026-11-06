// Package langs là registry adapter; tách riêng để tránh vòng import lang <-> golang.
package langs

import (
	"fmt"
	"strings"

	"aisdlc/internal/lang"
	"aisdlc/internal/lang/golang"
)

// For trả adapter theo tên ngôn ngữ.
func For(language string) (lang.Adapter, error) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "go":
		return golang.Adapter{}, nil
	}
	return nil, fmt.Errorf("Ngôn ngữ chưa hỗ trợ: %s (hiện có: go)", language)
}

// RuntimeFor trả runtime (build, test, tiến trình) theo tên ngôn ngữ.
func RuntimeFor(language string) (lang.Runtime, error) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "go":
		return golang.Runtime{}, nil
	}
	return nil, fmt.Errorf("Ngôn ngữ chưa hỗ trợ: %s (hiện có: go)", language)
}
