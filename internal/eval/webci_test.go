package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/risk"
)

// CI chế độ web: approval lấy từ nguồn web (CIApprovalSource), không từ gates.json.

type srcFunc func(ctx context.Context, key model.FeatureKey, tree string) (CIApprovals, error)

func (f srcFunc) Approvals(ctx context.Context, key model.FeatureKey, tree string) (CIApprovals, error) {
	return f(ctx, key, tree)
}

func fixed(a CIApprovals) srcFunc {
	return func(context.Context, model.FeatureKey, string) (CIApprovals, error) { return a, nil }
}

func webOf(a map[gate.Gate]gate.Approval, trees map[gate.Gate]string) CIApprovals {
	return CIApprovals{Mode: "web", Pinned: true, Approvals: a, EvaluatedTree: trees}
}

func (e *evalEnv) appr(g gate.Gate) gate.Approval {
	e.t.Helper()
	h, err := e.gates().CurrentHash(g)
	e.must(err)
	return gate.Approval{Gate: g, Hash: h, Approver: "dev", At: clock()}
}

func (e *evalEnv) serverAuto(g gate.Gate) gate.Approval {
	a := e.appr(g)
	a.Approver, a.Rule = gate.ApprovalAuto, "flow:low"
	return a
}

func (e *evalEnv) commitWebConfig() {
	e.t.Helper()
	e.config("unmanaged: [\"*.md\"]\napprovals: web\n")
	e.sh("add", "aisdlc/config.yaml")
	e.sh("commit", "-q", "-m", "web mode")
}

func (e *evalEnv) runWith(src CIApprovalSource) EvalReport {
	e.t.Helper()
	r, err := e.service().Run(context.Background(), true, "HEAD", src)
	e.must(err)
	return r
}

func (e *evalEnv) webCi(src CIApprovalSource) EvalReport {
	e.t.Helper()
	e.commitWebConfig()
	return e.runWith(src)
}

func TestEvalServerPinnedLocalWinsOverConfigWeb(t *testing.T) {
	// lead chuyển repo về local trên web: CI theo server, kiểm gates.json như chế độ local
	e := newEvalEnv(t)
	r := e.webCi(fixed(CIApprovals{Mode: "local", Pinned: true}))
	if has(hardIDs(r), is("GATE:WEB")) || has(hardIDs(r), is("GATE:CASES")) {
		t.Errorf("hard %v", hardIDs(r))
	}
	eq(t, "locked", mustSoft(t, r, "CASES_LOCKED").Level, SoftOK)
	if !r.Passed {
		t.Errorf("hard %+v", r.Hard)
	}
}

func TestEvalServerNotPinnedAndConfigWebFailsClosed(t *testing.T) {
	// server chưa có bản ghi chế độ (trả local mặc định) mà config.yaml nói web: vẫn là web, chưa có approval
	e := newEvalEnv(t)
	r := e.webCi(fixed(CIApprovals{Mode: "local", Pinned: false}))
	eq(t, "cases", hardOf(t, r, "GATE:CASES").Passed, false)
	eq(t, "passed", r.Passed, false)
}

func TestEvalServerNotPinnedAndConfigLocalStaysLocal(t *testing.T) {
	e := newEvalEnv(t)
	r := e.runWith(fixed(CIApprovals{Mode: "local", Pinned: false}))
	if has(hardIDs(r), prefix("GATE:")) {
		t.Errorf("hard %v", hardIDs(r))
	}
	if !r.Passed {
		t.Errorf("hard %+v", r.Hard)
	}
}

func TestEvalWebModeMissingGateIsHardFail(t *testing.T) {
	e := newEvalEnv(t)
	r := e.webCi(fixed(webOf(map[gate.Gate]gate.Approval{gate.CASES: e.appr(gate.CASES)}, nil)))
	eq(t, "review", hardOf(t, r, "GATE:REVIEW"), HardCheck{"GATE:REVIEW", false, "chưa duyệt REVIEW trên web"})
	eq(t, "cases", hardOf(t, r, "GATE:CASES").Passed, true)
	if has(hardIDs(r), is("CASES_LOCKED")) {
		t.Errorf("hard %v", hardIDs(r))
	}
	eq(t, "passed", r.Passed, false)
}

