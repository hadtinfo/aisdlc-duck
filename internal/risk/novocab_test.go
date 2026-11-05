package risk

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Như NoDomainStringsTest của Java: chỉ quét mã nguồn chính (không quét *_test.go, vì test phải dùng
// từ vựng nghiệp vụ làm dữ liệu mẫu).
func TestNoBusinessVocabulary(t *testing.T) {
	domain := regexp.MustCompile(`(?i)hạch toán|bút toán|định khoản|\bledger_|\bfee_|accounting`)
	var hits []string
	scanned := 0
	for _, dir := range []string{"../../internal", "../../cmd"} {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			scanned++
			if domain.Match(b) {
				hits = append(hits, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if scanned == 0 {
		t.Fatal("không quét được file .go nào")
	}
	if len(hits) != 0 {
		t.Errorf("từ vựng nghiệp vụ trong mã nguồn: %v", hits)
	}
}
