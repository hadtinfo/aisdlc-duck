package eval

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/i18n"
	"aisdlc/internal/risk"
	"aisdlc/internal/run"
)

// viChars là chữ cái có dấu tiếng Việt (cùng bảng với test catalog của internal/i18n).
var viChars = regexp.MustCompile(`(?i)[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)

func useLocale(t *testing.T, l i18n.Locale) {
	t.Helper()
	i18n.SetDefault(l)
	t.Cleanup(func() { i18n.SetDefault(i18n.VI) })
}

// eval/report.md theo locale của repo: en thì không còn ký tự tiếng Việt (nội dung do eval sinh ra).
func TestEnglishReportHasNoVietnamese(t *testing.T) {
	useLocale(t, i18n.EN)
	hard := []HardCheck{{"CASES_ALL_PASS", false, "2/3 case pass"}, {"SCOPE", true, ""}}
	soft := []SoftCheck{{"COVERAGE_CHANGED", "OK", "90%"}, AgentReviewCheck(filepath.Join(t.TempDir(), "none.json"))}
	rs := &RiskSection{Level: "high", PolicyVersion: 3, Matched: []string{"r1"},
		Requirements: []risk.RequirementStatus{{ID: "coverage", Name: risk.RequirementDisplayName(
			risk.Requirement{ID: risk.ReqCoverage, Params: map[string]any{"min": 80}}), Status: risk.RequirementMissing, Detail: "x"}}}
	cases := []run.CaseResult{{ID: "C1", Step: "S1", Outcome: run.CaseOutcomeFailed, Message: "status: expected 200, got 500"}}
	md := RenderReport(NewEvalReport("PAY-123", t0, "local", hard, soft, "abc", rs), cases)
	for _, w := range []string{"Eval PAY-123: FAIL", "## Risk", "| Hard check | Result | Detail |", "| Soft metric | Level | Detail |", "Cases not passed:"} {
		if !strings.Contains(md, w) {
			t.Errorf("thiếu %q:\n%s", w, md)
		}
	}
	if m := viChars.FindString(md); m != "" {
		t.Errorf("còn chữ tiếng Việt %q:\n%s", m, md)
	}
	// report.md tiếng Anh vẫn được cập nhật dòng AGENT_REVIEW
	out := WithSoftRow(md, SoftCheck{"AGENT_REVIEW", "OK", "5/5 PASS"})
	if !strings.Contains(out, "| AGENT_REVIEW | OK | 5/5 PASS |") {
		t.Errorf("không thay được dòng:\n%s", out)
	}
}

// Repo đổi locale sau khi đã có report.md: WithSoftRow nhận tiêu đề bảng của cả hai ngôn ngữ.
func TestWithSoftRowAcceptsOtherLocaleHeader(t *testing.T) {
	vi := RenderReport(sampleReport(true), nil)
	useLocale(t, i18n.EN)
	out := WithSoftRow(strings.Replace(vi, "| AGENT_REVIEW |", "| OTHER |", 1), SoftCheck{"AGENT_REVIEW", "OK", "5/5 PASS"})
	if !strings.Contains(out, "| AGENT_REVIEW | OK | 5/5 PASS |") {
		t.Errorf("không chèn được dòng vào report vi:\n%s", out)
	}
}
