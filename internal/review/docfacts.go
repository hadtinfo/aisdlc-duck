package review

import (
	"regexp"
	"sort"
	"strings"

	"aisdlc/internal/spec"
)

// Assumption là một giả định agent tự chốt; Source rỗng khi dòng không ghi nguồn hợp lệ.
type Assumption struct {
	Text, Source string
}

// Rule là một quy tắc nghiệp vụ; ID rỗng khi dòng không có mã BR-n.
type Rule struct {
	ID       string
	Text     string
	Boundary bool
}

type BrdDoc struct {
	Rules              []Rule
	AcceptanceCriteria int
	OutOfScope         []string
	AnsweredQuestions  []string
	Assumptions        []Assumption
	AssumptionsSection bool
}

type ApiRow struct {
	Status string `json:"status"`
	When   string `json:"when"`
	Body   string `json:"body"`
}

type DesignDoc struct {
	Assumptions        []Assumption
	AssumptionsSection bool
	APILines           []string
	StatusCodes        []string
	APIRows            []ApiRow
}

// Sources là các nguồn hợp lệ của một giả định.
var Sources = []string{"tự chọn", "suy ra từ BRD", "câu hỏi mở đã trả lời", "ngoài phạm vi"}

const assumptionsSection = "Giả định"

var (
	bulletRe     = regexp.MustCompile(`^\s*[-*]\s+(.*\S)\s*$`)
	ruleRe       = regexp.MustCompile(`^\**(?i:br)-(\d+)\**\s*:\s*\**\s*(.*)$`)
	boundaryRe   = regexp.MustCompile(`^(.*\S)\s*\[biên\]\s*$`)
	answeredRe   = regexp.MustCompile(`^\[[xX]\]\s*(.*)$`)
	sourceTailRe = regexp.MustCompile(`^(.*\S)\s*\(([^()]*)\)\s*$`)
	apiLineRe    = regexp.MustCompile(`\b(GET|POST|PUT|PATCH|DELETE)\s+/\S*`)
	digitsRe     = regexp.MustCompile(`\d+`)
	threeDigitRe = regexp.MustCompile(`^\d{3}$`)
	listMarkRe   = regexp.MustCompile(`^[-*]\s+`)
)

// lines tách theo \n, \r hoặc \r\n, không trả dòng rỗng cuối (như String.lines của Java).
func lines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	out := strings.Split(s, "\n")
	if out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// ParseBRD đọc dữ kiện xác định từ brd.md; đầu vào rỗng cho kết quả rỗng.
func ParseBRD(md string) BrdDoc {
	_, s := spec.MarkdownSections(md)
	rules := []Rule{}
	for _, b := range bullets(s["Quy tắc nghiệp vụ"]) {
		rules = append(rules, parseRule(b))
	}
	answered := []string{}
	for _, b := range bullets(s["Câu hỏi mở"]) {
		if m := answeredRe.FindStringSubmatch(b); m != nil {
			answered = append(answered, strings.TrimSpace(m[1]))
		}
	}
	_, has := s[assumptionsSection]
	return BrdDoc{
		Rules:              rules,
		AcceptanceCriteria: len(bullets(s["Tiêu chí chấp nhận"])),
		OutOfScope:         bullets(s["Ngoài phạm vi"]),
		AnsweredQuestions:  answered,
		Assumptions:        assumptions(s[assumptionsSection]),
		AssumptionsSection: has,
	}
}

// ParseDesign đọc dữ kiện xác định từ design.md.
func ParseDesign(md string) DesignDoc {
	_, s := spec.MarkdownSections(md)
	api := s["API"]
	apiLines := []string{}
	rows := []ApiRow{}
	codes := map[string]bool{}
	for _, raw := range lines(api) {
		l := listMarkRe.ReplaceAllString(strings.TrimSpace(raw), "")
		if apiLineRe.MatchString(l) {
			apiLines = append(apiLines, l)
		}
		if r, ok := apiRow(raw); ok {
			rows = append(rows, r)
		}
		if strings.HasPrefix(strings.TrimSpace(raw), "|") {
			continue
		}
		// phần sau "·" là body mẫu, số trong đó không phải mã trạng thái
		if cut := strings.Index(raw, "·"); cut >= 0 {
			raw = raw[:cut]
		}
		for _, d := range digitsRe.FindAllString(raw, -1) {
			if len(d) == 3 {
				codes[d] = true
			}
		}
	}
	for _, r := range rows {
		codes[r.Status] = true
	}
	sc := make([]string, 0, len(codes))
	for c := range codes {
		sc = append(sc, c)
	}
	sort.Strings(sc)
	_, has := s[assumptionsSection]
	return DesignDoc{
		Assumptions:        assumptions(s[assumptionsSection]),
		AssumptionsSection: has,
		APILines:           apiLines,
		StatusCodes:        sc,
		APIRows:            rows,
	}
}

func bullets(body string) []string {
	out := []string{}
	for _, l := range lines(body) {
		if m := bulletRe.FindStringSubmatch(l); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// apiRow đọc dòng bảng "| 200 | khi | body |"; bỏ qua header, dòng phân cách và dòng không đủ 3 ô.
func apiRow(line string) (ApiRow, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "|") {
		return ApiRow{}, false
	}
	end := len(t)
	if strings.HasSuffix(t, "|") && len(t) > 1 {
		end = len(t) - 1
	}
	cells := strings.SplitN(t[1:end], "|", 3)
	if len(cells) != 3 {
		return ApiRow{}, false
	}
	status := strings.TrimSpace(cells[0])
	if !threeDigitRe.MatchString(status) {
		return ApiRow{}, false
	}
	return ApiRow{status, strings.TrimSpace(cells[1]), strings.TrimSpace(cells[2])}, true
}

func parseRule(line string) Rule {
	body := line
	boundary := false
	if b := boundaryRe.FindStringSubmatch(line); b != nil {
		boundary, body = true, b[1]
	}
	if m := ruleRe.FindStringSubmatch(body); m != nil {
		return Rule{spec.NormalizeRuleID("BR-" + m[1]), strings.TrimSpace(m[2]), boundary}
	}
	return Rule{"", body, boundary}
}

func assumptions(body string) []Assumption {
	out := []Assumption{}
	for _, b := range bullets(body) {
		out = append(out, parseAssumption(b))
	}
	return out
}

func parseAssumption(line string) Assumption {
	if m := sourceTailRe.FindStringSubmatch(line); m != nil {
		for _, src := range Sources {
			if strings.EqualFold(src, strings.TrimSpace(m[2])) {
				return Assumption{m[1], src}
			}
		}
	}
	return Assumption{line, ""}
}
