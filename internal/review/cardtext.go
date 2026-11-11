package review

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxCardLines = 10

var textOrder = []Level{HIGH, WARN, NA, INFO}

type cardText struct {
	width int
	color bool
	lines []string
}

// RenderCard dựng review card dạng chữ cho terminal: bố cục cố định, mỗi dòng không quá width cột.
// key là feature key để in tiêu đề (rỗng thì chỉ có cổng); approval khác rỗng thì in dòng "Duyệt: …" dưới tiêu đề.
func RenderCard(key string, card ReviewCard, width int, color bool, approval string) string {
	if width < 20 {
		width = 20
	}
	t := &cardText{width: width, color: color}
	t.card(key, card, approval)
	return strings.Join(t.lines, "\n") + "\n"
}

func (t *cardText) card(key string, c ReviewCard, approval string) {
	prefix := ""
	if key != "" {
		prefix = key + " · "
	}
	t.add("Review card — " + prefix + "cổng " + c.Gate)
	if approval != "" {
		t.add("Duyệt: " + approval)
	}
	if c.Risk != nil {
		t.add("Rủi ro: " + c.Risk.Level)
		if a := c.Risk.Agent; a != nil {
			reason := ""
			if strings.TrimSpace(a.Reason) != "" {
				reason = " — " + a.Reason
			}
			labels := ""
			if len(a.Labels) > 0 {
				labels = ", nhãn: " + strings.Join(a.Labels, ", ")
			}
			t.add("Agent: " + a.Level + reason + labels)
		}
	}
	if c.RiskLLM != nil {
		t.riskLLM(*c.RiskLLM)
	}
	t.flags(c.Flags)
	t.assumptions(c)
	if c.Brd != nil {
		t.brd(*c.Brd)
	}
	if c.Design != nil {
		t.design(*c.Design)
	}
	if c.Cases != nil {
		t.cases(*c.Cases)
	}
}

func (t *cardText) flags(flags []ReviewFlag) {
	t.add("")
	t.add("Cờ đỏ (" + strconv.Itoa(len(flags)) + ")")
	if len(flags) == 0 {
		t.add("  Không có cờ nào")
	}
	shown := 0
	for _, lv := range textOrder {
		for _, f := range flags {
			if f.Level != lv {
				continue
			}
			if shown == maxCardLines {
				break
			}
			shown++
			t.flagLine("  ["+string(lv)+"] "+f.Code+": "+f.Message, lv)
			if strings.TrimSpace(f.Hint) != "" {
				t.add("         → " + f.Hint)
			}
		}
	}
	t.more(len(flags), shown)
}

func (t *cardText) flagLine(text string, lv Level) {
	cut := t.cut(text)
	if t.color {
		tag := "[" + string(lv) + "]"
		code := "\u001b[2m"
		switch lv {
		case HIGH:
			code = "\u001b[31m"
		case WARN:
			code = "\u001b[33m"
		}
		cut = strings.Replace(cut, tag, code+tag+"\u001b[0m", 1)
	}
	t.lines = append(t.lines, cut)
}

// assumptions: chỉ thẻ BRD và DESIGN có mục giả định; thẻ khác (Assumptions nil) bỏ hẳn mục này.
func (t *cardText) assumptions(c ReviewCard) {
	if c.Assumptions == nil {
		return
	}
	as := c.Assumptions
	t.add("")
	t.add("Giả định (" + strconv.Itoa(len(as)) + ")")
	if len(as) == 0 {
		if c.AssumptionsSection != nil && *c.AssumptionsSection {
			t.add("  Agent không ghi giả định nào")
		} else {
			t.add("  Agent không ghi giả định nào (không có mục Giả định)")
		}
		return
	}
	n := min(len(as), maxCardLines)
	for _, a := range as[:n] {
		src := ""
		if strings.TrimSpace(a.Source) != "" {
			src = " (" + a.Source + ")"
		}
		t.add("  - " + a.Text + src)
	}
	t.more(len(as), n)
}

func (t *cardText) brd(b BrdPart) {
	t.add("")
	t.add("BRD")
	t.add(fmt.Sprintf("  Quy tắc: %d (%d có ID) · Tiêu chí chấp nhận: %d", b.Rules, b.RulesWithID, b.AcceptanceCriteria))
	t.list("  Ngoài phạm vi:", b.OutOfScope)
	t.list("  Câu hỏi đã trả lời:", b.AnsweredQuestions)
}

