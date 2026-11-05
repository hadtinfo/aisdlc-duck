package risk

import (
	"strings"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/review"
)

const flowPolicyYAML = `schema: 1
levels: [low, medium, high]
default: medium
flow:
  low: { brd: auto, design: auto, cases: auto, step: auto }
  medium: { brd: manual, design: manual, cases: manual, step: auto }
`

func flowPolicy(t *testing.T) RiskPolicy { return mustParse(t, flowPolicyYAML) }

func TestAutoGate_allowedWhenFlowAutoAndNoFlag(t *testing.T) {
	p := flowPolicy(t)
	for _, g := range []gate.Gate{gate.BRD, gate.DESIGN, gate.CASES} {
		if r, manual := AutoGate(p, "low", g, false); manual {
			t.Errorf("low %s phải tự duyệt được: %s", g, r)
		}
	}
	if _, manual := AutoGate(p, "medium", gate.CASES, false); !manual {
		t.Error("medium cases phải duyệt tay")
	}
	if r, manual := AutoGate(p, "high", gate.BRD, false); !manual || r != "Mức high: cổng brd đặt duyệt tay" {
		t.Errorf("high brd: %q %v", r, manual)
	}
}

func TestAutoGate_reviewAlwaysManual(t *testing.T) {
	all := mustParse(t, "schema: 1\nlevels: [low, high]\ndefault: low\nflow:\n  low: { brd: auto, design: auto, cases: auto, step: auto }\n")
	if r, manual := AutoGate(all, "low", gate.REVIEW, false); !manual || r != "Review luôn duyệt tay" {
		t.Errorf("%q %v", r, manual)
	}
}

func TestAutoGate_highFlagBlocks(t *testing.T) {
	if r, manual := AutoGate(flowPolicy(t), "low", gate.BRD, true); !manual || r != "Review card có cờ đỏ mức HIGH" {
		t.Errorf("%q %v", r, manual)
	}
}

func TestFlowUnit(t *testing.T) {
	for g, want := range map[gate.Gate]string{gate.BRD: "brd", gate.DESIGN: "design", gate.CASES: "cases", gate.REVIEW: ""} {
		if FlowUnit(g) != want {
			t.Errorf("FlowUnit(%s)", g)
		}
	}
}

func TestBrdLevel_raisedToDefault(t *testing.T) {
	p := flowPolicy(t)
	for in, want := range map[string]string{"low": "medium", "high": "high", "medium": "medium", "": "medium", "weird": "medium"} {
		if got := BrdLevel(p, in); got != want {
			t.Errorf("BrdLevel(%q) = %s", in, got)
		}
	}
}

func TestLevelForGate_brdTakesStricterOfPreviewAndRiskFile(t *testing.T) {
	p := flowPolicy(t)
	lowDefault := mustParse(t, "schema: 1\nlevels: [low, medium, high]\ndefault: low\n")
	checks := []struct {
		p                  RiskPolicy
		g                  gate.Gate
		preview, risk, out string
	}{
		{lowDefault, gate.BRD, "low", "", "low"},
		{lowDefault, gate.BRD, "low", "high", "high"},
		{lowDefault, gate.BRD, "high", "low", "high"},
		{p, gate.BRD, "low", "low", "medium"},
		{p, gate.DESIGN, "high", "low", "low"},
		{p, gate.DESIGN, "low", "", ""},
		{p, gate.REVIEW, "low", "low", ""},
	}
	for i, c := range checks {
		if got := LevelForGate(c.p, c.g, c.preview, c.risk); got != c.out {
			t.Errorf("#%d: %q, want %q", i, got, c.out)
		}
	}
	if Stricter(p, "low", "weird") != "weird" || Stricter(p, "", "low") != "low" || Stricter(p, "high", "") != "high" {
		t.Error("Stricter sai")
	}
	if Stricter(p, "low", "high") != "high" || Stricter(p, "high", "low") != "high" {
		t.Error("Stricter thang sai")
	}
}

func TestAutoStep_missingMeansManual(t *testing.T) {
	if AutoStep(nil) {
		t.Error("nil phải duyệt tay")
	}
	if AutoStep(&RiskFile{}) {
		t.Error("không có flow phải duyệt tay")
	}
	if !AutoStep(&RiskFile{Flow: map[string]string{"step": "auto"}}) {
		t.Error("step auto phải tự accept")
	}
	if AutoStep(&RiskFile{Flow: map[string]string{"step": "manual"}}) {
		t.Error("step manual phải duyệt tay")
	}
}

func TestAutoFromRiskFile_usesSnapshot(t *testing.T) {
	f := &RiskFile{Flow: map[string]string{"brd": "auto", "design": "manual"}}
	if !AutoFromRiskFile(f, gate.BRD) || AutoFromRiskFile(f, gate.DESIGN) || AutoFromRiskFile(f, gate.REVIEW) ||
		AutoFromRiskFile(nil, gate.BRD) || AutoFromRiskFile(&RiskFile{}, gate.BRD) {
		t.Error("AutoFromRiskFile sai")
	}
}

func TestHasHighFlag_onlyHighCounts(t *testing.T) {
	warn := review.ReviewFlag{Code: "W", Level: review.WARN}
	high := review.ReviewFlag{Code: "H", Level: review.HIGH}
	if HasHighFlag(nil) || HasHighFlag([]review.ReviewFlag{warn}) || !HasHighFlag([]review.ReviewFlag{warn, high}) {
		t.Error("HasHighFlag sai")
	}
}

func TestSupersedeMessage_raisedVsFlowChanged(t *testing.T) {
	p := flowPolicy(t)
	cases := []struct {
		from, to string
		gates    []gate.Gate
		want     string
	}{
		{"low", "medium", []gate.Gate{gate.BRD, gate.DESIGN}, "Mức tăng low → medium: cổng BRD, Thiết kế cần dev duyệt lại."},
		{"", "medium", []gate.Gate{gate.CASES}, "Flow của mức medium đổi: cổng Case cần dev duyệt lại."},
		{"medium", "low", []gate.Gate{gate.BRD}, "Flow của mức low đổi: cổng BRD cần dev duyệt lại."},
	}
	for _, c := range cases {
		if got := SupersedeMessage(&p, c.from, c.to, c.gates); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
	if !strings.Contains(SupersedeMessage(nil, "low", "high", []gate.Gate{gate.REVIEW}), "Flow của mức high đổi: cổng Review") {
		t.Error("policy nil phải là flow đổi")
	}
}
