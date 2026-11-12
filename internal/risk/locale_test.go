package risk

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
)

// viChars là chữ cái có dấu tiếng Việt (cùng bảng với test catalog của internal/i18n).
var viChars = regexp.MustCompile(`(?i)[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)

func noVietnamese(t *testing.T, what, s string) {
	t.Helper()
	if viChars.MatchString(s) {
		t.Errorf("%s còn chữ tiếng Việt: %q", what, s)
	}
}

const badPolicy = "schema: 2\nlevels: [low]\ndefault: mid\nbogus: 1\nrules:\n  - id: R1\n    level: x\n    describe: ''\nflow:\n  low: {review: auto, brd: maybe}\n"

// Dạng In theo locale tường minh, không đụng locale mặc định (web server gọi dạng này với locale của request).
func TestParsePolicyInEnglish(t *testing.T) {
	_, err := ParsePolicyIn(i18n.EN, badPolicy)
	var pe *PolicyError
	if !errors.As(err, &pe) || len(pe.Errors) < 4 {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasPrefix(err.Error(), "Invalid risk policy: ") || !strings.Contains(err.Error(), "unknown key 'bogus'") {
		t.Errorf("err = %v", err)
	}
	noVietnamese(t, "lỗi", err.Error())
	if _, err := PolicyFromJSONIn(i18n.EN, "{"); err == nil {
		t.Error("JSON hỏng không lỗi")
	} else {
		noVietnamese(t, "lỗi JSON", err.Error())
	}
	// dạng cũ vẫn theo locale mặc định (vi)
	if _, err := ParsePolicy(badPolicy); err == nil || !strings.HasPrefix(err.Error(), "Chính sách rủi ro không hợp lệ: ") {
		t.Errorf("vi: %v", err)
	}
	// PolicyError dựng ngoài gói (web) không có locale: vi
	if got := (&PolicyError{Errors: []string{"x"}}).Error(); got != "Chính sách rủi ro không hợp lệ: x" {
		t.Errorf("PolicyError{} = %q", got)
	}
}

func TestPresetsMigrateDiffInEnglish(t *testing.T) {
	if _, err := LoadPresetIn(i18n.EN, "nope", "1"); err == nil {
		t.Error("mẫu lạ không lỗi")
	} else {
		noVietnamese(t, "lỗi mẫu", err.Error())
	}
	p, err := LoadPresetIn(i18n.EN, "banking", "2")
	if err != nil {
		t.Fatal(err)
	}
	m, err := MigrateToV2In(i18n.EN, p)
	if err != nil {
		t.Fatal(err)
	}
	kw := false
	for _, r := range m.Rules {
		kw = kw || strings.Contains(r.Describe, "(old keyword: ")
	}
	if !kw {
		t.Errorf("rules = %+v", m.Rules)
	}
	off := m
	off.Rules = append([]RiskRule{}, m.Rules...)
	off.Rules[0].Enabled = false
	for _, c := range DiffPoliciesIn(i18n.EN, m, off) {
		if c.Kind == "CHANGE_RULE" && !strings.HasSuffix(c.After, " (off)") {
			t.Errorf("after = %q", c.After)
		}
	}
	if _, err := LatestPresetIn(i18n.EN, "nope"); err == nil {
		t.Error("mẫu lạ không lỗi")
	} else {
		noVietnamese(t, "lỗi mẫu", err.Error())
	}
	msg := SupersedeMessageIn(i18n.EN, &m, "low", "high", []gate.Gate{gate.DESIGN, gate.CASES})
	if msg != "Level raised low → high: gate Design, Case needs dev re-approval." {
		t.Errorf("msg = %q", msg)
	}
	if r, manual := AutoGateIn(i18n.EN, m, "low", gate.BRD, false); !manual || r != "The BRD gate is always approved manually" {
		t.Errorf("AutoGateIn = %q %v", r, manual)
	}
	if n := RequirementDisplayNameIn(i18n.EN, Requirement{ID: ReqCoverage, Params: map[string]any{"min": 80}}); n != "Minimum new-line coverage 80%" {
		t.Errorf("name = %q", n)
	}
}
