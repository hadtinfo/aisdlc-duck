package gates_test

import (
	"fmt"
	"strings"
	"testing"

	"aisdlc/internal/risk"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/webtest"
)

// Port AutoApproveIT (5): server tự duyệt bản nộp sạch khi flow của mức là auto (spec risk-flow §5.1, chế độ web).

// genericPolicy là mẫu generic mới nhất: low tự duyệt mọi cổng, medium/high duyệt tay; mặc định low.
func genericPolicy(t *testing.T) string {
	t.Helper()
	y, err := risk.PresetYAML("generic", risk.PresetLatestVersion("generic"))
	if err != nil {
		t.Fatal(err)
	}
	return y
}

// cards: thẻ cổng sạch (Thiết kế/Case có mức risk.yaml); designFlags là cờ của thẻ Thiết kế.
func cards(designFlags string) string {
	return fmt.Sprintf(`{"BRD":{"gate":"BRD","flags":[]},
	 "DESIGN":{"gate":"DESIGN","flags":%s,"risk":{"level":"low","policyVersion":1,"matched":[]}},
	 "CASES":{"gate":"CASES","flags":[],"risk":{"level":"low","policyVersion":1,"matched":[]}},
	 "REVIEW":{"gate":"REVIEW","flags":[]}}`, designFlags)
}

const highFlag = `[{"code":"X","level":"HIGH","message":"m","refs":[]}]`

// autoFeat: tính năng có chính sách (policy "" = không có) và thẻ cổng.
func (f *gfx) autoFeat(t *testing.T, repo, key, policy, cardsJSON string) *feat {
	t.Helper()
	ft := f.feat(t, repo, key, "FULL")
	if policy != "" {
		ft.importPolicy(policy)
	}
	ft.cardsJSON = cardsJSON
	ft.push()
	return ft
}

func (f *gfx) audit(t *testing.T, repo, action string) string {
	return strings.Join(f.strs(t, "SELECT detail::text FROM audit WHERE repo_id = $1 AND action = $2 ORDER BY id", repo, action), "\n")
}

func gatesOf(as []snapshot.Appr) string {
	return apprKeys(as, func(a snapshot.Appr) string { return a.Gate })
}

func (f *gfx) okState(t *testing.T, r webtest.Resp) snapshot.FeatureGateState {
	t.Helper()
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	return f.verified(t, r.Body)
}

// Cổng BRD luôn duyệt tay: bản nộp sạch, chính sách cho low tự duyệt mọi cổng, vẫn chờ dev.
func TestBrdNeverAuto(t *testing.T) {
	fx := newGfx(t, "aa")
	f := fx.autoFeat(t, "r/aa-0", "K-1", genericPolicy(t), cards("[]"))
	if s := fx.okState(t, f.submitWithRisk("BRD", "low")); len(s.Approvals) != 0 {
		t.Fatal(s.Approvals)
	}
	if s := fx.okState(t, f.submit("BRD", "[]")); len(s.Approvals) != 0 {
		t.Fatal(s.Approvals)
	}
	if n := fx.count(t, "SELECT count(*) FROM audit WHERE repo_id = 'r/aa-0' AND action LIKE 'gate.auto-%'"); n != 0 {
		t.Fatal(n)
	}
}

func TestAutoApproveLowCleanSubmission(t *testing.T) {
	fx := newGfx(t, "aa")
	ci := webtest.CliToken(t, fx.e.Pool, "ci-token", "aa-ci")
	f := fx.autoFeat(t, "r/aa-1", "K-1", genericPolicy(t), cards("[]"))
	f.openGates("BRD") // BRD luôn duyệt tay
	// Thiết kế: mức lấy từ risk.level của request
	s := fx.okState(t, f.submitWithRisk("DESIGN", "low"))
	if got := apprKeys(s.Approvals, func(a snapshot.Appr) string {
		return a.Gate + "|" + a.Approver + "|" + a.Channel + "|" + a.Rule + "|" + a.Hash
	}); got != "BRD|aa.owner|web||"+f.hash("BRD")+",DESIGN|auto|auto|flow:low|"+f.hash("DESIGN") {
		t.Fatal(got)
	}
	// thiếu risk.level: không biết mức, chờ dev
	if got := gatesOf(fx.okState(t, f.submit("CASES", "[]")).Approvals); got != "BRD,DESIGN" {
		t.Fatal(got)
	}
	if got := gatesOf(fx.okState(t, f.submitWithRisk("CASES", "low")).Approvals); got != "BRD,DESIGN,CASES" {
		t.Fatal(got)
	}
	if got := strings.Join(fx.strs(t, "SELECT gate || '|' || coalesce(rule, '-') || '|' || approver || '|' || channel "+
		"FROM approvals WHERE repo_id = 'r/aa-1' ORDER BY id"), ","); got != "BRD|-|aa.owner|web,DESIGN|flow:low|auto|auto,CASES|flow:low|auto|auto" {
		t.Fatal(got)
	}
	if a := fx.audit(t, "r/aa-1", "gate.auto-approve"); !strings.Contains(a, `"rule": "flow:low"`) || !strings.Contains(a, `"policyVersion": 1`) {
		t.Fatal(a)
	}
	if n := fx.count(t, "SELECT count(*) FROM audit WHERE repo_id = 'r/aa-1' AND action = 'gate.auto-approve' "+
		"AND login IS NULL AND ip IS NULL AND channel = 'auto' AND target = 'DESIGN'"); n != 1 {
		t.Fatal(n)
	}

	// CI thấy channel và rule
	c := fx.w.GetBearer("/api/ci/approvals?repo=r/aa-1&feature=K-1", ci)
	if c.Status != 200 {
		t.Fatal(c.Status, c.Body)
	}
	design := c.JSON(t)["approvals"].([]any)[1].(map[string]any)
	if design["gate"] != "DESIGN" || design["channel"] != "auto" || design["rule"] != "flow:low" {
		t.Fatal(design)
	}

	// thẻ trên web hiện "tự duyệt (mức low)"
	if d := fx.w.GetSession("/api/features/r/aa-1/K-1", fx.ownerS); !strings.Contains(d.Body, "tự duyệt (mức low)") {
		t.Fatal(d.Body)
	}
}