func (t *cardText) design(d DesignPart) {
	t.add("")
	t.add("Thiết kế")
	t.add("  Bước:")
	n := min(len(d.Steps), maxCardLines)
	for _, s := range d.Steps[:n] {
		budget := ""
		if s.Budget != nil {
			budget = fmt.Sprintf(" · tối đa %d file/%d dòng", s.Budget.Files, s.Budget.Lines)
		}
		t.add("    " + s.ID + " " + s.Title + " · scope " + strings.Join(s.Scope, ", ") + budget +
			" · khớp " + strconv.Itoa(s.MatchedCount) + " file")
		if len(s.PlannedFiles) > 0 {
			t.add("        sẽ tạo (migration nháp): " + strings.Join(s.PlannedFiles, ", "))
		}
		if len(s.EmptyGlobs) > 0 {
			t.add("        glob chưa khớp file nào: " + strings.Join(s.EmptyGlobs, ", "))
		}
	}
	t.more(len(d.Steps), n)
	t.list("  API:", d.APILines)
	if len(d.StatusCodes) > 0 {
		t.add("  Mã trạng thái: " + strings.Join(d.StatusCodes, ", "))
	}
	if len(d.Tables) > 0 {
		t.add("  Bảng DB:")
		k := min(len(d.Tables), maxCardLines)
		for _, tp := range d.Tables[:k] {
			names := make([]string, 0, len(tp.Columns))
			for _, c := range tp.Columns {
				names = append(names, c.Name)
			}
			cols := ""
			if len(names) > 0 {
				cols = " (" + strings.Join(names, ", ") + ")"
			}
			t.add("    [" + tp.Status + "] " + tp.Name + cols)
		}
		t.more(len(d.Tables), k)
	}
	touch := make([]string, 0, len(d.TouchesExisting))
	for _, s := range d.TouchesExisting {
		touch = append(touch, s.Kind+" "+s.Table+": "+strings.Join(strings.Fields(s.Text), " "))
	}
	t.list("  Động vào bảng có sẵn:", touch)
}

func (t *cardText) cases(c CasesPart) {
	t.add("")
	t.add("Case")
	n := min(len(c.Rows), maxCardLines)
	for _, r := range c.Rows[:n] {
		exp := ""
		if strings.TrimSpace(r.Expect) != "" {
			exp = " → " + r.Expect
		}
		t.add("  " + r.ID + " " + r.Step + " " + r.Kind + " " + r.Title + exp)
	}
	t.more(len(c.Rows), n)
	if len(c.Coverage) > 0 {
		t.add("  Độ phủ (số case theo loại):")
		cov := make([]string, 0, len(c.Coverage))
		for _, e := range c.Coverage {
			kinds := sortedKeys(e.Counts)
			parts := make([]string, 0, len(kinds))
			for _, k := range kinds {
				parts = append(parts, k+" "+strconv.Itoa(e.Counts[k]))
			}
			cov = append(cov, e.Step+": "+strings.Join(parts, ", "))
		}
		k := min(len(cov), maxCardLines)
		for _, s := range cov[:k] {
			t.add("    " + s)
		}
		t.more(len(cov), k)
	}
	if len(c.RuleMatrix) > 0 {
		t.add("  Quy tắc → case:")
		m := make([]string, 0, len(c.RuleMatrix))
		for _, e := range c.RuleMatrix {
			// ruleMatrix có đủ mọi loại (danh sách rỗng): chỉ in loại có case
			var parts []string
			for _, k := range sortedKeys(e.ByKind) {
				if len(e.ByKind[k]) > 0 {
					parts = append(parts, k+" ["+strings.Join(e.ByKind[k], ", ")+"]")
				}
			}
			byKind := strings.Join(parts, ", ")
			if byKind == "" {
				byKind = "chưa có case"
			}
			m = append(m, e.Rule+": "+byKind)
		}
		k := min(len(m), maxCardLines)
		for _, s := range m[:k] {
			t.add("    " + s)
		}
		t.more(len(m), k)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (t *cardText) list(title string, items []string) {
	if len(items) == 0 {
		return
	}
	t.add(title)
	n := min(len(items), maxCardLines)
	for _, s := range items[:n] {
		t.add("    - " + s)
	}
	t.more(len(items), n)
}

func (t *cardText) more(total, shown int) {
	if shown < total {
		t.add("  … và " + strconv.Itoa(total-shown) + " nữa")
	}
}

func (t *cardText) add(s string) { t.lines = append(t.lines, t.cut(s)) }

func (t *cardText) cut(s string) string {
	one := strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
	if utf8.RuneCountInString(one) <= t.width {
		return one
	}
	r := []rune(one)
	return string(r[:t.width-1]) + "…"
}

func (t *cardText) riskLLM(b RiskLLMBlock) {
	t.add("Xếp loại LLM: " + b.Level)
	if strings.TrimSpace(b.Summary) != "" {
		t.add("  " + b.Summary)
	}
	if b.Override != nil {
		t.add("  Lệch mức rule (" + b.Override.From + "): " + b.Override.Reason)
	}
	for _, m := range b.Matched {
		t.add("  " + m.Rule + " (" + m.Level + "): " + m.On)
	}
	for _, n := range b.NotMatched {
		t.add("  Không khớp: " + n.Rule + " — " + n.Reason)
	}
}
