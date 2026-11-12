package cardbuild

import (
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/review"
)

// viChars là chữ cái có dấu tiếng Việt (cùng bảng với test catalog của internal/i18n).
var viChars = regexp.MustCompile(`(?i)[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)

// Spec: thẻ tiếng Anh dựng từ fixture tiếng Anh không còn ký tự tiếng Việt (cờ, chữ trên thẻ, bản chữ terminal).
func TestEnglishCardHasNoVietnamese(t *testing.T) {
	i18n.SetDefault(i18n.EN)
	t.Cleanup(func() { i18n.SetDefault(i18n.VI) })
	f := setup(t, "PAY-9", "en1", model.FlowFull, true)
	for _, g := range []gate.Gate{gate.BRD, gate.DESIGN, gate.CASES, gate.REVIEW} {
		c := f.build(g)
		if c.Locale != "en" {
			t.Errorf("%s: locale = %q", g, c.Locale)
		}
		js := toJSON(t, c)
		if !strings.Contains(js, `"locale":"en"`) {
			t.Errorf("%s: JSON thiếu locale: %s", g, js)
		}
		if len(c.Flags) == 0 && g != gate.REVIEW {
			t.Errorf("%s: fixture phải sinh cờ để kiểm thông điệp", g)
		}
		text := review.RenderCard("PAY-9", c, 200, false, "")
		for what, s := range map[string]string{"JSON": js, "chữ": text} {
			if m := viChars.FindString(s); m != "" {
				t.Errorf("%s %s còn chữ tiếng Việt %q:\n%s", g, what, m, s)
			}
		}
	}
}

// Thẻ vi không có khóa locale: JSON giữ nguyên byte như trước khi có song ngữ.
func TestVietnameseCardHasNoLocaleKey(t *testing.T) {
	i18n.SetDefault(i18n.VI)
	c := pay1(t).build(gate.DESIGN)
	if js := toJSON(t, c); c.Locale != "" || strings.Contains(js, `"locale"`) {
		t.Errorf("locale = %q, JSON = %s", c.Locale, js)
	}
}
