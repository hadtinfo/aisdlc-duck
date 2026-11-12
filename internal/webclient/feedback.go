package webclient

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
)

var (
	// tiêu đề mục do gate.Service ghi theo locale của repo: nhận cả hai ngôn ngữ (repo có thể đổi locale giữa chừng)
	feedbackHeader = regexp.MustCompile(`^## (\w+) — (trả lại|mở lại|rejected|reopened) (?:bởi|by) (.+) (?:lúc|at) (\S+)\s*$`)
	// FeedbackMarker là dòng đánh dấu id feedback từ web (<!-- aisdlc-fb:<id> -->), không thuộc nội dung góp ý.
	FeedbackMarker = regexp.MustCompile(`^<!-- aisdlc-fb:(\d+) -->\s*$`)
	lineBreak      = regexp.MustCompile(`\r\n|[\n\x0B\f\r\x{85}\x{2028}\x{2029}]`)
)

// Feedback là mục mới nhất của một cổng trong feedback.md (FeedbackLog.Feedback); Kind: REJECTED | REOPENED.
type Feedback struct {
	Kind    string  `json:"kind"`
	By      string  `json:"by"`
	At      Instant `json:"at"`
	Comment string  `json:"comment"`
}

// LastFeedback đọc mục mới nhất của cổng g trong feedback.md (do GateService.Reject/Reopen ghi); ok=false khi chưa có.
func LastFeedback(feedbackMd string, g gate.Gate) (Feedback, bool, error) {
	st, err := os.Stat(feedbackMd)
	if err != nil || !st.Mode().IsRegular() {
		return Feedback{}, false, nil
	}
	b, err := os.ReadFile(feedbackMd)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Feedback{}, false, nil
		}
		return Feedback{}, false, fmt.Errorf(i18n.T("Không đọc được %s: %v"), feedbackMd, err)
	}
	var found *Feedback
	var kind, by string
	var at time.Time
	var body strings.Builder
	for _, line := range lineBreak.Split(string(b)+"\n## END", -1) {
		if !strings.HasPrefix(line, "## ") {
			if !FeedbackMarker.MatchString(line) {
				body.WriteString(line)
				body.WriteByte('\n')
			}
			continue
		}
		if kind != "" {
			found = &Feedback{Kind: kind, By: by, At: Instant(at), Comment: strings.TrimSpace(body.String())}
		}
		kind = ""
		body.Reset()
		m := feedbackHeader.FindStringSubmatch(line)
		if m != nil && m[1] == string(g) {
			t, err := time.Parse(time.RFC3339Nano, m[4])
			if err != nil {
				continue // header hỏng: bỏ qua mục này
			}
			at, by = t.UTC(), m[3]
			kind = "REOPENED"
			if m[2] == "trả lại" || m[2] == "rejected" {
				kind = "REJECTED"
			}
		}
	}
	if found == nil {
		return Feedback{}, false, nil
	}
	return *found, true, nil
}