func TestAutoApproveHighFlagStaysPending(t *testing.T) {
	fx := newGfx(t, "aa")
	pol := genericPolicy(t)
	f := fx.autoFeat(t, "r/aa-2", "K-1", pol, cards(highFlag))
	f.openGates("BRD")
	if s := fx.okState(t, f.submitWithRisk("DESIGN", "low")); gatesOf(s.Approvals) != "BRD" {
		t.Fatal(s.Approvals)
	}
	// mức medium: flow duyệt tay
	g := fx.autoFeat(t, "r/aa-2b", "K-1", pol, cards("[]"))
	g.openGates("BRD")
	if s := fx.okState(t, g.submitWithRisk("DESIGN", "medium")); gatesOf(s.Approvals) != "BRD" {
		t.Fatal(s.Approvals)
	}
	// không có chính sách trên server: không tự duyệt
	n := fx.autoFeat(t, "r/aa-2c", "K-1", "", cards("[]"))
	n.openGates("BRD")
	if s := fx.okState(t, n.submitWithRisk("DESIGN", "low")); gatesOf(s.Approvals) != "BRD" {
		t.Fatal(s.Approvals)
	}
	// snapshot cũ (thẻ không ứng với nội dung đã nộp): không tự duyệt
	o := fx.autoFeat(t, "r/aa-2d", "K-1", pol, cards("[]"))
	o.openGates("BRD")
	o.text["DESIGN"] = "DESIGN v2" // nộp nội dung mới, snapshot vẫn là v1
	if s := fx.okState(t, o.submitWithRisk("DESIGN", "low")); gatesOf(s.Approvals) != "BRD" {
		t.Fatal(s.Approvals)
	}
	// thẻ có cờ cấu trúc lạ (khóa lạ, mức lạ, flags không phải mảng): coi như có cờ HIGH
	for i, flags := range []string{`[{"code":"X","level":"WARN","extra":1}]`, `[{"code":"X","level":"LOW"}]`, `"x"`, `[1]`,
		`[{"code":"X","refs":[null]}]`} {
		b := fx.autoFeat(t, fmt.Sprintf("r/aa-2e%d", i), "K-1", pol, cards(flags))
		b.openGates("BRD")
		if s := fx.okState(t, b.submitWithRisk("DESIGN", "low")); gatesOf(s.Approvals) != "BRD" {
			t.Fatal(flags, s.Approvals)
		}
	}
	// cờ hợp lệ không HIGH (và null, thiếu flags): vẫn tự duyệt
	for i, flags := range []string{`[{"code":"X","level":"WARN","message":"m","hint":"h","refs":["a"]},null]`, `null`} {
		b := fx.autoFeat(t, fmt.Sprintf("r/aa-ok%d", i), "K-1", pol, cards(flags))
		b.openGates("BRD")
		if s := fx.okState(t, b.submitWithRisk("DESIGN", "low")); gatesOf(s.Approvals) != "BRD,DESIGN" {
			t.Fatal(flags, s.Approvals)
		}
	}
	if c := fx.count(t, "SELECT count(*) FROM approvals WHERE repo_id LIKE 'r/aa-2%' AND channel = 'auto'"); c != 0 {
		t.Fatal(c)
	}
}

func TestAutoApproveUnknownLevelStaysPending(t *testing.T) {
	fx := newGfx(t, "aa")
	f := fx.autoFeat(t, "r/aa-3", "K-1", genericPolicy(t), cards("[]"))
	f.openGates("BRD")
	if got := gatesOf(fx.okState(t, f.submitWithRisk("DESIGN", "extreme")).Approvals); got != "BRD" {
		t.Fatal(got)
	}
	if a := fx.audit(t, "r/aa-3", "gate.auto-skip"); !strings.Contains(a, "auto: mức extreme không có trong chính sách v1") {
		t.Fatal(a)
	}
}

func TestReviewNeverAuto(t *testing.T) {
	fx := newGfx(t, "aa")
	f := fx.autoFeat(t, "r/aa-4", "K-1", genericPolicy(t), cards("[]"))
	f.openGates("BRD")
	fx.okState(t, f.submitWithRisk("DESIGN", "low"))
	fx.okState(t, f.submitWithRisk("CASES", "low"))
	if got := gatesOf(fx.okState(t, f.submitWithRisk("REVIEW", "low")).Approvals); got != "BRD,DESIGN,CASES" {
		t.Fatal(got)
	}
}

func TestAutoApproveFeatureLevelHigherThanRequestStaysPending(t *testing.T) {
	fx := newGfx(t, "aa")
	f := fx.autoFeat(t, "r/aa-5", "PAY-5", genericPolicy(t), cards("[]"))
	f.openGates("BRD")
	// server đã biết mức high (event RISK_CLASSIFIED); request vẫn nói low → lấy mức chặt hơn
	f.riskEvent("RISK_CLASSIFIED", `{"level":"high","policyVersion":1,"matched":[]}`)
	s := fx.okState(t, f.submitWithRisk("DESIGN", "low"))
	if got := apprKeys(s.Approvals, func(a snapshot.Appr) string { return a.Gate + "|" + a.Channel }); got != "BRD|web" {
		t.Fatal(got)
	}
}
