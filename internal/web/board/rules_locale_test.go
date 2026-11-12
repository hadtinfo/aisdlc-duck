package board

import (
	"regexp"
	"testing"

	"aisdlc/internal/i18n"
)

var viChar = regexp.MustCompile(`(?i)[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)

// Mọi loại mục hộp thư, locale en: không còn ký tự tiếng Việt, cùng kind/command như vi.
func TestInboxEnglish(t *testing.T) {
	s := js(t, `{"feature":{"flow":"FULL"},
 "gates":[{"gate":"BRD","state":"AWAITING_APPROVAL"},{"gate":"DESIGN","state":"STALE"},{"gate":"CASES","state":"SUBMITTED_WITH_FINDINGS"}],
 "openQuestions":[{"gate":"BRD","text":"Q1?"}],
 "steps":[{"id":"S1","status":"AWAITING_DEV","record":{"check":{"casesNotPassed":["C1","C2"],"outOfScope":["a b.go"]}}}],
 "eval":{"passed":false}}`)
	vi, en := Inbox(s, i18n.VI), Inbox(s, i18n.EN)
	if len(en) != 8 || len(vi) != len(en) {
		t.Fatalf("vi=%v en=%v", vi, en)
	}
	for i := range en {
		if en[i].Kind != vi[i].Kind || en[i].Command != vi[i].Command {
			t.Errorf("kind/command phải giống vi: %v vs %v", en[i], vi[i])
		}
		if viChar.MatchString(en[i].Text) {
			t.Errorf("%s còn tiếng Việt: %q", en[i].Kind, en[i].Text)
		}
		if en[i].Text == vi[i].Text {
			t.Errorf("%s chưa dịch: %q", en[i].Kind, en[i].Text)
		}
	}
}
