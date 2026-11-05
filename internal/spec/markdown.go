package spec

import (
	"regexp"
	"strings"

	"aisdlc/internal/model"
)

var (
	commentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	openQRe   = regexp.MustCompile(`^\s*[-*]\s*\[ \]\s*(.*\S)?\s*$`)
	newlineRe = regexp.MustCompile(`\r\n|\n|\r`)
)

const openQuestionsSection = "Câu hỏi mở"

// MarkdownSections tách markdown theo heading cấp 2 (chuẩn hóa NFC, bỏ comment HTML).
// names giữ thứ tự xuất hiện đầu tiên của mỗi mục.
func MarkdownSections(md string) (names []string, body map[string]string) {
	text := commentRe.ReplaceAllString(model.NFC(md), "")
	bufs := map[string]*strings.Builder{}
	var cur *strings.Builder
	for _, line := range newlineRe.Split(text, -1) {
		if strings.HasPrefix(line, "## ") {
			name := strings.TrimSpace(line[3:])
			b, ok := bufs[name]
			if !ok {
				b = &strings.Builder{}
				bufs[name] = b
				names = append(names, name)
			}
			cur = b
		} else if cur != nil {
			cur.WriteString(line)
			cur.WriteByte('\n')
		}
	}
	body = make(map[string]string, len(bufs))
	for k, v := range bufs {
		body[k] = v.String()
	}
	return names, body
}

// OpenQuestions trả mỗi dòng "- [ ] ..." trong mục "Câu hỏi mở".
func OpenQuestions(md string) []string {
	_, body := MarkdownSections(md)
	b, ok := body[openQuestionsSection]
	if !ok {
		return nil
	}
	var out []string
	for _, line := range newlineRe.Split(b, -1) {
		if m := openQRe.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}
