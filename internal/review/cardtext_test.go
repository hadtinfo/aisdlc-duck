package review

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func textCard() ReviewCard {
	var as []Assumption
	for i := 1; i <= 12; i++ {
		as = append(as, Assumption{fmt.Sprintf("giả định số %d", i), "tự chọn"})
	}
	flags := []ReviewFlag{
		NewFlag("ASSUMPTIONS_EMPTY", INFO, "thông tin", "gợi ý info", nil),
		NewFlag("SCOPE_MATCHES_OLD_MIGRATION", WARN, strings.Repeat("x", 200), "gợi ý warn", nil),
	}
	yes := true
	return ReviewCard{Gate: "DESIGN", Flags: flags, Assumptions: as, AssumptionsSection: &yes}
}

func lineIndex(lines []string, part string) int {
	for i, l := range lines {
		if strings.Contains(l, part) {
			return i
		}
	}
	return -1
}

func has(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func TestRiskShowsAgentAssessment(t *testing.T) {
	c := ReviewCard{Gate: "DESIGN", Flags: []ReviewFlag{}, Risk: &RiskPart{Level: "high", PolicyVersion: 2,
		Agent: &RiskAgent{Level: "medium", Labels: []string{"schema-change", "auth"}, Reason: "Đổi bảng quyền"}}}
	lines := strings.Split(RenderCard("PAY-1", c, 120, false, ""), "\n")
	for _, w := range []string{"Rủi ro: high", "Agent: medium — Đổi bảng quyền, nhãn: schema-change, auth"} {
		if !has(lines, w) {
			t.Errorf("thiếu dòng %q trong %q", w, lines)
		}
	}
	none := ReviewCard{Gate: "DESIGN", Flags: []ReviewFlag{}, Risk: &RiskPart{Level: "high", PolicyVersion: 2}}
	if strings.Contains(RenderCard("PAY-1", none, 120, false, ""), "Agent:") {
		t.Error("không được in Agent khi thiếu đánh giá")
	}
}

func TestLinesWithinWidthAndOrder(t *testing.T) {
	text := RenderCard("PAY-1", textCard(), 100, false, "")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for _, l := range lines {
		if utf8.RuneCountInString(l) > 100 {
			t.Errorf("dòng quá rộng: %q", l)
		}
	}
	eq(t, "title", lines[0], "Review card — PAY-1 · cổng DESIGN")
	warn := lineIndex(lines, "[WARN] SCOPE_MATCHES_OLD_MIGRATION")
	info := lineIndex(lines, "[INFO] ASSUMPTIONS_EMPTY")
	if warn <= 0 || warn >= info {
		t.Fatalf("thứ tự cờ: warn %d info %d", warn, info)
	}
	if !strings.HasSuffix(lines[warn], "…") {
		t.Errorf("dòng warn chưa cắt: %q", lines[warn])
	}
	eq(t, "hint", lines[warn+1], "         → gợi ý warn")
	for _, w := range []string{"Cờ đỏ (2)", "Giả định (12)", "  … và 2 nữa"} {
		if !has(lines, w) {
			t.Errorf("thiếu dòng %q", w)
		}
	}
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "  - giả định") {
			n++
		}
	}
	eq(t, "assumption lines", n, 10)
	if strings.Contains(text, "\u001b") {
		t.Error("không màu mà vẫn có escape")
	}
}

func TestColorOnlyWhenAsked(t *testing.T) {
	if !strings.Contains(RenderCard("PAY-1", textCard(), 100, true, ""), "\u001b[33m[WARN]") {
		t.Error("thiếu màu WARN")
	}
}

func TestNoAssumptionsSectionWhenCardHasNone(t *testing.T) {
	c := ReviewCard{Gate: "CASES", Flags: []ReviewFlag{}}
	if strings.Contains(RenderCard("PAY-1", c, 100, false, ""), "Giả định") {
		t.Error("thẻ CASES không có mục giả định")
	}
	no := false
	legacy := ReviewCard{Gate: "BRD", Flags: []ReviewFlag{}, Assumptions: []Assumption{}, AssumptionsSection: &no}
	out := RenderCard("PAY-1", legacy, 100, false, "")
	if !strings.Contains(out, "Giả định (0)") || !strings.Contains(out, "(không có mục Giả định)") {
		t.Errorf("thẻ cũ: %q", out)
	}
}

func TestApprovalLineUnderTitle(t *testing.T) {
	lines := strings.Split(RenderCard("PAY-1", textCard(), 100, false, "tự duyệt (mức low)"), "\n")
	eq(t, "approval", lines[1], "Duyệt: tự duyệt (mức low)")
}
