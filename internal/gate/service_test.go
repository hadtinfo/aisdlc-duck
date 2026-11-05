package gate

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/model"
	"aisdlc/internal/spec"
)

type fx struct {
	t   *testing.T
	p   model.FeaturePaths
	cfg model.RepoConfig
}

func setup(t *testing.T) *fx {
	t.Helper()
	p := newPaths(t, "PAY-123")
	cfg, err := model.LoadRepoConfig(p.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	write(t, p.Brd(), "# BRD\n")
	write(t, p.DesignMd(), "# Design\n")
	write(t, p.DesignYaml(), "steps: []\n")
	write(t, p.Cases(), "cases: []\n")
	write(t, p.SchemaExpected(), "{}\n")
	return &fx{t, p, cfg}
}

func (f *fx) service(flow model.Flow) *Service {
	return NewService(f.p, flow, f.cfg, NewLocalGateStore(f.p), func() time.Time { return t0 })
}

func (f *fx) mustState(s *Service, g Gate) GateState {
	f.t.Helper()
	st, err := s.State(g)
	if err != nil {
		f.t.Fatal(err)
	}
	return st
}

func (f *fx) approveClean(s *Service, g Gate) {
	f.t.Helper()
	if _, err := s.Submit(g, nil); err != nil {
		f.t.Fatal(err)
	}
	if _, err := s.Approve(g, "dev1", ""); err != nil {
		f.t.Fatal(err)
	}
}

func isGateErr(err error) bool {
	var e *Error
	return errors.As(err, &e)
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func eqState(t *testing.T, got, want GateState) {
	t.Helper()
	if got != want {
		t.Errorf("state %s, want %s", got, want)
	}
}

func TestFullFlowOrderEnforced(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	eqState(t, f.mustState(s, DESIGN), LockedByPrevious)
	if _, err := s.Submit(DESIGN, nil); !isGateErr(err) {
		t.Errorf("phải lỗi GateException: %v", err)
	}
	g, ok, err := s.FirstNotOpen()
	if err != nil || !ok || g != BRD {
		t.Errorf("%v %v %v", g, ok, err)
	}
}

func TestApproveRequiresCleanSubmission(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	if _, err := s.Approve(BRD, "dev1", ""); !isGateErr(err) || err.Error() != "Cổng BRD chưa được nộp." {
		t.Errorf("%v", err)
	}
	s.Submit(BRD, []spec.Finding{{Code: "X", Message: "thiếu mục"}})
	eqState(t, f.mustState(s, BRD), SubmittedWithFindings)
	if _, err := s.Approve(BRD, "dev1", ""); err == nil || err.Error() != "Cổng BRD còn 1 phát hiện chưa xử lý." {
		t.Errorf("%v", err)
	}
	s.Submit(BRD, nil)
	eqState(t, f.mustState(s, BRD), AwaitingApproval)
	if _, err := s.Approve(BRD, "dev1", ""); err != nil {
		t.Fatal(err)
	}
	eqState(t, f.mustState(s, BRD), Approved)
	if !must[bool](t)(s.IsOpen(BRD)) {
		t.Error("IsOpen")
	}
}

func TestApproveRejectsIfChangedSinceSubmit(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	s.Submit(BRD, nil)
	write(t, f.p.Brd(), "# BRD sửa\n")
	eqState(t, f.mustState(s, BRD), NotSubmitted)
	if _, err := s.Approve(BRD, "dev1", ""); !isGateErr(err) ||
		err.Error() != "Artifact của cổng BRD đã đổi kể từ lúc nộp; hãy nộp lại." {
		t.Errorf("%v", err)
	}
}

func TestEditAfterApprovalMakesStaleAndClosesDownstream(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	f.approveClean(s, BRD)
	f.approveClean(s, DESIGN)
	f.approveClean(s, CASES)
	if !must[bool](t)(s.CodeReady()) {
		t.Fatal("CodeReady")
	}
	write(t, f.p.Brd(), "# BRD đã sửa\n")
	eqState(t, f.mustState(s, BRD), Stale)
	eqState(t, f.mustState(s, DESIGN), LockedByPrevious)
	eqState(t, f.mustState(s, CASES), LockedByPrevious)
	if must[bool](t)(s.IsOpen(DESIGN)) || must[bool](t)(s.CodeReady()) {
		t.Error("phải đóng")
	}
	if g, ok, _ := s.FirstNotOpen(); !ok || g != BRD {
		t.Errorf("%v", g)
	}
}

func TestLiteFlowStartsAtCases(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowLite)
	if !reflect.DeepEqual(Sequence(model.FlowLite), []Gate{CASES, REVIEW}) {
		t.Fatal("Sequence")
	}
	s.Submit(CASES, nil)
	if must[bool](t)(s.CodeReady()) {
		t.Fatal("chưa duyệt")
	}
	s.Approve(CASES, "dev1", "")
	if !must[bool](t)(s.CodeReady()) {
		t.Fatal("đã duyệt")
	}
}

func TestRejectWritesFeedbackAndClears(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowLite)
	s.Submit(CASES, nil)
	if err := s.Reject(CASES, "dev1", "Thiếu case lỗi 404"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f.p.Feedback())
	fb := string(b)
	if !strings.Contains(fb, "## CASES — trả lại bởi dev1 lúc 2026-11-02T03:00:00Z") || !strings.Contains(fb, "Thiếu case lỗi 404") {
		t.Errorf("%q", fb)
	}
	eqState(t, f.mustState(s, CASES), NotSubmitted)
}

