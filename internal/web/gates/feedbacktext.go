package gates

import (
	"strings"

	"aisdlc/internal/i18n"
)

// Chỉ dẫn "trả lại / mở lại" gửi agent được lưu tiếng Việt (bảng instructions), như mọi chữ server đã lưu. Khi hiển
// thị trên web thì dịch theo locale người xem: tách lại cổng và login từ câu đã lưu (cả dòng lưu từ trước bản song
// ngữ), phần ghi chú của người duyệt giữ nguyên văn.
var (
	fbReopened = i18n.VI.T("Cổng %s được mở lại bởi %s")
	fbRejected = i18n.VI.T("Cổng %s bị trả lại bởi %s")
)

// feedbackInstruction là câu lưu vào instructions khi trả lại (REJECTED) hoặc mở lại một cổng.
func feedbackInstruction(kind, gate, login string) string {
	if kind == "REJECTED" {
		return i18n.VI.Tf(fbRejected, gate, login)
	}
	return i18n.VI.Tf(fbReopened, gate, login)
}

// LocalizeFeedback dịch câu chỉ dẫn feedback đã lưu sang loc; câu không khớp mẫu thì giữ nguyên.
func LocalizeFeedback(text string, loc i18n.Locale) string {
	if loc == i18n.VI {
		return text
	}
	for _, key := range []string{fbReopened, fbRejected} {
		parts := strings.Split(key, "%s") // [tiền tố, giữa, ""]
		rest, ok := strings.CutPrefix(text, parts[0])
		if !ok {
			continue
		}
		gate, rest, ok := strings.Cut(rest, parts[1])
		if !ok || gate == "" || strings.ContainsAny(gate, " \n") {
			continue
		}
		login, tail := rest, ""
		if i := strings.Index(rest, ": "); i >= 0 {
			login, tail = rest[:i], rest[i:]
		}
		if login == "" || strings.ContainsAny(login, " \n") {
			continue
		}
		return loc.Tf(key, gate, login) + tail
	}
	return text
}
