package gates

import (
	"aisdlc/internal/i18n"
	"aisdlc/internal/snapshot"
)

// feedbackInstruction là câu lưu vào instructions khi trả lại (REJECTED) hoặc mở lại một cổng (snapshot.FeedbackInstruction).
func feedbackInstruction(kind, gate, login string) string {
	return snapshot.FeedbackInstruction(kind, gate, login)
}

// LocalizeFeedback dịch câu chỉ dẫn feedback đã lưu sang loc (snapshot.LocalizeFeedback, dùng chung với CLI).
func LocalizeFeedback(text string, loc i18n.Locale) string {
	return snapshot.LocalizeFeedback(text, loc)
}