func TestReopenClearsAndLogs(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowLite)
	f.approveClean(s, CASES)
	if err := s.Reopen(CASES, "dev1"); err != nil {
		t.Fatal(err)
	}
	eqState(t, f.mustState(s, CASES), NotSubmitted)
	b, _ := os.ReadFile(f.p.Feedback())
	if !strings.Contains(string(b), "CASES") || !strings.Contains(string(b), "dev1") {
		t.Errorf("%q", b)
	}
}

func TestGatesJsonSurvivesReload(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	f.approveClean(s, BRD)
	reloaded := NewLocalGateStore(f.p)
	a, ok, err := reloaded.Approval(BRD)
	if err != nil || !ok || a.Approver != "dev1" || !a.At.Equal(t0) {
		t.Errorf("%+v %v %v", a, ok, err)
	}
	eqState(t, f.mustState(f.service(model.FlowFull), BRD), Approved)
}

type countingStore struct {
	real                   *LocalGateStore
	subs, apprs, refreshes int
}

func (c *countingStore) Refresh() error { c.refreshes++; return c.real.Refresh() }
func (c *countingStore) Submission(g Gate) (Submission, bool, error) {
	c.subs++
	return c.real.Submission(g)
}
func (c *countingStore) SaveSubmission(s Submission) error { return c.real.SaveSubmission(s) }
func (c *countingStore) Approval(g Gate) (Approval, bool, error) {
	c.apprs++
	return c.real.Approval(g)
}
func (c *countingStore) SaveApproval(a Approval) error { return c.real.SaveApproval(a) }
func (c *countingStore) Clear(g Gate) error            { return c.real.Clear(g) }

func TestStateReadsStoreBoundedTimes(t *testing.T) {
	f := setup(t)
	for _, g := range allGates {
		f.approveClean(f.service(model.FlowFull), g)
	}
	cs := &countingStore{real: NewLocalGateStore(f.p)}
	s := NewService(f.p, model.FlowFull, f.cfg, cs, func() time.Time { return t0 })
	eqState(t, f.mustState(s, REVIEW), Approved)
	if cs.apprs > 4 {
		t.Errorf("approval đọc %d lần", cs.apprs)
	}
	if cs.subs != 0 {
		t.Errorf("submission đọc %d lần", cs.subs)
	}
	if cs.refreshes != 1 {
		t.Errorf("refresh %d lần", cs.refreshes)
	}
}

