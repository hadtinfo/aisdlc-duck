package eval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/risk"
	"aisdlc/internal/run"
	"aisdlc/internal/step"
)

var t0 = time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)

func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v", name, got, want)
	}
}

func softOf(r EvalReport, id string) SoftCheck {
	for _, s := range r.Soft {
		if s.ID == id {
			return s
		}
	}
	return SoftCheck{}
}

func failedCases(n int) []run.CaseResult {
	out := make([]run.CaseResult, n)
	for i := range out {
		out[i] = run.CaseResult{ID: fmt.Sprintf("C%d", i), Step: "S1", Outcome: run.CaseOutcomeFailed, Message: "mong đợi 200"}
	}
	return out
}

func TestAgentReviewSeriousCriterionWarns(t *testing.T) {
	p := writeReview(t, reviewJSON(fiveCriteria("NGHIEM_TRONG"), issue("medium", "security")))
	eq(t, "check", AgentReviewCheck(p), SoftCheck{"AGENT_REVIEW", "WARN", "4/5 ĐẠT, 1 NGHIÊM_TRỌNG (security); 0 high, 1 medium"})
}

func TestAgentReviewAllGoodIsOk(t *testing.T) {
	criteria := strings.Replace(fiveCriteria("DAT"), `"id":"error-handling","label":"DAT"`, `"id":"error-handling","label":"CAN_SUA"`, 1)
	p := writeReview(t, reviewJSON(criteria, issue("medium", "error-handling")+","+issue("medium", "other")))
	eq(t, "check", AgentReviewCheck(p), SoftCheck{"AGENT_REVIEW", "OK", "4/5 ĐẠT, 1 CẦN_SỬA (error-handling); 0 high, 2 medium"})
}

func TestAgentReviewMissingFileIsNaAndInvalidIsWarn(t *testing.T) {
	eq(t, "missing", AgentReviewCheck(filepath.Join(t.TempDir(), "x.json")), SoftCheck{"AGENT_REVIEW", "NA", "chưa có review-agent.json"})
	got := AgentReviewCheck(writeReview(t, "{không phải json"))
	eq(t, "level", got.Level, "WARN")
}

func sampleReport(notRisk bool) EvalReport {
	hard := []HardCheck{{"CASES_ALL_PASS", true, "3/3"}, {"SCOPE", true, ""}}
	soft := []SoftCheck{{"COVERAGE_CHANGED", "OK", "90%"}, {"AGENT_REVIEW", "NA", "chưa có review-agent.json"}}
	var rs *RiskSection
	if !notRisk {
		rs = &RiskSection{Level: "high", PolicyVersion: 3, Matched: []string{"r1"},
			Requirements: []risk.RequirementStatus{{ID: "coverage", Name: "Coverage", Status: risk.RequirementMissing, Detail: "thiếu"}}}
	}
	return NewEvalReport("PAY-123", t0, "local", hard, soft, "abc", rs)
}

func featurePaths(t *testing.T) model.FeaturePaths {
	return model.FeaturePaths{RepoRoot: t.TempDir(), Key: "PAY-123"}
}

