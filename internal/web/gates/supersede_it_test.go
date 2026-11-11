package gates_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
)

// Port SupersedeIT (3): sự kiện rủi ro nâng mức → server thay thế bản tự duyệt mà flow của mức mới là duyệt tay
// (spec risk-flow §5.4). Bản tự duyệt do AutoApprover ghi khi nộp; thay thế chạy trong tx ingest (RiskChanged).

func (f *gfx) revoked(t *testing.T, repo string) string {
	return strings.Join(f.strs(t, "SELECT gate || ':' || channel || ':' || coalesce(revoked_by, '-') FROM approvals "+
		"WHERE repo_id = $1 ORDER BY id", repo), ",")
}

// supersedeFeat: tính năng có BRD đã duyệt tay (BRD luôn duyệt tay); designFlags là cờ của thẻ Thiết kế.
func (f *gfx) supersedeFeat(t *testing.T, repo, designFlags string) *feat {
	ft := f.autoFeat(t, repo, "PAY-1", genericPolicy(t), cards(designFlags))
	ft.openGates("BRD")
	return ft
}

func TestSupersedeOnRiskClassifiedRaise(t *testing.T) {
	fx := newGfx(t, "ss")
	f := fx.supersedeFeat(t, "r/ss-1", "[]")
	f.riskEvent("RISK_CLASSIFIED", `{"level":"low","policyVersion":1,"matched":[]}`)
	fx.okState(t, f.submitWithRisk("DESIGN", "low"))
	fx.okState(t, f.submitWithRisk("CASES", "low"))
	if s := fx.revoked(t, "r/ss-1"); s != "BRD:web:-,DESIGN:auto:-,CASES:auto:-" {
		t.Fatal(s)
	}

	f.riskEvent("RISK_CLASSIFIED", `{"level":"high","policyVersion":1,"matched":["G1"]}`)
	if s := fx.revoked(t, "r/ss-1"); s != "BRD:web:-,DESIGN:auto:auto-supersede,CASES:auto:auto-supersede" {
		t.Fatal(s)
	}
	if m := fx.str(t, "SELECT detail->>'message' FROM audit WHERE repo_id = 'r/ss-1' AND action = 'gate.auto-supersede'"); m != "Mức tăng low → high: cổng Thiết kế, Case cần dev duyệt lại." {
		t.Fatal(m)
	}
	if a := fx.str(t, "SELECT coalesce(login, '-') || '|' || channel || '|' || target || '|' || (detail->>'from') || '|' || "+
		"(detail->>'to') || '|' || (detail->'gates')::text || '|' || (detail->>'policyVersion') FROM audit WHERE repo_id = 'r/ss-1' "+
		"AND action = 'gate.auto-supersede'"); a != `-|auto|DESIGN,CASES|low|high|["DESIGN", "CASES"]|1` {
		t.Fatal(a)
	}

	// trang tính năng: cổng bị thay thế mang cờ superseded (huy hiệu "đã thay thế")
	d := fx.w.GetSession("/api/features/r/ss-1/PAY-1", fx.ownerS).JSON(t)
	for _, n := range d["snapshot"].(map[string]any)["gates"].([]any) {
		g := n.(map[string]any)
		sup := g["superseded"] == true
		if want := g["gate"] == "DESIGN" || g["gate"] == "CASES"; sup != want {
			t.Fatal(g)
		}
	}

	// phong bì ký cho CLI chỉ còn bản duyệt tay BRD; bản tự duyệt bị thay thế đi kèm (chỉ để hiển thị)
	body := fx.envelopeRaw(t, f)
	s := fx.verified(t, body)
	if gatesOf(s.Approvals) != "BRD" {
		t.Fatal(s.Approvals)
	}
	if got := apprKeys(s.Superseded, func(a snapshot.Appr) string { return a.Gate + "|" + a.Rule }); got != "DESIGN|flow:low,CASES|flow:low" {
		t.Fatal(got)
	}
	// cache cổng có chữ ký của CLI coi cổng là chưa duyệt (Superseded)
	cache := filepath.Join(t.TempDir(), "gates.json")
	if err := os.WriteFile(cache, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	store := gate.NewWebGateStore(cache, fx.keys.Public(), "r/ss-1", model.FeatureKey("PAY-1"))
	if _, ok := store.State(); !ok {
		t.Fatal("CLI không đọc được phong bì")
	}
	for _, g := range []gate.Gate{gate.DESIGN, gate.CASES} {
		if a, ok, _ := store.Approval(g); !ok || !a.Superseded {
			t.Fatalf("%s %+v", g, a)
		}
	}
	if _, ok, _ := store.Submission(gate.DESIGN); !ok { // nội dung không đổi: chỉ chờ dev duyệt lại
		t.Fatal("mất bản nộp DESIGN")
	}

	// dev duyệt tay lại được mà không cần nộp lại
	f.approveOk("DESIGN")
	env := fx.envelope(t, f)
	if got := apprKeys(env.Approvals, func(a snapshot.Appr) string { return a.Gate + "|" + a.Channel }); got != "BRD|web,DESIGN|web" {
		t.Fatal(got)
	}
	if got := gatesOf(env.Superseded); got != "CASES" {
		t.Fatal(got)
	}
}

func TestResubmitDesignAfterRaiseStaysPending(t *testing.T) {
	fx := newGfx(t, "ss")
	f := fx.supersedeFeat(t, "r/ss-4", "[]")
	fx.okState(t, f.submitWithRisk("DESIGN", "low"))
	f.riskEvent("RISK_CLASSIFIED", `{"level":"high","policyVersion":1,"matched":["G1"]}`)
	if s := fx.revoked(t, "r/ss-4"); s != "BRD:web:-,DESIGN:auto:auto-supersede" {
		t.Fatal(s)
	}
	// không biết mức trước (risk_level NULL): thông điệp lấy mức từ rule flow:low của bản bị thay thế
	if m := fx.str(t, "SELECT detail->>'message' FROM audit WHERE repo_id = 'r/ss-4' AND action = 'gate.auto-supersede'"); m != "Mức tăng low → high: cổng Thiết kế cần dev duyệt lại." {
		t.Fatal(m)
	}

	// agent nộp lại Thiết kế không đổi, CLI gửi mức risk.yaml high → chờ dev
	fx.okState(t, f.submitWithRisk("DESIGN", "high"))
	// gửi mức thấp hơn (hoặc không gửi, CLI cũ): mức server đã biết (features.risk_level = high) vẫn chặn
	fx.okState(t, f.submitWithRisk("DESIGN", "low"))
	f.submitOk("DESIGN")
	if s := fx.revoked(t, "r/ss-4"); s != "BRD:web:-,DESIGN:auto:auto-supersede" {
		t.Fatal(s)
	}
	if env := fx.envelope(t, f); gatesOf(env.Approvals) != "BRD" {
		t.Fatal(env.Approvals)
	}
}

func TestSupersedeIgnoresManualApprovals(t *testing.T) {
	// Thiết kế có cờ HIGH: dev duyệt tay; Case tự duyệt ở mức low
	fx := newGfx(t, "ss")
	f := fx.supersedeFeat(t, "r/ss-2", highFlag)
	fx.okState(t, f.submitWithRisk("DESIGN", "low"))
	f.approveOk("DESIGN")
	fx.okState(t, f.submitWithRisk("CASES", "low"))
	if s := fx.revoked(t, "r/ss-2"); s != "BRD:web:-,DESIGN:web:-,CASES:auto:-" {
		t.Fatal(s)
	}

	// đổi tay lên medium (RISK_OVERRIDDEN): chỉ bản tự duyệt bị thay thế
	f.riskEvent("RISK_OVERRIDDEN", `{"from":"low","to":"medium","reason":"r"}`)
	if s := fx.revoked(t, "r/ss-2"); s != "BRD:web:-,DESIGN:web:-,CASES:auto:auto-supersede" {
		t.Fatal(s)
	}
	if got := gatesOf(fx.envelope(t, f).Approvals); got != "BRD,DESIGN" {
		t.Fatal(got)
	}
	if m := fx.str(t, "SELECT detail->>'message' FROM audit WHERE repo_id = 'r/ss-2' AND action = 'gate.auto-supersede'"); m != "Mức tăng low → medium: cổng Case cần dev duyệt lại." {
		t.Fatal(m)
	}

	// mức vẫn cho phép auto: không thay thế gì
	g := fx.supersedeFeat(t, "r/ss-3", "[]")
	fx.okState(t, g.submitWithRisk("DESIGN", "low"))
	g.riskEvent("RISK_CLASSIFIED", `{"level":"low","policyVersion":1,"matched":[]}`)
	if s := fx.revoked(t, "r/ss-3"); s != "BRD:web:-,DESIGN:auto:-" {
		t.Fatal(s)
	}
	if n := fx.count(t, "SELECT count(*) FROM audit WHERE repo_id = 'r/ss-3' AND action = 'gate.auto-supersede'"); n != 0 {
		t.Fatal(n)
	}
}