func TestStaleThenResubmitAndApproveRecovers(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	f.approveClean(s, BRD)
	write(t, f.p.Brd(), "# BRD v2\n")
	eqState(t, f.mustState(s, BRD), Stale)
	s.Submit(BRD, nil)
	s.Approve(BRD, "dev1", "")
	eqState(t, f.mustState(s, BRD), Approved)
}

func TestCorruptGatesJsonGivesVietnameseGateException(t *testing.T) {
	f := setup(t)
	write(t, f.p.Gates(), "{not json")
	_, err := f.service(model.FlowFull).State(BRD)
	if !isGateErr(err) || !strings.Contains(err.Error(), "gates.json hỏng") {
		t.Errorf("%v", err)
	}
}

func TestHasApprovalTrueForApprovedAndStaleOnly(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	has := func(g Gate) bool { return must[bool](t)(s.HasApproval(g)) }
	if has(BRD) {
		t.Fatal("chưa")
	}
	s.Submit(BRD, nil)
	if has(BRD) {
		t.Fatal("mới nộp")
	}
	s.Approve(BRD, "dev1", "")
	if !has(BRD) {
		t.Fatal("approved")
	}
	write(t, f.p.Brd(), "# BRD v2\n")
	eqState(t, f.mustState(s, BRD), Stale)
	if !has(BRD) || has(DESIGN) || s.Flow() != model.FlowFull {
		t.Fatal("stale")
	}
}

func TestAutoApproveRecordsAutoApproverAndRule(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	s.Submit(BRD, nil)
	a, err := s.AutoApprove(BRD, "flow:low")
	if err != nil {
		t.Fatal(err)
	}
	if a.Approver != "auto" || a.Rule != "flow:low" || !a.IsAuto() || a.RuleLevel() != "low" {
		t.Errorf("%+v", a)
	}
	eqState(t, f.mustState(s, BRD), Approved)
	got, ok, _ := f.service(model.FlowFull).Approval(BRD)
	if !ok || got.Hash != a.Hash || got.Rule != a.Rule || got.Approver != a.Approver || !got.At.Equal(a.At) {
		t.Errorf("%+v", got)
	}
}

func TestAutoApproveReviewAlwaysManual(t *testing.T) {
	f := setup(t)
	if _, err := f.service(model.FlowLite).AutoApprove(REVIEW, "flow:low"); !isGateErr(err) ||
		err.Error() != "Cổng REVIEW luôn duyệt tay." {
		t.Errorf("%v", err)
	}
}

func TestAutoApproveNeedsCleanSubmission(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	s.Submit(BRD, []spec.Finding{{Code: "X", Message: "thiếu mục"}})
	if _, err := s.AutoApprove(BRD, "flow:low"); !isGateErr(err) {
		t.Errorf("%v", err)
	}
}

func TestSupersedeAutoGateBackToAwaitingNotCountedAsApproved(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	s.Submit(BRD, nil)
	s.AutoApprove(BRD, "flow:low")
	s.Submit(DESIGN, nil)
	s.AutoApprove(DESIGN, "flow:low")

	got, err := s.SupersedeAuto(BRD, DESIGN)
	if err != nil || !reflect.DeepEqual(got, []Gate{BRD, DESIGN}) {
		t.Fatalf("%v %v", got, err)
	}
	eqState(t, f.mustState(s, BRD), AwaitingApproval)
	eqState(t, f.mustState(s, DESIGN), LockedByPrevious)
	if must[bool](t)(s.HasApproval(BRD)) || must[bool](t)(s.ApprovalMatches(BRD)) || must[bool](t)(s.CodeReady()) {
		t.Error("không tính là đã duyệt")
	}
	if a, ok, _ := s.Approval(BRD); !ok || !a.Superseded {
		t.Errorf("%+v", a)
	}
	// dev duyệt tay lại, không cần nộp lại
	if _, err := s.Approve(BRD, "dev1", ""); err != nil {
		t.Fatal(err)
	}
	eqState(t, f.mustState(s, BRD), Approved)
	if a, _, _ := s.Approval(BRD); a.Approver != "dev1" {
		t.Errorf("%+v", a)
	}
	eqState(t, f.mustState(s, DESIGN), AwaitingApproval)
}

