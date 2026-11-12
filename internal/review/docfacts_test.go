package review

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"aisdlc/internal/spec"
)

const brdSample = `# BRD
## Quy tắc nghiệp vụ
- BR-1: Hạn mức 50.000.000 VND/ngày
- Không ID
## Tiêu chí chấp nhận
- a
- b
- c
## Ngoài phạm vi
- x
- y
## Giả định
- Số tiền BIGINT, đơn vị VND. (tự chọn)
- Ngày tính theo giờ VN.
## Câu hỏi mở
- [x] Có tính phí? → không
- [ ] Còn mở?
`

func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v", name, got, want)
	}
}

func TestBrdParsesRuleIdsAssumptionsAndCounts(t *testing.T) {
	d := ParseBRD(brdSample)
	eq(t, "rules", d.Rules, []Rule{{ID: "BR-1", Text: "Hạn mức 50.000.000 VND/ngày"}, {Text: "Không ID"}})
	eq(t, "acceptance", d.AcceptanceCriteria, 3)
	eq(t, "outOfScope", len(d.OutOfScope), 2)
	eq(t, "assumptions", d.Assumptions, []Assumption{
		{"Số tiền BIGINT, đơn vị VND.", "tự chọn"}, {"Ngày tính theo giờ VN.", ""}})
	eq(t, "section", d.AssumptionsSection, true)
	eq(t, "answered", d.AnsweredQuestions, []string{"Có tính phí? → không"})
}

func TestBrdWithoutAssumptionsSection(t *testing.T) {
	d := ParseBRD("## Quy tắc nghiệp vụ\n- BR-1: a\n")
	eq(t, "section", d.AssumptionsSection, false)
	eq(t, "assumptions", len(d.Assumptions), 0)
}

func TestBrdIgnoresHtmlComments(t *testing.T) {
	d := ParseBRD("## Tiêu chí chấp nhận\n<!--\n- ẩn\n-->\n- thật\n")
	eq(t, "acceptance", d.AcceptanceCriteria, 1)
}

func TestBrdEmptyInputDoesNotPanic(t *testing.T) {
	d := ParseBRD("")
	eq(t, "rules", len(d.Rules), 0)
	eq(t, "outOfScope", len(d.OutOfScope), 0)
	eq(t, "answered", len(d.AnsweredQuestions), 0)
	eq(t, "assumptions", len(d.Assumptions), 0)
	eq(t, "acceptance", d.AcceptanceCriteria, 0)
	eq(t, "apiLines", len(ParseDesign("").APILines), 0)
}

func TestDesignApiLinesAndStatuses(t *testing.T) {
	d := ParseDesign("## API\n- POST /api/v1/limits/check → 200, 400, 404\nGhi chú 400\n## DB\n- 500\n")
	eq(t, "apiLines", d.APILines, []string{"POST /api/v1/limits/check → 200, 400, 404"})
	eq(t, "statusCodes", d.StatusCodes, []string{"200", "400", "404"})
	eq(t, "section", d.AssumptionsSection, false)
}

func TestDesignApiTableRows(t *testing.T) {
	d := ParseDesign("## API\n" +
		"POST /api/v1/limits/check · body {\"amount\":100}\n" +
		"| Mã | Khi nào | Body |\n" +
		"|----|---------|------|\n" +
		"| 200 | trong hạn mức | `{\"allowed\":true}` |\n" +
		"| 400 | thiếu amount | `{\"error\":\"INVALID\"}` |\n" +
		"| 404 | không thấy tài khoản | `{\"error\":\"NOT_FOUND\"}` |\n" +
		"| 500 | thiếu ô |\n")
	var st []string
	for _, r := range d.APIRows {
		st = append(st, r.Status)
	}
	eq(t, "statuses", st, []string{"200", "400", "404"})
	eq(t, "when", d.APIRows[0].When, "trong hạn mức")
	eq(t, "body", d.APIRows[0].Body, "`{\"allowed\":true}`")
	eq(t, "codes", d.StatusCodes, []string{"200", "400", "404"})
	eq(t, "no rows", len(ParseDesign("## API\n- POST /a → 200\n").APIRows), 0)
}