func TestRefreshAgentReviewRewritesOnlyThatRowWithoutMaven(t *testing.T) {
	p := featurePaths(t)
	before := sampleReport(true)
	jsonPath := filepath.Join(p.EvalDir(), "report.json")
	mdPath := filepath.Join(p.EvalDir(), "report.md")
	if err := before.Write(jsonPath); err != nil {
		t.Fatal(err)
	}
	mdBefore := RenderReport(before, nil)
	if err := os.WriteFile(mdPath, []byte(mdBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ReviewAgent(), []byte(reviewJSON(fiveCriteria("NGHIEM_TRONG"), issue("high", "security"))), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RefreshAgentReview(p); err != nil {
		t.Fatal(err)
	}

	after, err := ReadEvalReport(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "agent row", softOf(after, "AGENT_REVIEW"), SoftCheck{"AGENT_REVIEW", "WARN", "4/5 ĐẠT, 1 NGHIÊM_TRỌNG (security); 1 high, 0 medium"})
	eq(t, "hard", after.Hard, before.Hard)
	eq(t, "soft ids", []string{after.Soft[0].ID, after.Soft[1].ID}, []string{"COVERAGE_CHANGED", "AGENT_REVIEW"})
	eq(t, "tree", after.EvaluatedTree, before.EvaluatedTree)
	eq(t, "at", after.At, before.At)
	eq(t, "passed", after.Passed, before.Passed)
	b, _ := os.ReadFile(mdPath)
	md := string(b)
	if !strings.Contains(md, "| AGENT_REVIEW | WARN | 4/5 ĐẠT, 1 NGHIÊM_TRỌNG (security); 1 high, 0 medium |") ||
		strings.Contains(md, "chưa có review-agent.json") {
		t.Errorf("report.md chưa cập nhật:\n%s", md)
	}
	eq(t, "line count", strings.Count(md, "\n"), strings.Count(mdBefore, "\n"))
}

func TestRefreshAgentReviewWithoutReportDoesNothing(t *testing.T) {
	p := featurePaths(t)
	if err := RefreshAgentReview(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.EvalDir()); err == nil {
		t.Error("không có report.json thì không được tạo gì")
	}
}

func TestRefreshAgentReviewAppendsRowWhenAbsent(t *testing.T) {
	p := featurePaths(t)
	r := sampleReport(true)
	r.Soft = r.Soft[:1]
	if err := r.Write(filepath.Join(p.EvalDir(), "report.json")); err != nil {
		t.Fatal(err)
	}
	if err := RefreshAgentReview(p); err != nil {
		t.Fatal(err)
	}
	got, _ := ReadEvalReport(filepath.Join(p.EvalDir(), "report.json"))
	eq(t, "agent row", softOf(got, "AGENT_REVIEW").Level, "NA")
	eq(t, "soft count", len(got.Soft), 2)
}

func TestSoftRowInsertedIntoFullReportStaysUnder40Lines(t *testing.T) {
	var hard []HardCheck
	for i := 0; i < 8; i++ {
		hard = append(hard, HardCheck{fmt.Sprintf("CHECK:c%d", i), false, "exit 1"})
	}
	soft := []SoftCheck{{"COVERAGE_CHANGED", "OK", "90%"}}
	md := RenderReport(NewEvalReport("PAY-123", t0, "local", hard, soft, "abc", nil), failedCases(60))

	out := WithSoftRow(md, SoftCheck{"AGENT_REVIEW", "OK", "5/5 ĐẠT"})

	if n := len(splitLines(out)); n > 40 {
		t.Errorf("%d dòng", n)
	}
	if !strings.Contains(out, "| COVERAGE_CHANGED | OK | 90% |\n| AGENT_REVIEW | OK | 5/5 ĐẠT |\n") {
		t.Errorf("chưa chèn đúng chỗ:\n%s", out)
	}
}

func TestReportMarkdownUnder40Lines(t *testing.T) {
	var hard []HardCheck
	var soft []SoftCheck
	for i := 0; i < 8; i++ {
		hard = append(hard, HardCheck{fmt.Sprintf("CHECK:c%d", i), i%2 == 0, "exit 1\nchi tiết | dài"})
	}
	for i := 0; i < 6; i++ {
		soft = append(soft, SoftCheck{fmt.Sprintf("BUDGET:S%d", i), "WARN", "12/5 file"})
	}
	r := NewEvalReport("PAY-123", t0, "local", hard, soft, "abc", nil)

	md := RenderReport(r, failedCases(60))

	lines := splitLines(md)
	if len(lines) > 40 {
		t.Errorf("%d dòng", len(lines))
	}
	eq(t, "title", lines[0], "Eval PAY-123: KHÔNG ĐẠT")
	for _, w := range []string{"| CHECK:c7 |", "| BUDGET:S5 |", "C0", "case khác", `exit 1 chi tiết \| dài`} {
		if !strings.Contains(md, w) {
			t.Errorf("thiếu %q", w)
		}
	}
}

func TestReportHasRiskSection(t *testing.T) {
	reqs := []risk.RequirementStatus{
		{ID: "coverage", Name: "Coverage dòng mới tối thiểu 90%", Status: risk.RequirementOK},
		{ID: "concurrent", Name: "Case đồng thời 4 luồng", Status: risk.RequirementMissing, Detail: "chưa có case"},
	}
	section := &RiskSection{Level: "high", PolicyVersion: 3, Matched: []string{"r1"}, Requirements: reqs}
	md := RenderReport(NewEvalReport("PAY-123", t0, "local", nil, nil, "abc", section), nil)

	for _, w := range []string{"## Rủi ro", "high", "Coverage dòng mới tối thiểu 90%", "Case đồng thời 4 luồng", "chưa có case"} {
		if !strings.Contains(md, w) {
			t.Errorf("thiếu %q", w)
		}
	}
	if strings.Index(md, "## Rủi ro") > strings.Index(md, "| Kiểm tra cứng |") {
		t.Error("mục rủi ro phải trước bảng kiểm tra cứng")
	}
	if n := len(splitLines(md)); n > 40 {
		t.Errorf("%d dòng", n)
	}
}

func TestReportJsonRoundTripAndNoRiskKey(t *testing.T) {
	dir := t.TempDir()
	plain := sampleReport(true)
	f := filepath.Join(dir, "eval", "report.json")
	if err := plain.Write(f); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f)
	if strings.Contains(string(b), `"risk"`) {
		t.Errorf("report không có risk thì không được có khóa risk: %s", b)
	}
	if !strings.Contains(string(b), `"at": "2026-11-02T03:00:00Z"`) || !strings.HasSuffix(string(b), "}\n") {
		t.Errorf("định dạng: %s", b)
	}
	got, err := ReadEvalReport(f)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "plain", got, plain)
	withRisk := sampleReport(false)
	if err := withRisk.Write(f); err != nil {
		t.Fatal(err)
	}
	got, _ = ReadEvalReport(f)
	eq(t, "risk", got.Risk, withRisk.Risk)
	if _, err := ReadEvalReport(filepath.Join(dir, "none.json")); err == nil || !strings.HasPrefix(err.Error(), "Không đọc được ") {
		t.Errorf("lỗi %v", err)
	}
}