func TestSupersedeAutoLeavesManualApprovals(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	f.approveClean(s, BRD)
	got, err := s.SupersedeAuto(BRD)
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
	eqState(t, f.mustState(s, BRD), Approved)
}

func TestOldGatesJsonWithoutRuleOrSupersededStillReads(t *testing.T) {
	f := setup(t)
	write(t, f.p.Gates(), `{"submissions":{},"approvals":{"BRD":{"gate":"BRD","hash":"h","approver":"dev1",                "at":"2026-11-02T03:00:00Z"}}}
`)
	s := f.service(model.FlowFull)
	a, ok, err := s.Approval(BRD)
	if err != nil || !ok || a.Rule != "" || a.Superseded {
		t.Fatalf("%+v %v %v", a, ok, err)
	}
	if !must[bool](t)(s.HasApproval(BRD)) {
		t.Error("HasApproval")
	}
}

func TestManualApprovalJsonHasNoNewFields(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	f.approveClean(s, BRD)
	b, _ := os.ReadFile(f.p.Gates())
	if strings.Contains(string(b), "rule") || strings.Contains(string(b), "superseded") || strings.Contains(string(b), "channel") {
		t.Errorf("%s", b)
	}
	s.Submit(DESIGN, nil)
	s.AutoApprove(DESIGN, "flow:low")
	s.SupersedeAuto(DESIGN)
	b, _ = os.ReadFile(f.p.Gates())
	if !strings.Contains(string(b), `"rule": "flow:low"`) || !strings.Contains(string(b), `"superseded": true`) ||
		!strings.Contains(string(b), `"at": "2026-11-02T03:00:00Z"`) {
		t.Errorf("%s", b)
	}
}

func TestApproveRecordsChannel(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowFull)
	s.Submit(BRD, nil)
	a, err := s.Approve(BRD, "dev1", ApprovalClaudeCode)
	if err != nil || a.Channel != "claude-code" {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestGateOutsideFlowIsError(t *testing.T) {
	f := setup(t)
	_, err := f.service(model.FlowLite).State(BRD)
	if !isGateErr(err) || err.Error() != "Cổng BRD không thuộc luồng LITE." {
		t.Errorf("%v", err)
	}
}

func TestCasesLockedOnWhitespaceEdit(t *testing.T) {
	f := setup(t)
	s := f.service(model.FlowLite)
	f.approveClean(s, CASES)
	if !must[bool](t)(s.ApprovalMatches(CASES)) {
		t.Fatal("phải khớp sau duyệt")
	}
	b, _ := os.ReadFile(f.p.Cases())
	write(t, f.p.Cases(), string(b)+" ")
	if must[bool](t)(s.ApprovalMatches(CASES)) {
		t.Error("thêm dấu cách phải làm lệch hash")
	}
}

func TestRiskYamlEditBreaksCasesApproval(t *testing.T) {
	f := setup(t)
	write(t, f.p.Risk(), "level: low\n")
	s := f.service(model.FlowLite)
	f.approveClean(s, CASES)
	if !must[bool](t)(s.ApprovalMatches(CASES)) {
		t.Fatal("phải khớp sau duyệt")
	}
	write(t, f.p.Risk(), "level: high\n")
	if must[bool](t)(s.ApprovalMatches(CASES)) {
		t.Error("sửa risk.yaml phải làm lệch hash")
	}
	eqState(t, f.mustState(s, CASES), Stale)
}
