package eval

import (
	"os"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/risk"
)

// CI chế độ local: bản tự duyệt trong gates.json phải khớp flow chụp trong risk.yaml (spec risk-flow §5.1).
// Ba test chế độ web của CiAutoApprovalTest (webCi_*) ở webci_test.go (TestCiAutoApprovalWeb*).

// autoCases ghi risk.yaml rồi nộp lại cổng Case (risk.yaml nằm trong hash) và tự duyệt nó.
func (e *evalEnv) autoCases(level string, flow map[string]string) {
	e.t.Helper()
	e.must(risk.RiskFile{Level: level, Policy: risk.PolicyRef{Version: 2, Hash: "h"},
		Matched: []risk.Match{{Rule: "r1", Level: level, On: "x"}}, Requirements: []risk.Requirement{}, Flow: flow}.
		Write(e.paths.Risk()))
	g := e.gates()
	_, err := g.Submit(gate.CASES, nil)
	e.must(err)
	_, err = g.AutoApprove(gate.CASES, "flow:low")
	e.must(err)
}

func TestCiAutoApprovalMatchingFlowPass(t *testing.T) {
	e := newEvalEnv(t)
	e.autoCases("low", map[string]string{"brd": "auto", "design": "auto", "cases": "auto", "step": "auto"})

	ci := e.run(true)

	eq(t, "check", hardOf(t, ci, "GATE_AUTO:CASES"),
		HardCheck{"GATE_AUTO:CASES", true, "tự duyệt (mức low), khớp flow trong risk.yaml"})
	if !ci.Passed {
		t.Errorf("hard %+v", ci.Hard)
	}
}

func TestCiAutoApprovalNotMatchingFail(t *testing.T) {
	e := newEvalEnv(t)
	e.autoCases("high", map[string]string{"brd": "manual", "design": "manual", "cases": "manual", "step": "manual"})

	ci := e.run(true)

	eq(t, "check", hardOf(t, ci, "GATE_AUTO:CASES"),
		HardCheck{"GATE_AUTO:CASES", false, "Bản tự duyệt CASES không khớp flow trong risk.yaml"})
	eq(t, "passed", ci.Passed, false)
}

func TestCiAutoApprovalWithoutRiskYamlFail(t *testing.T) {
	e := newEvalEnv(t)
	e.autoCases("low", map[string]string{"cases": "auto"})
	// gates.json đã ghi bản tự duyệt; risk.yaml bị xóa sau đó
	e.must(os.Remove(e.paths.Risk()))

	ci := e.run(true)

	eq(t, "passed", hardOf(t, ci, "GATE_AUTO:CASES").Passed, false)
}

func TestCiManualApprovalsNoAutoCheck(t *testing.T) {
	e := newEvalEnv(t)
	if ids := hardIDs(e.run(true)); has(ids, prefix("GATE_AUTO")) {
		t.Errorf("hard %v", ids)
	}
}

func TestLocalEvalDoesNotCheckAutoApprovals(t *testing.T) {
	e := newEvalEnv(t)
	e.autoCases("high", map[string]string{"cases": "manual"})
	if ids := hardIDs(e.run(false)); has(ids, prefix("GATE_AUTO")) {
		t.Errorf("hard %v", ids)
	}
}

func TestAutoApprovalsSkipsSupersededAndManual(t *testing.T) {
	approvals := map[gate.Gate]gate.Approval{
		gate.BRD:    {Gate: gate.BRD, Approver: gate.ApprovalAuto, Superseded: true},
		gate.DESIGN: {Gate: gate.DESIGN, Approver: "dev"},
	}
	got := AutoApprovals(gate.Sequence("FULL"), func(g gate.Gate) (gate.Approval, bool) {
		a, ok := approvals[g]
		return a, ok
	}, "/không/có/risk.yaml")
	if len(got) != 0 {
		t.Errorf("%+v", got)
	}
}