func TestConfigChangedKeepsJavaWording(t *testing.T) {
	eq(t, "CONFIG_CHANGED", ConfigChangedText(), " bị sửa trong thay đổi này — sửa cấu hình phải ở MR riêng")
}

func TestDiffByStep(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.name", "T"}, {"config", "user.email", "t@e.com"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	repo := gitx.New(dir)
	ctx := context.Background()
	snap := func() string {
		s, err := repo.SnapshotTree(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	put := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t1 := snap()
	put("a.txt", "một\nhai\n")
	t2 := snap()
	put("b.txt", "ba\n")
	records := []step.StepRecord{
		{ID: "S1", Status: step.StepAccepted, StartTree: t1, EndTree: t2},
		{ID: "S2", Status: step.StepInProgress, StartTree: t2},
	}
	records[0].Changes, _ = repo.Diff(ctx, t1, t2)

	out, err := DiffByStep(records, map[string]string{"S1": "Bước một", "S2": "Bước hai"}, repo, false)
	if err != nil {
		t.Fatal(err)
	}
	want := "## S1 — Bước một\n\n2\t0\ta.txt\n\n## S2 — Bước hai\n\n1\t0\tb.txt\n\n"
	eq(t, "diff", out, want)

	withPatch, err := DiffByStep(records[:1], map[string]string{"S1": "Bước một"}, repo, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withPatch, "\n```diff\n") || !strings.Contains(withPatch, "+một") || !strings.HasSuffix(withPatch, "```\n\n") {
		t.Errorf("patch:\n%s", withPatch)
	}
}
