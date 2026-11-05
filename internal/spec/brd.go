package spec

import "aisdlc/internal/model"

var brdRequired = []string{"Mục tiêu", "Trong phạm vi", "Ngoài phạm vi", "Quy tắc nghiệp vụ", "Tiêu chí chấp nhận"}

// ValidateBRD kiểm các mục bắt buộc của brd.md và câu hỏi mở.
func ValidateBRD(content string) []Finding {
	_, sections := MarkdownSections(content)
	var findings []Finding
	for _, name := range brdRequired {
		body, ok := sections[model.NFC(name)]
		if !ok {
			findings = append(findings, Finding{"BRD_MISSING_SECTION", "BRD thiếu mục bắt buộc: \"" + name + "\"."})
		} else if isBlank(body) {
			findings = append(findings, Finding{"BRD_EMPTY_SECTION", "Mục \"" + name + "\" của BRD đang trống."})
		}
	}
	for _, q := range OpenQuestions(content) {
		findings = append(findings, Finding{"BRD_OPEN_QUESTION", "Câu hỏi mở chưa được trả lời: " + q})
	}
	return findings
}
