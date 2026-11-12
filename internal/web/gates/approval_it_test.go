package gates_test

import (
	"path/filepath"
	"sync"

	"aisdlc/internal/fsx"
	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"aisdlc/internal/snapshot"
)

// Port ApprovalIT (19): duyệt / trả lại / mở lại trên web theo luật giai đoạn 1 (POST /api/gates/**).

func (f *gfx) approvals(t *testing.T, repo, gate string) int {
	return f.count(t, "SELECT count(*) FROM approvals WHERE repo_id = $1 AND gate = $2", repo, gate)
}

// ---- luật 1: chế độ web, owner hoặc lead

func TestRule1OnlyOwnerOrLeadMayApproveInWebMode(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-1", "K-1", "FULL")
	f.submitOk("BRD")
	dev := f.approve(fx.devS, "BRD")
	if dev.Status != 403 || fx.errOf(t, dev) != "Chỉ chủ tính năng hoặc lead được duyệt" {
		t.Fatal(dev.Status, dev.Body)
	}
	if r := fx.w.PostBearer("/api/gates/approve", f.approveBody("BRD", f.hash("BRD")), fx.approveDev); r.Status != 403 {
		t.Fatal(r.Status, r.Body)
	}
	if n := fx.approvals(t, "r/ap-1", "BRD"); n != 0 {
		t.Fatal(n)
	}
	lead := f.approve(fx.leadS, "BRD")
	if lead.Status != 200 {
		t.Fatal(lead.Status, lead.Body)
	}
	s := fx.verified(t, lead.Body)
	if got := apprKeys(s.Approvals, func(a snapshot.Appr) string { return a.Gate + "|" + a.Approver + "|" + a.Channel }); got != "BRD|ap.lead|web" {
		t.Fatal(got)
	}
	// repo local: không duyệt trên web được
	fx.exec(t, "UPDATE repo_settings SET approvals = 'local' WHERE repo_id = 'r/ap-1'")
	local := f.approve(fx.ownerS, "DESIGN")
	if local.Status != 409 || fx.errOf(t, local) != "Repo không ở chế độ duyệt trên web" || local.JSON(t)["code"] != "NOT_WEB_MODE" {
		t.Fatal(local.Status, local.Body)
	}
}

func TestAuthKindsForApproveEndpoint(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-auth", "K-1", "FULL")
	f.submitOk("BRD")
	body := f.approveBody("BRD", f.hash("BRD"))
	if r := fx.w.PostBearer("/api/gates/approve", body, fx.ingest); r.Status != 403 {
		t.Fatal(r.Status)
	}
	if r := fx.w.PostSession("/api/gates/approve", body, fx.ownerS, ""); r.Status != 403 {
		t.Fatal(r.Status)
	}
	if r := fx.w.PostSession("/api/gates/approve", body, fx.ownerS, "sai"); r.Status != 403 {
		t.Fatal(r.Status)
	}
	if n := fx.approvals(t, "r/ap-auth", "BRD"); n != 0 {
		t.Fatal(n)
	}
	if r := fx.w.PostBearer("/api/gates/approve", body, fx.approveOwner); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if s := fx.str(t, "SELECT channel || '|' || approver FROM approvals WHERE repo_id = 'r/ap-auth'"); s != "terminal|ap.owner" {
		t.Fatal(s)
	}
	audit := fx.strs(t, "SELECT a::text FROM audit a WHERE action = 'gate.approve' AND repo_id = 'r/ap-auth'")
	if len(audit) != 1 || strings.Contains(audit[0], fx.approveOwner) {
		t.Fatal(audit)
	}
	if s := fx.str(t, "SELECT channel || '|' || login || '|' || target || '|' || (detail->>'channel') || '|' || (detail->>'hash') "+
		"FROM audit WHERE action = 'gate.approve' AND repo_id = 'r/ap-auth'"); s != "terminal|ap.owner|BRD|terminal|"+f.hash("BRD") {
		t.Fatal(s)
	}
}

