// Package fsx chứa tiện ích file dùng chung.
package fsx

import (
	"fmt"
	"os"
	"path/filepath"

	"aisdlc/internal/i18n"
)

// WriteAtomic ghi data vào path theo kiểu atomic: tạo thư mục cha, ghi file tạm cùng thư mục, fsync rồi rename.
// Người đọc luôn thấy nội dung cũ hoặc nội dung mới đầy đủ, không bao giờ thấy file ghi dở.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf(i18n.T("Không ghi được %s: %w"), path, err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf(i18n.T("Không ghi được %s: %w"), path, err)
	}
	tmp := f.Name()
	fail := func(err error) error {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf(i18n.T("Không ghi được %s: %w"), path, err)
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Chmod(perm); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf(i18n.T("Không ghi được %s: %w"), path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf(i18n.T("Không ghi được %s: %w"), path, err)
	}
	return nil
}
