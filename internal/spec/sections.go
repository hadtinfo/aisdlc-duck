package spec

import (
	"strings"

	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
)

// SectionID là ID chuẩn của một mục markdown; tiêu đề thay đổi theo locale, ID thì không.
type SectionID string

const (
	SecGoal          SectionID = "goal"
	SecInScope       SectionID = "inScope"
	SecOutOfScope    SectionID = "outOfScope"
	SecRules         SectionID = "rules"
	SecAcceptance    SectionID = "acceptance"
	SecAssumptions   SectionID = "assumptions"
	SecOpenQuestions SectionID = "openQuestions"
)

var sectionOrder = []SectionID{SecGoal, SecInScope, SecOutOfScope, SecRules, SecAcceptance, SecAssumptions, SecOpenQuestions}

var headings = map[SectionID]struct{ VI, EN string }{
	SecGoal:          {"Mục tiêu", "Goal"},
	SecInScope:       {"Trong phạm vi", "In scope"},
	SecOutOfScope:    {"Ngoài phạm vi", "Out of scope"},
	SecRules:         {"Quy tắc nghiệp vụ", "Business rules"},
	SecAcceptance:    {"Tiêu chí chấp nhận", "Acceptance criteria"},
	SecAssumptions:   {"Giả định", "Assumptions"},
	SecOpenQuestions: {"Câu hỏi mở", "Open questions"},
}

// Heading trả tiêu đề của mục theo locale.
func Heading(id SectionID, l i18n.Locale) string {
	h := headings[id]
	if l == i18n.EN {
		return h.EN
	}
	return h.VI
}

func sectionIDOf(name string) (SectionID, bool) {
	name = model.NFC(name)
	for _, id := range sectionOrder {
		h := headings[id]
		if strings.EqualFold(name, model.NFC(h.VI)) || strings.EqualFold(name, model.NFC(h.EN)) {
			return id, true
		}
	}
	return "", false
}

// SectionsByID trả nội dung các mục chuẩn theo ID, nhận tiêu đề của cả hai ngôn ngữ.
// Cùng một ID dưới nhiều tiêu đề thì nối theo thứ tự xuất hiện.
func SectionsByID(md string) map[SectionID]string {
	names, body := MarkdownSections(md)
	out := map[SectionID]string{}
	for _, n := range names {
		if id, ok := sectionIDOf(n); ok {
			out[id] += body[n]
		}
	}
	return out
}

// AssumptionSources là các nguồn hợp lệ của một giả định, theo từng ngôn ngữ.
var AssumptionSources = []struct{ ID, VI, EN string }{
	{"own", "tự chọn", "own choice"},
	{"inferred", "suy ra từ BRD", "inferred from BRD"},
	{"answered", "câu hỏi mở đã trả lời", "answered open question"},
	{"outOfScope", "ngoài phạm vi", "out of scope"},
}

// SourceID trả ID chuẩn của nguồn giả định (khớp không phân biệt hoa thường); không khớp thì "".
func SourceID(s string) string {
	s = strings.TrimSpace(s)
	for _, a := range AssumptionSources {
		if strings.EqualFold(s, a.VI) || strings.EqualFold(s, a.EN) {
			return a.ID
		}
	}
	return ""
}

// BoundaryTags là thẻ đánh dấu quy tắc biên trong BRD.
var BoundaryTags = []string{"[biên]", "[boundary]"}