func TestChannelClaudeCodeOnlyForTokens(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-cc", "K-1", "FULL")
	f.submitOk("BRD")
	body := f.approveBody("BRD", f.hash("BRD"))
	if r := fx.w.PostBearer("/api/gates/approve", strings.Replace(body, "{", `{"channel":"web",`, 1), fx.approveOwner); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if s := fx.str(t, "SELECT channel FROM approvals WHERE repo_id = 'r/ap-cc'"); s != "terminal" {
		t.Fatal(s)
	}
	g := fx.feat(t, "r/ap-cc2", "K-1", "FULL")
	g.submitOk("BRD")
	body = g.approveBody("BRD", g.hash("BRD"))
	if r := fx.w.PostBearer("/api/gates/approve", strings.Replace(body, "{", `{"channel":"claude-code",`, 1), fx.approveOwner); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if got := fx.strs(t, "SELECT channel FROM approvals WHERE repo_id LIKE 'r/ap-cc%' ORDER BY id"); !reflect.DeepEqual(got, []string{"terminal", "claude-code"}) {
		t.Fatal(got)
	}
	if s := fx.str(t, "SELECT detail->>'channel' FROM audit WHERE action = 'gate.approve' AND repo_id = 'r/ap-cc2' ORDER BY id DESC LIMIT 1"); s != "claude-code" {
		t.Fatal(s)
	}
}

// ---- luật 2: cổng trước

