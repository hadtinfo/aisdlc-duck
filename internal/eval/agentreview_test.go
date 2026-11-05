package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func criterion(id, label string) string {
	return `{"id":"` + id + `","label":"` + label + `","reason":"lý do ` + id + `"}`
}

func reviewJSON(criteria, issues string) string {
	return `{"summary":"ổn","criteria":[` + criteria + `],"issues":[` + issues + `]}`
}

func fiveCriteria(securityLabel string) string {
	return strings.Join([]string{criterion("sequence", "DAT"), criterion("error-handling", "DAT"),
		criterion("over-engineering", "DAT"), criterion("naming", "DAT"), criterion("security", securityLabel)}, ",")
}

func issue(severity, category string) string {
	return `{"step":"S1","file":"internal/a.go","line":12,"severity":"` + severity + `","category":"` + category +
		`","message":"thiếu kiểm tra nil"}`
}

func writeReview(t *testing.T, json string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "review-agent.json")
	if err := os.WriteFile(f, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func wantErr(t *testing.T, path string, parts ...string) {
	t.Helper()
	_, err := ReadAgentReview(path)
	if err == nil {
		t.Fatal("phải lỗi")
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("lỗi %q thiếu %q", err, p)
		}
	}
}

func TestReadsValidReview(t *testing.T) {
	r, err := ReadAgentReview(writeReview(t, reviewJSON(fiveCriteria("CAN_SUA"),
		issue("medium", "security")+","+issue("low", "other"))))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "summary", r.Summary, "ổn")
	eq(t, "criteria", len(r.Criteria), 5)
	eq(t, "passed", r.PassedCount(), 4)
	eq(t, "severities", []string{r.Issues[0].Severity, r.Issues[1].Severity}, []string{"medium", "low"})
	eq(t, "issue", r.Issues[0], Issue{"S1", "internal/a.go", 12, "medium", "security", "thiếu kiểm tra nil"})
}

func TestRejectsUnknownSeverity(t *testing.T) {
	wantErr(t, writeReview(t, reviewJSON(fiveCriteria("DAT"), issue("critical", "naming"))), "severity", "critical")
}

func TestRejectsMissingCriterion(t *testing.T) {
	four := strings.Join([]string{criterion("sequence", "DAT"), criterion("error-handling", "DAT"),
		criterion("over-engineering", "DAT"), criterion("naming", "DAT")}, ",")
	wantErr(t, writeReview(t, reviewJSON(four, "")), "security")
}

func TestRejectsDuplicateCriterion(t *testing.T) {
	wantErr(t, writeReview(t, reviewJSON(fiveCriteria("DAT")+","+criterion("naming", "DAT"), "")), "naming")
}

func TestRejectsUnknownLabel(t *testing.T) {
	wantErr(t, writeReview(t, reviewJSON(fiveCriteria("TOT"), "")), "TOT")
}

func TestRejectsEmptyReasonAndUnknownCategory(t *testing.T) {
	empty := strings.Replace(fiveCriteria("DAT"), "lý do naming", " ", 1)
	wantErr(t, writeReview(t, reviewJSON(empty, "")), "reason")
	wantErr(t, writeReview(t, reviewJSON(fiveCriteria("DAT"), issue("low", "style"))), "style")
}

func TestRejectsMalformedJson(t *testing.T) {
	wantErr(t, writeReview(t, "{không phải json"), "review-agent.json không phải JSON hợp lệ")
}

func TestNullDocumentIsEmpty(t *testing.T) {
	wantErr(t, writeReview(t, "null"), "review-agent.json rỗng.")
}
