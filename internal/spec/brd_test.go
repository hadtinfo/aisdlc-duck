package spec

import (
	"reflect"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"aisdlc/templates"
)

const filledBRD = `# BRD
## Mục tiêu
Giới hạn chuyển tiền.
## Trong phạm vi
API kiểm tra hạn mức.
## Ngoài phạm vi
Giao diện web.
## Quy tắc nghiệp vụ
Tối đa 500 triệu mỗi ngày.
## Tiêu chí chấp nhận
Vượt hạn mức trả 422.
`

func tmplText(t *testing.T, name string) string {
	t.Helper()
	b, err := templates.FS.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func codesOf(f []Finding) []string {
	var out []string
	for _, x := range f {
		out = append(out, x.Code)
	}
	return out
}

func TestBrdTemplateUntouchedReportsAllSectionsEmpty(t *testing.T) {
	got := codesOf(ValidateBRD(tmplText(t, "brd.md")))
	want := []string{"BRD_EMPTY_SECTION", "BRD_EMPTY_SECTION", "BRD_EMPTY_SECTION", "BRD_EMPTY_SECTION", "BRD_EMPTY_SECTION"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestBrdFilledIsValid(t *testing.T) {
	if f := ValidateBRD(filledBRD); len(f) != 0 {
		t.Fatalf("%v", f)
	}
}

func TestBrdAcceptsNfdHeadingsAndTrailingSpaces(t *testing.T) {
	content := norm.NFD.String(strings.Replace(filledBRD, "## Mục tiêu", "## Mục tiêu   ", 1))
	if content == filledBRD {
		t.Fatal("phải khác bản gốc")
	}
	if f := ValidateBRD(content); len(f) != 0 {
		t.Fatalf("%v", f)
	}
}

func TestBrdNfdHeadingsMatch(t *testing.T) {
	content := strings.NewReplacer("## Mục tiêu", norm.NFD.String("## Mục tiêu"),
		"## Trong phạm vi", norm.NFD.String("## Trong phạm vi"),
		"## Tiêu chí chấp nhận", norm.NFD.String("## Tiêu chí chấp nhận")).Replace(filledBRD)
	for _, f := range ValidateBRD(content) {
		if f.Code == "BRD_MISSING_SECTION" {
			t.Fatalf("%v", f)
		}
	}
}

func TestBrdMissingSection(t *testing.T) {
	f := ValidateBRD(strings.Replace(filledBRD, "## Ngoài phạm vi\nGiao diện web.\n", "", 1))
	if len(f) != 1 || f[0].Code != "BRD_MISSING_SECTION" || !strings.Contains(f[0].Message, "Ngoài phạm vi") {
		t.Fatalf("%v", f)
	}
}

func TestBrdOpenQuestionBlocks(t *testing.T) {
	q := "Hạn mức tính theo ngày lịch hay 24h?"
	f := ValidateBRD(filledBRD + "## Câu hỏi mở\n- [ ] " + q + "\n")
	if len(f) != 1 || f[0].Code != "BRD_OPEN_QUESTION" || !strings.Contains(f[0].Message, q) {
		t.Fatalf("%v", f)
	}
	if f := ValidateBRD(filledBRD + "## Câu hỏi mở\n- [x] " + q + " → ngày lịch\n"); len(f) != 0 {
		t.Fatalf("%v", f)
	}
}

func TestCRLFMarkdown(t *testing.T) {
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }
	if f := ValidateBRD(crlf(filledBRD)); len(f) != 0 {
		t.Fatalf("brd: %v", f)
	}
	untouched := tmplText(t, "brd.md")
	if !reflect.DeepEqual(ValidateBRD(crlf(untouched)), ValidateBRD(untouched)) {
		t.Fatal("brd template CRLF khác LF")
	}
	d := spec2(t)
	if f := ValidateDesign(crlf(tmplText(t, "design.md")), d); len(f) != 0 {
		t.Fatalf("design: %v", f)
	}
	open := crlf(tmplText(t, "design.md") + "\n- [ ] Có cần?\n")
	if got := codesOf(ValidateDesign(open, d)); !reflect.DeepEqual(got, []string{"DESIGN_OPEN_QUESTION"}) {
		t.Fatalf("%v", got)
	}
}

func spec2(t *testing.T) DesignSpec {
	t.Helper()
	d, err := ReadDesign(tmplFile(t, "design.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