func TestRule2PreviousGatesMustBeApprovedAtCurrentHash(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-2", "K-1", "FULL")
	f.openGates("BRD")
	f.submitOk("DESIGN")
	f.text["BRD"] = "BRD v2" // BRD đổi sau khi duyệt -> STALE
	f.push()
	r := f.approve(fx.ownerS, "DESIGN")
	if r.Status != 409 || fx.errOf(t, r) != "Cổng trước chưa được duyệt" {
		t.Fatal(r.Status, r.Body)
	}
	f.text["BRD"] = "BRD v1"
	f.push()
	if r := f.approve(fx.ownerS, "DESIGN"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
}

func TestGateOutsideFlowIs400(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-lite", "K-1", "LITE")
	if r := fx.w.PostSession("/api/gates/approve", f.approveBody("BRD", "x"), fx.ownerS, fx.ownerS.CSRF); r.Status != 400 ||
		fx.errOf(t, r) != "Cổng BRD không thuộc luồng của tính năng" {
		t.Fatal(r.Status, r.Body)
	}
	if r := fx.w.PostSession("/api/gates/approve", f.approveBody("NOPE", "x"), fx.ownerS, fx.ownerS.CSRF); r.Status != 400 ||
		fx.errOf(t, r) != "Cổng không hợp lệ: NOPE" {
		t.Fatal(r.Status, r.Body)
	}
	if r := fx.w.PostSession("/api/gates/approve", `{"repo":"r/ap-lite","feature":"KHONG","gate":"CASES","hash":"x"}`,
		fx.ownerS, fx.ownerS.CSRF); r.Status != 404 {
		t.Fatal(r.Status, r.Body)
	}
}

// ---- luật 3: có submission

func TestRule3GateMustBeSubmittedAndRejectDropsSubmission(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-3", "K-1", "FULL")
	r := f.approve(fx.ownerS, "BRD")
	if r.Status != 409 || fx.errOf(t, r) != "Cổng chưa được nộp" {
		t.Fatal(r.Status, r.Body)
	}
	f.submitOk("BRD")
	if r := f.action("reject", fx.ownerS, "BRD", str("thiếu phạm vi")); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	after := f.approve(fx.ownerS, "BRD")
	if after.Status != 409 || fx.errOf(t, after) != "Cổng chưa được nộp" {
		t.Fatal(after.Status, after.Body)
	}
	f.submitOk("BRD")
	if r := f.approve(fx.ownerS, "BRD"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
}

// ---- luật 4: hash

func TestRule4HashMustMatchSubmissionAndLatestSnapshot(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-4", "K-1", "FULL")
	f.submitOk("BRD")
	wrong := fx.w.PostSession("/api/gates/approve", f.approveBody("BRD", "deadbeef"), fx.ownerS, fx.ownerS.CSRF)
	if wrong.Status != 409 || fx.errOf(t, wrong) != "Nội dung đã đổi kể từ lúc nộp, cần nộp lại" {
		t.Fatal(wrong.Status, wrong.Body)
	}
	submitted := f.hash("BRD")
	f.text["BRD"] = "BRD v2"
	f.push() // snapshot mới: currentHash khác bản nộp
	changed := fx.w.PostSession("/api/gates/approve", f.approveBody("BRD", submitted), fx.ownerS, fx.ownerS.CSRF)
	if changed.Status != 409 || fx.errOf(t, changed) != "Nội dung đã đổi kể từ lúc nộp, cần nộp lại" {
		t.Fatal(changed.Status, changed.Body)
	}
	f.submitOk("BRD")
	if r := f.approve(fx.ownerS, "BRD"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
}

// ---- luật 5: findings

func TestRule5SubmissionWithFindingsCannotBeApproved(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-5", "K-1", "FULL")
	if r := f.submit("BRD", `[{"code":"B1","message":"thiếu"}]`); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	r := f.approve(fx.ownerS, "BRD")
	if r.Status != 409 || fx.errOf(t, r) != "Bản nộp còn phát hiện" {
		t.Fatal(r.Status, r.Body)
	}
	f.submitOk("BRD")
	if r := f.approve(fx.ownerS, "BRD"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
}

// ---- luật 6: REVIEW

func TestRule6ReviewNeedsPassedEvalOnCurrentTreeAndAcceptedSteps(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-6", "K-1", "LITE")
	f.openGates("CASES")
	f.evalJSON = `{"passed":false,"evaluatedTree":"t1"}`
	f.tree = "t1"
	f.stepsJSON = `[{"id":"S1","status":"ACCEPTED"},{"id":"S2","status":"AWAITING_DEV"}]`
	f.push()
	f.submitOk("REVIEW")
	r := f.approve(fx.ownerS, "REVIEW")
	if r.Status != 409 || fx.errOf(t, r) != "Eval chưa ĐẠT" {
		t.Fatal(r.Status, r.Body)
	}
	f.evalJSON = `{"passed":true,"evaluatedTree":"t0"}`
	f.push()
	if r := f.approve(fx.ownerS, "REVIEW"); fx.errOf(t, r) != "Eval không khớp code hiện tại, chạy lại aisdlc eval" {
		t.Fatal(r.Status, r.Body)
	}
	f.evalJSON = `{"passed":true,"evaluatedTree":"t1"}`
	f.push()
	if r := f.approve(fx.ownerS, "REVIEW"); r.Status != 409 || fx.errOf(t, r) != "Còn bước chưa accept" {
		t.Fatal(r.Status, r.Body)
	}
	f.stepsJSON = `[{"id":"S1","status":"ACCEPTED"},{"id":"S2","status":"ACCEPTED"}]`
	f.push()
	r = f.approve(fx.ownerS, "REVIEW")
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if s := fx.str(t, "SELECT evaluated_tree FROM approvals WHERE repo_id = 'r/ap-6' AND gate = 'REVIEW'"); s != "t1" {
		t.Fatal(s)
	}
	var trees []string
	for _, a := range fx.verified(t, r.Body).Approvals {
		if a.Gate == "REVIEW" {
			trees = append(trees, a.EvaluatedTree)
		}
	}
	if !reflect.DeepEqual(trees, []string{"t1"}) {
		t.Fatal(trees)
	}
}

func TestRule6ReviewWithoutTreeIsRefused(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-6b", "K-1", "LITE")
	f.openGates("CASES")
	f.evalJSON = `{"passed":true,"evaluatedTree":"t1"}`
	f.push() // không có tree
	f.submitOk("REVIEW")
	if r := f.approve(fx.ownerS, "REVIEW"); r.Status != 409 || fx.errOf(t, r) != "Eval không khớp code hiện tại, chạy lại aisdlc eval" {
		t.Fatal(r.Status, r.Body)
	}
}

// ---- luật 7: idempotent

func TestRule7ApprovingTwiceIsIdempotent(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-7", "K-1", "FULL")
	f.submitOk("BRD")
	if r := f.approve(fx.ownerS, "BRD"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	again := f.approve(fx.leadS, "BRD")
	if again.Status != 200 {
		t.Fatal(again.Status, again.Body)
	}
	if got := apprKeys(fx.verified(t, again.Body).Approvals, func(a snapshot.Appr) string { return a.Approver }); got != "ap.owner" {
		t.Fatal(got)
	}
	if n := fx.approvals(t, "r/ap-7", "BRD"); n != 1 {
		t.Fatal(n)
	}
	if n := fx.count(t, "SELECT count(*) FROM audit WHERE action = 'gate.approve' AND repo_id = 'r/ap-7'"); n != 1 {
		t.Fatal(n)
	}
}

// ---- flags / assumptionsSeen (R3)

func TestFlagsAndAssumptionsAreStoredSanitized(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-r3", "K-1", "FULL")
	f.submitOk("BRD")
	assumptions := []any{strings.Repeat("x", 600), 42, "có\u0000nul"}
	for i := 0; i < 60; i++ {
		assumptions = append(assumptions, "gia dinh "+itoa(i))
	}
	flags := []any{map[string]any{"code": "SCOPE_WIDE", "level": "HIGH"}, "chuoi", map[string]any{"code": 1, "level": "WARN"},
		map[string]any{"code": "DB_DROP", "level": "WARN"}}
	b, _ := json.Marshal(map[string]any{"repo": f.repo, "feature": f.key, "gate": "BRD", "hash": f.hash("BRD"),
		"flags": flags, "assumptionsSeen": assumptions})
	if r := fx.w.PostSession("/api/gates/approve", string(b), fx.ownerS, fx.ownerS.CSRF); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	var seen []string
	if err := json.Unmarshal([]byte(fx.str(t, "SELECT assumptions_seen::text FROM approvals WHERE repo_id = 'r/ap-r3'")), &seen); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 50 || len(seen[0]) != 500 || seen[1] != "gia dinh 0" {
		t.Fatal(len(seen), len(seen[0]), seen[1])
	}
	var fl any
	if err := json.Unmarshal([]byte(fx.str(t, "SELECT flags::text FROM approvals WHERE repo_id = 'r/ap-r3'")), &fl); err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(fl); string(got) != `[{"code":"SCOPE_WIDE","level":"HIGH"},{"code":"DB_DROP","level":"WARN"}]` {
		t.Fatal(string(got))
	}
	if d := fx.str(t, "SELECT (detail->'flags')::text FROM audit WHERE action = 'gate.approve' AND repo_id = 'r/ap-r3'"); !strings.Contains(d, "SCOPE_WIDE") ||
		!strings.Contains(d, "DB_DROP") {
		t.Fatal(d)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

// ---- trả lại / mở lại

func TestRejectDesignRevokesDesignAndLaterGates(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-rj", "K-1", "FULL")
	f.openGates("BRD", "DESIGN", "CASES")
	if r := f.action("reject", fx.ownerS, "DESIGN", nil); r.Status != 400 || fx.errOf(t, r) != "Thiếu comment (lý do trả lại)" {
		t.Fatal(r.Status, r.Body)
	}
	if r := f.action("reject", fx.ownerS, "DESIGN", str("  ")); r.Status != 400 {
		t.Fatal(r.Status)
	}
	if r := f.action("reject", fx.devS, "DESIGN", str("không")); r.Status != 403 {
		t.Fatal(r.Status)
	}
	r := f.action("reject", fx.ownerS, "DESIGN", str("thiếu bảng"))
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	s := fx.verified(t, r.Body)
	if got := apprKeys(s.Approvals, func(a snapshot.Appr) string { return a.Gate }); got != "BRD" {
		t.Fatal(got)
	}
	if len(s.Feedback) != 1 || s.Feedback[0].Gate != "DESIGN" || s.Feedback[0].Kind != "REJECTED" || s.Feedback[0].Comment != "thiếu bảng" {
		t.Fatal(s.Feedback)
	}
	if got := fx.strs(t, "SELECT gate FROM approvals WHERE repo_id = 'r/ap-rj' AND revoked_at IS NOT NULL AND revoked_by = 'ap.owner' ORDER BY gate"); !reflect.DeepEqual(got, []string{"CASES", "DESIGN"}) {
		t.Fatal(got)
	}
	if n := fx.count(t, "SELECT count(*) FROM audit WHERE action = 'gate.reject' AND repo_id = 'r/ap-rj' AND channel = 'web'"); n != 1 {
		t.Fatal(n)
	}
}

func TestRejectingPreviousGateBlocksApprovingLaterGate(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-rb", "K-1", "FULL")
	f.openGates("BRD")
	f.submitOk("DESIGN")
	if r := f.action("reject", fx.ownerS, "BRD", str("sai phạm vi")); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if r := f.approve(fx.ownerS, "DESIGN"); r.Status != 409 || fx.errOf(t, r) != "Cổng trước chưa được duyệt" {
		t.Fatal(r.Status, r.Body)
	}
}

func TestCommentIsCappedAt2000Chars(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-cm", "K-1", "FULL")
	f.openGates("BRD")
	for _, action := range []string{"reject", "reopen"} {
		r := f.action(action, fx.ownerS, "BRD", str(strings.Repeat("x", 2001)))
		if r.Status != 400 || fx.errOf(t, r) != "Ghi chú quá dài (tối đa 2000 ký tự)" {
			t.Fatal(action, r.Status, r.Body)
		}
	}
	if n := fx.count(t, "SELECT count(*) FROM feedback WHERE repo_id = 'r/ap-cm'"); n != 0 {
		t.Fatal(n)
	}
	if r := f.action("reject", fx.ownerS, "BRD", str(strings.Repeat("x", 2000))); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	// chỉ dẫn sinh ra dài hơn 2000 (thêm "Cổng BRD bị trả lại bởi ...: "): cắt còn 1999 ký tự + "…"
	text := []rune(fx.str(t, "SELECT text FROM instructions WHERE repo_id = 'r/ap-cm'"))
	if len(text) != 2000 || !strings.HasPrefix(string(text), "Cổng BRD bị trả lại bởi "+fx.owner+": xxx") || !strings.HasSuffix(string(text), "x…") {
		t.Fatal(len(text), string(text[:40]))
	}
	// vừa đủ 2000: giữ nguyên, không có "…"
	head := "Cổng BRD được mở lại bởi " + fx.owner + ": "
	if r := f.action("reopen", fx.ownerS, "BRD", str(strings.Repeat("y", 2000-len([]rune(head))))); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	all := fx.strs(t, "SELECT text FROM instructions WHERE repo_id = 'r/ap-cm' ORDER BY id")
	last := all[len(all)-1]
	if len([]rune(last)) != 2000 || !strings.HasPrefix(last, head) || !strings.HasSuffix(last, "yy") {
		t.Fatal(len([]rune(last)), last[:40])
	}
}

func TestReopenByLeadWithoutCommentRevokesAndWritesFeedback(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-ro", "K-1", "LITE")
	f.openGates("CASES")
	r := f.action("reopen", fx.leadS, "CASES", nil)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	s := fx.verified(t, r.Body)
	if len(s.Approvals) != 0 || len(s.Submissions) != 0 || len(s.Feedback) != 1 || s.Feedback[0].Kind != "REOPENED" {
		t.Fatalf("%+v", s)
	}
	if n := fx.count(t, "SELECT count(*) FROM audit WHERE action = 'gate.reopen' AND repo_id = 'r/ap-ro'"); n != 1 {
		t.Fatal(n)
	}
}

// ---- phạm vi bổ sung: nộp khi cổng trước chưa duyệt, artifacts rỗng/trùng, chi tiết có submittedPaths

func TestSubmissionRequiresPreviousGatesApprovedAndValidArtifacts(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-sub", "K-1", "FULL")
	if r := f.submit("DESIGN", "[]"); r.Status != 409 || fx.errOf(t, r) != "Cổng trước chưa được duyệt" {
		t.Fatal(r.Status, r.Body)
	}
	empty := `{"repo":"r/ap-sub","feature":"K-1","gate":"BRD","hash":"x","findings":[],"artifacts":[]}`
	if r := fx.w.PostBearer("/api/cli/submissions", empty, fx.ingest); r.Status != 400 || fx.errOf(t, r) != "artifacts rỗng" {
		t.Fatal(r.Status, r.Body)
	}
	dup := strings.Replace(f.submissionBody("BRD", "[]"), `"artifacts":[`, `"artifacts":[{"path":"`+gpath("BRD")+`","content":null},`, 1)
	if r := fx.w.PostBearer("/api/cli/submissions", dup, fx.ingest); r.Status != 400 || fx.errOf(t, r) != "artifacts trùng đường dẫn: "+gpath("BRD") {
		t.Fatal(r.Status, r.Body)
	}
	if n := fx.count(t, "SELECT count(*) FROM submissions WHERE repo_id = 'r/ap-sub'"); n != 0 {
		t.Fatal(n)
	}
	f.openGates("BRD")
	f.submitOk("DESIGN")

	r := fx.w.GetSession("/api/features/r/ap-sub/K-1", fx.devS)
	d := r.JSON(t)
	sp := d["submittedPaths"].(map[string]any)
	if b, _ := json.Marshal(sp["BRD"]); string(b) != `["`+gpath("BRD")+`"]` {
		t.Fatal(string(b))
	}
	if b, _ := json.Marshal(sp["DESIGN"]); string(b) != `["`+gpath("DESIGN")+`"]` {
		t.Fatal(string(b))
	}
	if strings.Contains(r.Body, "REVTSUdOIHYx") { // không trả nội dung (base64 của "DESIGN v1")
		t.Fatal(r.Body)
	}
	if p, ok := d["pendingImports"].([]any); !ok || len(p) != 0 {
		t.Fatal(d["pendingImports"])
	}
}

func TestCliGatesRejectsNulInQuery(t *testing.T) {
	fx := newGfx(t, "ap")
	if r := fx.w.GetBearer("/api/cli/gates?repo=r%00x&feature=K-1", fx.ingest); r.Status != 400 || fx.errOf(t, r) != "repo chứa ký tự NUL (U+0000)" {
		t.Fatal(r.Status, r.Body)
	}
	if r := fx.w.GetBearer("/api/cli/gates?repo=r&feature=K%00", fx.ingest); r.Status != 400 {
		t.Fatal(r.Status, r.Body)
	}
	if r := fx.w.GetBearer("/api/cli/gates?feature=K", fx.ingest); r.Status != 400 || fx.errOf(t, r) != "Thiếu repo" {
		t.Fatal(r.Status, r.Body)
	}
}

// Java đọc chỉ dẫn qua /api/cli/control (Task 8); ở đây kiểm thẳng bảng instructions.
func TestRejectCreatesInstruction(t *testing.T) {
	fx := newGfx(t, "ap")
	f := fx.feat(t, "r/ap-instr", "K-1", "FULL")
	f.openGates("BRD")
	f.submitOk("DESIGN")
	if r := f.action("reject", fx.ownerS, "DESIGN", str("sửa mục API")); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	got := fx.strs(t, "SELECT text FROM instructions WHERE repo_id = $1 AND delivered_at IS NULL ORDER BY id", f.repo)
	if !reflect.DeepEqual(got, []string{"Cổng DESIGN bị trả lại bởi " + fx.owner + ": sửa mục API"}) {
		t.Fatal(got)
	}
	if s := fx.str(t, "SELECT source || '|' || (feedback_id = (SELECT max(id) FROM feedback WHERE repo_id = $1))::text FROM instructions WHERE repo_id = $1", f.repo); s != "feedback|true" {
		t.Fatal(s)
	}
	f.action("reopen", fx.ownerS, "BRD", str("làm lại"))
	all := fx.strs(t, "SELECT text FROM instructions WHERE repo_id = $1 ORDER BY id", f.repo)
	if all[len(all)-1] != "Cổng BRD được mở lại bởi "+fx.owner+": làm lại" {
		t.Fatal(all)
	}
}

// Phía tiêu thụ: phong bì trạng thái cổng của server ghi ra file rồi đọc bằng gate.ReadWebGateState với khóa ghim.
func TestGateStateEnvelopeConsumedByCli(t *testing.T) {
	fx := newGfx(t, "gs")
	f := fx.feat(t, "r/gs-1", "K-1", "FULL")
	f.openGates("BRD")
	raw := fx.envelopeRaw(t, f)
	pk := fx.w.GetBearer("/api/cli/pubkey", fx.ingest)
	pub, err := snapshot.PublicKeyFromBase64(pk.JSON(t)["publicKey"].(string))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "K-1.json")
	if err := fsx.WriteAtomic(file, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	st, ok := gate.ReadWebGateState(file, pub, f.repo, model.FeatureKey("K-1"))
	if !ok || st.Repo != f.repo || st.Feature != "K-1" || len(st.Approvals) != 1 {
		t.Fatalf("%v %+v", ok, st)
	}
	if _, ok := gate.ReadWebGateState(file, pub, f.repo, model.FeatureKey("K-2")); ok {
		t.Fatal("sai tính năng phải bị từ chối")
	}
	if _, ok := gate.ReadWebGateState(file, pub, "r/khac", model.FeatureKey("K-1")); ok {
		t.Fatal("sai repo phải bị từ chối")
	}
}

// Thao tác cổng (duyệt) cũng tới Deps.Changed, không chỉ hub SSE.
func TestGateApproveReachesChangedObserver(t *testing.T) {
	fx := newGfx(t, "ch")
	f := fx.feat(t, "r/ch-1", "K-1", "FULL")
	f.submitOk("BRD")
	var mu sync.Mutex
	var seen []string
	fx.onChanged = func(r, k string) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r+"|"+k)
	}
	if r := f.approve(fx.ownerS, "BRD"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 || seen[len(seen)-1] != "r/ch-1|K-1" {
		t.Fatalf("Changed không thấy duyệt cổng: %v", seen)
	}
}
