package review

import (
	"regexp"
	"sort"
	"strings"

	"aisdlc/internal/spec"
)

// Assumption là một giả định agent tự chốt; Source rỗng khi dòng không ghi nguồn hợp lệ.
type Assumption struct {
	Text   string `json:"text"`
	Source string `json:"source"`
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

var (
	bulletRe     = regexp.MustCompile(`^\s*[-*]\s+(.*\S)\s*$`)
	ruleRe       = regexp.MustCompile(`^\**(?i:br)-(\d+)\**\s*:\s*\**\s*(.*)$`)
	boundaryRe   = regexp.MustCompile(`^(.*\S)\s*\[(?:biên|boundary)\]\s*$`)
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
	s := spec.SectionsByID(md)
	rules := []Rule{}
	for _, b := range bullets(s[spec.SecRules]) {
		rules = append(rules, parseRule(b))
	}
	answered := []string{}
	for _, b := range bullets(s[spec.SecOpenQuestions]) {
		if m := answeredRe.FindStringSubmatch(b); m != nil {
			answered = append(answered, strings.TrimSpace(m[1]))
		}
	}
	_, has := s[spec.SecAssumptions]
	return BrdDoc{
		Rules:              rules,
		AcceptanceCriteria: len(bullets(s[spec.SecAcceptance])),
		OutOfScope:         bullets(s[spec.SecOutOfScope]),
		AnsweredQuestions:  answered,
		Assumptions:        assumptions(s[spec.SecAssumptions]),
		AssumptionsSection: has,
	}
}

// ParseDesign đọc dữ kiện xác định từ design.md.
func ParseDesign(md string) DesignDoc {
	_, byName := spec.MarkdownSections(md)
	s := spec.SectionsByID(md)
	api := byName["API"]
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
	_, has := s[spec.SecAssumptions]
	return DesignDoc{
		Assumptions:        assumptions(s[spec.SecAssumptions]),
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
		tail := strings.TrimSpace(m[2])
		for _, src := range spec.AssumptionSources {
			if strings.EqualFold(src.VI, tail) {
				return Assumption{m[1], src.VI}
			}
			if strings.EqualFold(src.EN, tail) {
				return Assumption{m[1], src.EN}
			}
		}
	}
	return Assumption{line, ""}
}
