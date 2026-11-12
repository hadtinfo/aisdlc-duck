package policy

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/i18n"
	"aisdlc/internal/risk"
	"aisdlc/internal/web/httpx"
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

// Lỗi kiểm tra có phần động ("JSON chính sách không hợp lệ: %s", khóa lạ...) mà localizeAll không dịch được nay ra
// tiếng Anh nhờ hàm …In của risk theo locale của request; vi giữ nguyên.
func TestValidateDynamicErrorsFollowRequestLocale(t *testing.T) {
	s := &Service{} // repo=nil và JSON hỏng: không chạm DB
	for _, js := range []string{`{`, `{"schema":2,"khoa_la":1}`} {
		out, err := s.Validate(httpx.WithLocale(context.Background(), i18n.EN), nil, js)
		if err != nil {
			t.Fatal(err)
		}
		errs := out["errors"].([]string)
		if len(errs) == 0 {
			t.Fatal("phải có lỗi", js)
		}
		for _, m := range errs {
			if viChar.MatchString(m) {
				t.Fatal("còn tiếng Việt:", m)
			}
		}
		vi, _ := s.Validate(httpx.WithLocale(context.Background(), i18n.VI), nil, js)
		if !viChar.MatchString(strings.Join(vi["errors"].([]string), " ")) {
			t.Fatal("vi phải giữ tiếng Việt", vi)
		}
	}
	_, _, err := migrateDraft(i18n.EN, genericWith(t, risk.PresetLatestVersion("generic")))
	var pe *risk.PolicyError
	if !errors.As(err, &pe) || viChar.MatchString(pe.Errors[0]) {
		t.Fatal(err)
	}
}
