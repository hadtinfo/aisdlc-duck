// Package spec chứa kiểu dữ liệu của design.yaml, cases.yaml và đọc YAML.
package spec

import (
	"regexp"
	"strings"
)

var ruleIDPattern = regexp.MustCompile(`^\**(?i:br)-0*(\d+)\**$`)

// NormalizeRuleID chuẩn hóa "br-1", "BR-01", "**BR-1**" thành "BR-1"; chuỗi không phải ID chỉ được trim.
func NormalizeRuleID(raw string) string {
	t := strings.TrimSpace(raw)
	m := ruleIDPattern.FindStringSubmatch(t)
	if m == nil {
		return t
	}
	digits := m[1]
	if digits == "" {
		digits = "0"
	}
	return "BR-" + digits
}

// normalizeRules chuẩn hóa, bỏ chuỗi rỗng, bỏ trùng, giữ thứ tự.
func normalizeRules(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range in {
		if strings.TrimSpace(r) == "" {
			continue
		}
		n := NormalizeRuleID(r)
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}