func TestEvalWebModeHashMismatchIsHardFail(t *testing.T) {
	e := newEvalEnv(t)
	stale := gate.Approval{Gate: gate.CASES, Hash: "0000000000000000000000000000000000000000000000000000000000000000",
		Approver: "dev", At: clock()}
	r := e.webCi(fixed(webOf(map[gate.Gate]gate.Approval{gate.CASES: stale}, nil)))
	eq(t, "cases", hardOf(t, r, "GATE:CASES"), HardCheck{"GATE:CASES", false, "CASES đã đổi sau khi duyệt"})
	eq(t, "passed", r.Passed, false)
}

func TestEvalWebModeReviewTreeMismatchIsHardFail(t *testing.T) {
	e := newEvalEnv(t)
	e.write("aisdlc/features/PAY-123/review-agent.json", reviewJSON(fiveCriteria("DAT"), ""))
	r := e.webCi(fixed(webOf(
		map[gate.Gate]gate.Approval{gate.CASES: e.appr(gate.CASES), gate.REVIEW: e.appr(gate.REVIEW)},
		map[gate.Gate]string{gate.REVIEW: "deadbeef"})))
	eq(t, "review", hardOf(t, r, "GATE:REVIEW").Detail, "REVIEW đã đổi sau duyệt, chạy lại aisdlc eval rồi duyệt lại")
	eq(t, "passed", r.Passed, false)
}

func TestEvalWebModeAllApprovedAndMatchingPasses(t *testing.T) {
	e := newEvalEnv(t)
	e.write("aisdlc/features/PAY-123/review-agent.json", reviewJSON(fiveCriteria("DAT"), ""))
	r := e.webCi(srcFunc(func(_ context.Context, key model.FeatureKey, tree string) (CIApprovals, error) {
		eq(t, "feature", key, model.FeatureKey("PAY-123"))
		eq(t, "tree", tree, e.tree())
		return webOf(map[gate.Gate]gate.Approval{gate.CASES: e.appr(gate.CASES), gate.REVIEW: e.appr(gate.REVIEW)},
			map[gate.Gate]string{gate.REVIEW: e.tree()}), nil
	}))
	if !has(hardIDs(r), is("GATE:CASES")) || !has(hardIDs(r), is("GATE:REVIEW")) {
		t.Errorf("hard %v", hardIDs(r))
	}
	if !r.Passed {
		t.Errorf("hard %+v", r.Hard)
	}
	if has(softIDs(r), prefix("GATE:")) {
		t.Errorf("soft %v", softIDs(r))
	}
}

func TestEvalWebModeIgnoresLocalGatesJson(t *testing.T) {
	// gates.json có CASES duyệt, nhưng web không có gì: vẫn FAIL
	e := newEvalEnv(t)
	r := e.webCi(fixed(webOf(nil, nil)))
	eq(t, "cases", hardOf(t, r, "GATE:CASES").Passed, false)
}

func TestEvalWebModeUnreachableIsHardFail(t *testing.T) {
	e := newEvalEnv(t)
	r := e.webCi(srcFunc(func(context.Context, model.FeatureKey, string) (CIApprovals, error) {
		return CIApprovals{}, errors.New("HTTP 401")
	}))
	eq(t, "web", hardOf(t, r, "GATE:WEB"), HardCheck{"GATE:WEB", false, "Không hỏi được web (HTTP 401), chạy lại job"})
	eq(t, "passed", r.Passed, false)
}

func TestEvalWebReportedModeWebOverridesLocalConfig(t *testing.T) {
	e := newEvalEnv(t)
	r := e.runWith(fixed(webOf(nil, nil)))
	eq(t, "cases", hardOf(t, r, "GATE:CASES").Passed, false)
}

func TestEvalSourceFailureIsHardFailEvenWhenConfigSaysLocal(t *testing.T) {
	// config.yaml của đầu MR không được tắt kiểm tra web: có nguồn thì lỗi hỏi web luôn là FAIL
	e := newEvalEnv(t)
	r := e.runWith(srcFunc(func(context.Context, model.FeatureKey, string) (CIApprovals, error) {
		return CIApprovals{}, errors.New("HTTP 403")
	}))
	eq(t, "web", hardOf(t, r, "GATE:WEB"), HardCheck{"GATE:WEB", false, "Không hỏi được web (HTTP 403), chạy lại job"})
	eq(t, "passed", r.Passed, false)
}

// Nguồn panic (RuntimeException của Java) cũng là hard fail, không làm sập eval.
func TestEvalSourceRuntimeFailureIsHardFail(t *testing.T) {
	e := newEvalEnv(t)
	r := e.runWith(srcFunc(func(context.Context, model.FeatureKey, string) (CIApprovals, error) {
		panic("boom")
	}))
	eq(t, "web", hardOf(t, r, "GATE:WEB").Passed, false)
	eq(t, "passed", r.Passed, false)
}

