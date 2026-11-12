package spec

import "aisdlc/internal/i18n"

var brdRequired = []SectionID{SecGoal, SecInScope, SecOutOfScope, SecRules, SecAcceptance}

// ValidateBRD kiểm các mục bắt buộc của brd.md và câu hỏi mở.
func ValidateBRD(content string) []Finding {
	sections := SectionsByID(content)
	var findings []Finding
	for _, id := range brdRequired {
		name := Heading(id, i18n.Default())
		body, ok := sections[id]
		if !ok {
			findings = append(findings, Finding{"BRD_MISSING_SECTION", i18n.Tf("BRD thiếu mục bắt buộc: \"%s\".", name)})
		} else if isBlank(body) {
			findings = append(findings, Finding{"BRD_EMPTY_SECTION", i18n.Tf("Mục \"%s\" của BRD đang trống.", name)})
		}
	}
	for _, q := range OpenQuestions(content) {
		findings = append(findings, Finding{"BRD_OPEN_QUESTION", i18n.Tf("Câu hỏi mở chưa được trả lời: %s", q)})
	}
	return findings
}