func TestRuleIdsAreNormalized(t *testing.T) {
	d := ParseBRD("## Quy tắc nghiệp vụ\n- **BR-1**: a\n- br-2: b\n- **BR-3:** c\n")
	eq(t, "rules", d.Rules, []Rule{{ID: "BR-1", Text: "a"}, {ID: "BR-2", Text: "b"}, {ID: "BR-3", Text: "c"}})
}

func TestAssumptionSourceCaseInsensitiveAndUnknownIgnored(t *testing.T) {
	d := ParseBRD("## Giả định\n- A. (Tự chọn)\n- B (xem thêm)\n")
	eq(t, "assumptions", d.Assumptions, []Assumption{{"A.", "tự chọn"}, {"B (xem thêm)", ""}})
}

func TestApiRowBodyMayContainPipe(t *testing.T) {
	d := ParseDesign("## API\n| 200 | ok | `a | b` |\n| 400 | thiếu |\n")
	eq(t, "len", len(d.APIRows), 1)
	eq(t, "body", d.APIRows[0].Body, "`a | b`")
}

func TestCaseSpecRulesDropNullAndBlank(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.yaml")
	if err := os.WriteFile(p, []byte("cases:\n  - id: C1\n    rules: [BR-1, null, ' ', ' BR-2 ']\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := spec.ReadCases(p)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "rules", c.Cases[0].Rules, []string{"BR-1", "BR-2"})
}

func TestCaseSpecRulesOptional(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.yaml")
	os.WriteFile(a, []byte("cases:\n  - id: C1\n    step: S1\n"), 0o644)
	c, err := spec.ReadCases(a)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "empty", len(c.Cases[0].Rules), 0)
	b := filepath.Join(dir, "b.yaml")
	os.WriteFile(b, []byte("cases:\n  - id: C1\n    step: S1\n    rules: [BR-1]\n"), 0o644)
	c, err = spec.ReadCases(b)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "rules", c.Cases[0].Rules, []string{"BR-1"})
}

func TestTemplatesHaveAssumptionsAndRuleIds(t *testing.T) {
	read := func(n string) string {
		b, err := os.ReadFile(filepath.Join("..", "..", "templates", n))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	brd := read("brd.md")
	if !strings.Contains(brd, "BR-1") {
		t.Error("brd.md thiếu BR-1")
	}
	if a, q := strings.Index(brd, "## Giả định"), strings.Index(brd, "## Câu hỏi mở"); !(a > 0 && a < q) {
		t.Errorf("brd.md: Giả định %d, Câu hỏi mở %d", a, q)
	}
	design := read("design.md")
	if a, q := strings.Index(design, "## Giả định"), strings.Index(design, "## Câu hỏi mở"); !(a > 0 && a < q) {
		t.Errorf("design.md: Giả định %d, Câu hỏi mở %d", a, q)
	}
}

func TestBoundaryTag(t *testing.T) {
	d := ParseBRD("## Quy tắc nghiệp vụ\n- BR-3: Hạn mức tối đa 500 triệu/ngày [biên]\n- BR-4: thường\n")
	eq(t, "rules", d.Rules, []Rule{{ID: "BR-3", Text: "Hạn mức tối đa 500 triệu/ngày", Boundary: true}, {ID: "BR-4", Text: "thường"}})
}

func TestParseBRDEnglish(t *testing.T) {
	d := ParseBRD("## Business rules\n- BR-1: Max 500M per day [boundary]\n## Assumptions\n- Amounts in VND. (own choice)\n")
	if len(d.Rules) != 1 || !d.Rules[0].Boundary || d.Rules[0].ID != "BR-1" || d.Rules[0].Text != "Max 500M per day" {
		t.Fatalf("rules = %+v", d.Rules)
	}
	if !d.AssumptionsSection || len(d.Assumptions) != 1 || d.Assumptions[0].Source != "own choice" {
		t.Fatalf("assumptions = %+v", d.Assumptions)
	}
}
