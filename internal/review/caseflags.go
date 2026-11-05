package review

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"aisdlc/internal/spec"
)

const minDigits = 3

var (
	groupSepRe = regexp.MustCompile(`[.,_]`)
	numberRe   = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$`)
)

// ValuesNotInBrd cảnh báo số ở field giá trị trong when/then.body của case mà BRD không nhắc tới.
func ValuesNotInBrd(cases spec.CasesSpec, brdMarkdown string, valueFields []string) []ReviewFlag {
	brd := groupSepRe.ReplaceAllString(brdMarkdown, "")
	fields := map[string]bool{}
	for _, f := range valueFields {
		fields[strings.ToLower(f)] = true
	}
	missing := map[string]map[string]bool{}
	for _, c := range cases.Cases {
		var values []string
		if c.When != nil {
			collectValues(c.When.Body, fields, &values)
		}
		if c.Then != nil {
			collectValues(c.Then.Body, fields, &values)
		}
		for _, v := range values {
			if !containsStandalone(brd, v) {
				if missing[c.ID] == nil {
					missing[c.ID] = map[string]bool{}
				}
				missing[c.ID][v] = true
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	ids := make([]string, 0, len(missing))
	for id := range missing {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return NaturalCompare(ids[i], ids[j]) < 0 })
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		vs := make([]string, 0, len(missing[id]))
		for v := range missing[id] {
			vs = append(vs, v)
		}
		sort.Strings(vs)
		parts = append(parts, id+": "+strings.Join(vs, ", "))
	}
	return []ReviewFlag{NewFlag("CASE_VALUE_NOT_IN_BRD", WARN,
		"Giá trị trong case không thấy trong BRD ("+strings.Join(parts, "; ")+")",
		"Kiểm tra số liệu của case có đúng với BRD không.", ids)}
}

// containsStandalone là (?<!\d)v(?!\d) của Java: v xuất hiện mà hai bên không có chữ số.
func containsStandalone(s, v string) bool {
	for from := 0; from <= len(s); {
		i := strings.Index(s[from:], v)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(v)
		if (i == 0 || !isASCIIDigit(s[i-1])) && (end >= len(s) || !isASCIIDigit(s[end])) {
			return true
		}
		from = i + 1
	}
	return false
}

func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

func collectValues(node any, fields map[string]bool, out *[]string) {
	switch n := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(n))
		for k := range n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch v := n[k].(type) {
			case map[string]any, []any:
				collectValues(v, fields, out)
			default:
				if fields[strings.ToLower(k)] {
					if d, ok := digitsOf(v); ok {
						*out = append(*out, d)
					}
				}
			}
		}
	case []any:
		for _, e := range n {
			collectValues(e, fields, out)
		}
	}
}

// digitsOf lấy chữ số của giá trị số (bỏ dấu và phần thập phân thừa); false khi không phải số hoặc quá ngắn.
func digitsOf(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	var s string
	if f, ok := v.(float64); ok {
		s = strconv.FormatFloat(f, 'g', -1, 64)
	} else {
		s = fmt.Sprint(v)
	}
	s = strings.TrimSpace(s)
	s = strings.NewReplacer(",", "", "_", "").Replace(s)
	if !numberRe.MatchString(s) {
		return "", false
	}
	d := plainDigits(s)
	if len(d) < minDigits {
		return "", false
	}
	return d, true
}

// plainDigits là BigDecimal(s).abs().stripTrailingZeros().toPlainString() bỏ dấu chấm; s đã qua numberRe.
func plainDigits(s string) string {
	s = strings.TrimLeft(s, "+-")
	exp := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exp, _ = strconv.Atoi(strings.TrimPrefix(s[i+1:], "+"))
		s = s[:i]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	digits := strings.TrimLeft(intPart+frac, "0")
	scale := len(frac) - exp
	if digits == "" {
		return "0"
	}
	for strings.HasSuffix(digits, "0") {
		digits = digits[:len(digits)-1]
		scale--
	}
	if scale <= 0 {
		return digits + strings.Repeat("0", -scale)
	}
	if len(digits) > scale {
		return digits[:len(digits)-scale] + digits[len(digits)-scale:]
	}
	return "0" + strings.Repeat("0", scale-len(digits)) + digits
}

// RuleIDs cảnh báo BRD_RULE_NO_ID: quy tắc không có ID BR-n.
func RuleIDs(brd BrdDoc) []ReviewFlag {
	var noID []string
	for _, r := range brd.Rules {
		if r.ID == "" {
			noID = append(noID, r.Text)
		}
	}
	if len(noID) == 0 {
		return nil
	}
	sort.Strings(noID)
	return []ReviewFlag{NewFlag("BRD_RULE_NO_ID", WARN,
		strconv.Itoa(len(noID))+" quy tắc nghiệp vụ không có ID BR-n nên không đối chiếu được với case",
		"Đánh ID dạng \"- BR-1: ...\" cho mỗi quy tắc.", noID)}
}

// RuleCoverage sinh CASES_RULE_UNCOVERED (quy tắc chưa có case) và CASES_RULE_UNKNOWN (case trỏ tới BR-n không có trong BRD).
func RuleCoverage(brd BrdDoc, cases spec.CasesSpec) []ReviewFlag {
	var out []ReviewFlag
	covered := map[string]bool{}
	for _, c := range cases.Cases {
		for _, r := range c.Rules {
			covered[r] = true
		}
	}
	known := map[string]bool{}
	for _, r := range brd.Rules {
		if r.ID != "" {
			known[r.ID] = true
		}
	}
	var uncovered, unknown []string
	for id := range known {
		if !covered[id] {
			uncovered = append(uncovered, id)
		}
	}
	for id := range covered {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	natural := func(l []string) {
		sort.Slice(l, func(i, j int) bool { return NaturalCompare(l[i], l[j]) < 0 })
	}
	natural(uncovered)
	natural(unknown)
	if len(uncovered) > 0 {
		out = append(out, NewFlag("CASES_RULE_UNCOVERED", WARN,
			"Quy tắc chưa có case nào kiểm: "+strings.Join(uncovered, ", "),
			"Thêm case có rules: [BR-n] hoặc bỏ quy tắc không cần kiểm.", uncovered))
	}
	if len(unknown) > 0 && len(known) > 0 {
		out = append(out, NewFlag("CASES_RULE_UNKNOWN", WARN,
			"Case trỏ tới quy tắc không có trong BRD: "+strings.Join(unknown, ", "),
			"Sửa ID trong rules: của case hoặc thêm quy tắc vào BRD.", unknown))
	}
	return out
}
