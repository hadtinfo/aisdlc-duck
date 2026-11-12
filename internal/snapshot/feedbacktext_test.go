package snapshot

import (
	"testing"

	"aisdlc/internal/i18n"
)

func TestFeedbackInstructionStoredVietnameseAndLocalized(t *testing.T) {
	rej := FeedbackInstruction("REJECTED", "BRD", "an") + ": thiếu mục: Mục tiêu"
	if rej != "Cổng BRD bị trả lại bởi an: thiếu mục: Mục tiêu" {
		t.Fatalf("câu lưu đổi: %q", rej)
	}
	if got := LocalizeFeedback(rej, i18n.EN); got != "Gate BRD was rejected by an: thiếu mục: Mục tiêu" {
		t.Fatalf("EN: %q", got)
	}
	if got := LocalizeFeedback(rej, i18n.VI); got != rej {
		t.Fatalf("VI phải giữ nguyên: %q", got)
	}
	re := FeedbackInstruction("REOPENED", "DESIGN", "binh")
	if re != "Cổng DESIGN được mở lại bởi binh" || LocalizeFeedback(re, i18n.EN) != "Gate DESIGN was reopened by binh" {
		t.Fatal(re, LocalizeFeedback(re, i18n.EN))
	}
	for _, s := range []string{"sửa test", "Cổng  bị trả lại bởi x", "Cổng BRD bị trả lại bởi "} {
		if got := LocalizeFeedback(s, i18n.EN); got != s {
			t.Errorf("không khớp mẫu phải giữ nguyên: %q → %q", s, got)
		}
	}
}