func TestEvalWebModeFullFlowMissingDesignIsHardFail(t *testing.T) {
	e := newEvalEnv(t)
	e.flow = model.FlowFull
	e.full("steps: []\n")
	r := e.webCi(fixed(webOf(nil, nil)))
	eq(t, "design", hardOf(t, r, "GATE:DESIGN").Detail, "chưa duyệt DESIGN trên web")
	eq(t, "passed", r.Passed, false)
}

// ---- CiAutoApprovalTest, chế độ web ----

func (e *evalEnv) riskFlow(level string, flow map[string]string) {
	e.t.Helper()
	e.must(risk.RiskFile{Level: level, Policy: risk.PolicyRef{Version: 2, Hash: "h"},
		Matched: []risk.Match{{Rule: "r1", Level: level, On: "x"}}, Requirements: []risk.Requirement{}, Flow: flow}.
		Write(e.paths.Risk()))
}

func TestCiAutoApprovalWebMatchingFlowPass(t *testing.T) {
	e := newEvalEnv(t)
	e.riskFlow("low", map[string]string{"brd": "auto", "design": "auto", "cases": "auto", "step": "auto"})
	ci := e.webCi(srcFunc(func(context.Context, model.FeatureKey, string) (CIApprovals, error) {
		return webOf(map[gate.Gate]gate.Approval{gate.CASES: e.serverAuto(gate.CASES)}, nil), nil
	}))
	c := hardOf(t, ci, "GATE_AUTO:CASES")
	if !c.Passed {
		t.Errorf("%+v", c)
	}
	wantContains(t, "detail", c.Detail, "tự duyệt (mức low)")
	eq(t, "cases", hardOf(t, ci, "GATE:CASES").Passed, true)
}

func TestCiAutoApprovalWebNotMatchingFail(t *testing.T) {
	e := newEvalEnv(t)
	e.riskFlow("high", map[string]string{"brd": "manual", "design": "manual", "cases": "manual", "step": "manual"})
	ci := e.webCi(srcFunc(func(context.Context, model.FeatureKey, string) (CIApprovals, error) {
		return webOf(map[gate.Gate]gate.Approval{gate.CASES: e.serverAuto(gate.CASES)}, nil), nil
	}))
	eq(t, "auto", hardOf(t, ci, "GATE_AUTO:CASES").Passed, false)
	eq(t, "passed", ci.Passed, false)
}

func TestCiAutoApprovalWebManualApprovalNoAutoCheck(t *testing.T) {
	e := newEvalEnv(t)
	ci := e.webCi(fixed(webOf(map[gate.Gate]gate.Approval{gate.CASES: e.appr(gate.CASES)}, nil)))
	if has(hardIDs(ci), prefix("GATE_AUTO")) {
		t.Errorf("hard %v", hardIDs(ci))
	}
}

// ---- carry-over giai đoạn 3: hủy giữa lúc chạy lệnh checks ----

// Ctrl-C giữa lúc một lệnh checks đang chạy: lệnh bị kill do ctx, Run trả lỗi ctx, không ghi report, không báo
// "quá thời gian".
func TestEvalCanceledDuringChecksWritesNoReport(t *testing.T) {
	e := newEvalEnv(t)
	e.config("unmanaged: [\"*.md\"]\nchecks:\n  - { name: slow, cmd: \"sleep 30\" }\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(300*time.Millisecond, cancel)

	start := time.Now()
	_, err := e.service().Run(ctx, false, "", nil)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Errorf("chờ %v: lệnh check không bị kill", d)
	}
	if _, err := os.Stat(filepath.Join(e.paths.EvalDir(), "report.json")); err == nil {
		t.Error("không được ghi report.json khi bị hủy")
	}
}

// Một check bị hủy được ghi là bị hủy, không phải "quá thời gian".
func TestEvalCheckCanceledIsNotReportedAsTimeout(t *testing.T) {
	e := newEvalEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := e.service().check(ctx, model.CheckCommand{Name: "slow", Cmd: "sleep 30"}, e.t.TempDir())
	eq(t, "passed", c.Passed, false)
	wantContains(t, "detail", c.Detail, "bị hủy")
	if got := c.Detail; got != "" && strings.Contains(got, "quá thời gian") {
		t.Errorf("detail %q", got)
	}
}
