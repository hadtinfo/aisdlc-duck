package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aisdlc/internal/i18n"
)

var missing = []byte("<missing>")

// Content là nội dung một file theo đường dẫn tương đối; Data == nil nghĩa là file không có.
type Content struct {
	Path string
	Data []byte
}

// SHA256Contents hash theo thứ tự: path, 0x00, bytes (hoặc "<missing>"), 0x00.
func SHA256Contents(c []Content) string {
	h := sha256.New()
	for _, e := range c {
		h.Write([]byte(e.Path))
		h.Write([]byte{0})
		if e.Data == nil {
			h.Write(missing)
		} else {
			h.Write(e.Data)
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Contents đọc các file; đường dẫn tương đối với gốc repo (dấu "/"), file không có thì Data == nil.
func Contents(repoRoot string, files []string) ([]Content, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	out := make([]Content, 0, len(files))
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			return nil, err
		}
		rel := abs
		if r, err := filepath.Rel(root, abs); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			rel = r
		}
		var data []byte
		b, err := os.ReadFile(abs)
		switch {
		case err == nil:
			data = b
			if data == nil {
				data = []byte{}
			}
		case os.IsNotExist(err):
		default:
			return nil, fmt.Errorf(i18n.T("Không đọc được %s: %v"), f, err)
		}
		out = append(out, Content{Path: filepath.ToSlash(rel), Data: data})
	}
	return out, nil
}

// SHA256Files là hash của nội dung các file.
func SHA256Files(repoRoot string, files []string) (string, error) {
	c, err := Contents(repoRoot, files)
	if err != nil {
		return "", err
	}
	return SHA256Contents(c), nil
}
