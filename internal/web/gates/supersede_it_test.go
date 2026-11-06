package gates_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/gates"
)

// Port SupersedeIT (3), phần thuộc Task 6: kho (supersedeAuto, supersededGates), phong bì (superseded) và lớp phủ
// (cờ superseded). Việc server tự duyệt khi nộp và tự thay thế khi sự kiện rủi ro nâng mức là AutoApprover (Task 7):
// ở đây bản tự duyệt được ghi bằng gates.SaveAutoApproval và thay thế bằng gates.SupersedeAuto như AutoApprover sẽ gọi.

func (f *gfx) revoked(t *testing.T, repo string) string {
	return strings.Join(f.strs(t, "SELECT gate || ':' || channel || ':' || coalesce(revoked_by, '-') FROM approvals "+
		"WHERE repo_id = $1 ORDER BY id", repo), ",")
}

// autoApprove: tự duyệt bản nộp còn hiệu lực mới nhất của cổng ở mức low.
func (f *gfx) autoApprove(t *testing.T, ft *feat, g string) {
	t.Helper()
	ctx := context.Background()
	var id int64
	var hash string
	if err := f.e.Pool.QueryRow(ctx, "SELECT id, hash FROM submissions WHERE repo_id = $1 AND feature_key = $2 AND gate = $3 "+
		"ORDER BY id DESC LIMIT 1", ft.repo, ft.key, g).Scan(&id, &hash); err != nil {
		t.Fatal(err)
	}
	if _, err := gates.SaveAutoApproval(ctx, f.e.Pool, id, ft.repo, ft.key, gate.Gate(g), hash, "flow:low", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func (f *gfx) supersede(t *testing.T, ft *feat, gs ...gate.Gate) []string {
	t.Helper()
	out, err := gates.SupersedeAuto(context.Background(), f.e.Pool, ft.repo, ft.key, gs, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSupersedeOnRaise(t *testing.T) { // SupersedeIT.supersede_onRiskClassifiedRaise
	fx := newGfx(t, "ss")
	f := fx.feat(t, "r/ss-1", "PAY-1", "FULL")
	f.submitOk("BRD")
	fx.autoApprove(t, f, "BRD")
	f.submitOk("DESIGN")
	fx.autoApprove(t, f, "DESIGN")
	if s := fx.revoked(t, "r/ss-1"); s != "BRD:auto:-,DESIGN:auto:-" {
		t.Fatal(s)
	}
	if got := fx.supersede(t, f, gate.BRD, gate.DESIGN, gate.CASES); len(got) != 2 {
		t.Fatal(got)
	}
	if s := fx.revoked(t, "r/ss-1"); s != "BRD:auto:auto-supersede,DESIGN:auto:auto-supersede" {
		t.Fatal(s)
	}

	// trang tính năng: cổng bị thay thế mang cờ superseded (huy hiệu "đã thay thế")
	d := fx.w.GetSession("/api/features/r/ss-1/PAY-1", fx.ownerS).JSON(t)
	for _, n := range d["snapshot"].(map[string]any)["gates"].([]any) {
		g := n.(map[string]any)
		sup := g["superseded"] == true
		if want := g["gate"] == "BRD" || g["gate"] == "DESIGN"; sup != want {
			t.Fatal(g)
		}
	}

	// phong bì ký cho CLI không còn approval; bản tự duyệt bị thay thế đi kèm (chỉ để hiển thị)
	body := fx.envelopeRaw(t, f)
	s := fx.verified(t, body)
	if len(s.Approvals) != 0 {
		t.Fatal(s.Approvals)
	}
	if got := apprKeys(s.Superseded, func(a snapshot.Appr) string { return a.Gate + "|" + a.Rule }); got != "BRD|flow:low,DESIGN|flow:low" {
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
	for _, g := range []gate.Gate{gate.BRD, gate.DESIGN} {
		if a, ok, _ := store.Approval(g); !ok || !a.Superseded {
			t.Fatalf("%s %+v", g, a)
		}
	}
	if _, ok, _ := store.Submission(gate.BRD); !ok { // nội dung không đổi: chỉ chờ dev duyệt lại
		t.Fatal("mất bản nộp BRD")
	}

	// dev duyệt tay lại được mà không cần nộp lại
	f.approveOk("BRD")
	env := fx.envelope(t, f)
	if got := apprKeys(env.Approvals, func(a snapshot.Appr) string { return a.Gate + "|" + a.Channel }); got != "BRD|web" {
		t.Fatal(got)
	}
	if got := apprKeys(env.Superseded, func(a snapshot.Appr) string { return a.Gate }); got != "DESIGN" {
		t.Fatal(got)
	}
}

func TestResubmitBrdAfterRaiseStaysPending(t *testing.T) { // SupersedeIT.resubmitBrdAfterRaise_staysPending (phần Task 6)
	fx := newGfx(t, "ss")
	f := fx.feat(t, "r/ss-4", "PAY-1", "FULL")
	f.submitOk("BRD")
	fx.autoApprove(t, f, "BRD")
	f.submitOk("DESIGN")
	fx.autoApprove(t, f, "DESIGN")
	fx.supersede(t, f, gate.BRD, gate.DESIGN)
	// nộp lại BRD không đổi: không có gì làm bản bị thay thế có hiệu lực lại
	if r := f.submit("BRD", "[]"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if s := fx.revoked(t, "r/ss-4"); s != "BRD:auto:auto-supersede,DESIGN:auto:auto-supersede" {
		t.Fatal(s)
	}
	if env := fx.envelope(t, f); len(env.Approvals) != 0 || len(env.Superseded) != 2 {
		t.Fatalf("%+v", env)
	}
}

func TestSupersedeIgnoresManualApprovals(t *testing.T) { // SupersedeIT.supersede_ignoresManualApprovals (phần Task 6)
	fx := newGfx(t, "ss")
	f := fx.feat(t, "r/ss-2", "PAY-1", "FULL")
	f.submitOk("BRD")
	f.approveOk("BRD")
	f.submitOk("DESIGN")
	fx.autoApprove(t, f, "DESIGN")
	if s := fx.revoked(t, "r/ss-2"); s != "BRD:web:-,DESIGN:auto:-" {
		t.Fatal(s)
	}
	if got := fx.supersede(t, f, gate.BRD, gate.DESIGN); !reflect.DeepEqual(got, []string{"DESIGN"}) {
		t.Fatal(got)
	}
	if s := fx.revoked(t, "r/ss-2"); s != "BRD:web:-,DESIGN:auto:auto-supersede" {
		t.Fatal(s)
	}
	if got := apprKeys(fx.envelope(t, f).Approvals, func(a snapshot.Appr) string { return a.Gate }); got != "BRD" {
		t.Fatal(got)
	}
	// không còn bản tự duyệt hiệu lực: thay thế lần nữa không đổi gì
	if got := fx.supersede(t, f, gate.DESIGN); len(got) != 0 {
		t.Fatal(got)
	}
}
