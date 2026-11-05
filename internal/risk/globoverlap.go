package risk

import (
	"strings"

	"aisdlc/internal/review"
)

// Overlaps: hai glob có thể cùng khớp một đường dẫn không (xấp xỉ theo đường dẫn mẫu, spec §3.2).
// sampleFile là tên file mẫu của ngôn ngữ (lang.Adapter.SampleFileName).
func Overlaps(ruleGlob, scopeGlob, sampleFile string) bool {
	if strings.TrimSpace(ruleGlob) == "" || strings.TrimSpace(scopeGlob) == "" {
		return false
	}
	return sampleMatches(ruleGlob, scopeGlob, sampleFile) ||
		sampleMatches(scopeGlob, ruleGlob, sampleFile) ||
		openUnderPrefix(ruleGlob, scopeGlob) ||
		openUnderPrefix(scopeGlob, ruleGlob)
}

// sampleMatches: đường dẫn mẫu sinh từ other có khớp glob không.
func sampleMatches(glob, other, sampleFile string) bool {
	return review.ScopeMatcher([]string{glob})(review.SampleFor(other, sampleFile))
}

// openUnderPrefix: open dạng "tiền_tố/**" (tiền tố không có ký tự đại diện) và lead mở đầu bằng "**/".
func openUnderPrefix(open, lead string) bool {
	if !strings.HasPrefix(lead, "**/") || !strings.HasSuffix(open, "/**") {
		return false
	}
	prefix := open[:len(open)-3]
	return !strings.ContainsAny(prefix, "*?[{")
}
