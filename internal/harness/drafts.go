package harness

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"aisdlc/internal/lang"
)

// PrepareDrafts chép các bản nháp trong draftsDir vào outDir (xóa bản chép của lần trước), bỏ bản có version
// (hoặc tên, với file không có version) đã nằm trong mainDir. ok=false khi không còn bản nháp nào.
func PrepareDrafts(draftsDir, mainDir, outDir string, a lang.Adapter) (bool, error) {
	fail := func(err error) (bool, error) {
		return false, fmt.Errorf("Không chuẩn bị được migration nháp: %w", err)
	}
	main := map[string]bool{}
	mainFiles, err := walkFiles(mainDir, ".sql", ".go")
	if err != nil {
		return fail(err)
	}
	for _, f := range mainFiles {
		main[draftKey(f, a)] = true
	}
	drafts, err := walkFiles(draftsDir, ".sql")
	if err != nil {
		return fail(err)
	}
	var keep []string
	for _, f := range drafts {
		if !main[draftKey(f, a)] {
			keep = append(keep, f)
		}
	}
	if err := os.RemoveAll(outDir); err != nil {
		return fail(err)
	}
	if len(keep) == 0 {
		return false, nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fail(err)
	}
	for _, f := range keep {
		b, err := os.ReadFile(f)
		if err != nil {
			return fail(err)
		}
		if err := os.WriteFile(filepath.Join(outDir, filepath.Base(f)), b, 0o644); err != nil {
			return fail(err)
		}
	}
	return true, nil
}

func draftKey(file string, a lang.Adapter) string {
	name := filepath.Base(file)
	if v, ok := a.MigrationVersion(name); ok {
		return fmt.Sprintf("v:%d", v)
	}
	return "f:" + name
}

// walkFiles liệt kê (sắp xếp) file thường có một trong các đuôi; thư mục không có thì rỗng.
func walkFiles(dir string, exts ...string) ([]string, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			for _, e := range exts {
				if strings.HasSuffix(d.Name(), e) {
					out = append(out, p)
					break
				}
			}
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}
