package policy

import (
	"regexp"
	"testing"

	"aisdlc/internal/i18n"
)

var viChar = regexp.MustCompile(`(?i)[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)

func TestConflictAndPolicyErrorsEnglish(t *testing.T) {
	c := &Conflict{Latest: 3}
	if c.Error() != "Đã có phiên bản mới hơn (v3); tải lại để xem." || c.MessageIn(i18n.EN) != "A newer version exists (v3); reload to see it." {
		t.Fatal(c.Error(), c.MessageIn(i18n.EN))
	}
	got := localizeAll(i18n.EN, []string{"Chính sách đã dùng schema 2, không cần chuyển", "lỗi ngoài catalog"})
	if viChar.MatchString(got[0]) || got[1] != "lỗi ngoài catalog" {
		t.Fatal(got)
	}
	if out := localizeAll(i18n.EN, nil); out == nil || len(out) != 0 {
		t.Fatal("luôn khác nil", out)
	}
	if i18n.EN.T(ImportReason) != "Imported from aisdlc/risk-policy.yaml" {
		t.Fatal(i18n.EN.T(ImportReason))
	}
}
