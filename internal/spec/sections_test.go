package spec

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/i18n"
)

var viRune = regexp.MustCompile(`[à-ỹĐđ]`)

func TestSectionsByIDBothLanguages(t *testing.T) {
	en := "## Goal\nx\n## In scope\nx\n## Out of scope\nx\n## Business rules\nx\n## Acceptance criteria\nx\n"
	mixed := "## Goal\nx\n## Trong phạm vi\nx\n## Out of scope\nx\n## Quy tắc nghiệp vụ\nx\n## Acceptance criteria\nx\n"
	lower := "## goal\nx\n## in scope\nx\n## out of scope\nx\n## business rules\nx\n## acceptance criteria\nx\n"
	for name, md := range map[string]string{"en": en, "mixed": mixed, "lower": lower} {
		if f := ValidateBRD(md); len(f) != 0 {
			t.Errorf("%s: %v", name, f)
		}
	}
	got := SectionsByID("## Goal\na\n## Mục tiêu\nb\n")[SecGoal]
	if strings.TrimSpace(got) != "a\nb" {
		t.Errorf("concat = %q", got)
	}
}

func TestValidateBRDMessageFollowsLocale(t *testing.T) {
	t.Cleanup(func() { i18n.SetDefault(i18n.VI) })
	md := "## In scope\nx\n## Out of scope\nx\n## Business rules\nx\n## Acceptance criteria\nx\n"
	i18n.SetDefault(i18n.EN)
	f := ValidateBRD(md)
	if len(f) != 1 || !strings.Contains(f[0].Message, "Goal") || viRune.MatchString(f[0].Message) {
		t.Errorf("en: %v", f)
	}
	i18n.SetDefault(i18n.VI)
	f = ValidateBRD(md)
	if len(f) != 1 || f[0].Message != `BRD thiếu mục bắt buộc: "Mục tiêu".` {
		t.Errorf("vi: %v", f)
	}
}

func TestOpenQuestionsEnglish(t *testing.T) {
	got := OpenQuestions("## Open questions\n- [ ] Why?\n- [x] Done\n")
	if !reflect.DeepEqual(got, []string{"Why?"}) {
		t.Errorf("got %v", got)
	}
}

func TestSourceID(t *testing.T) {
	for in, want := range map[string]string{"Own choice": "own", "suy ra từ BRD": "inferred", "khác": ""} {
		if got := SourceID(in); got != want {
			t.Errorf("SourceID(%q) = %q, want %q", in, got, want)
		}
	}
}
