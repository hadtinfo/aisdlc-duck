package gates_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/gates"
	"aisdlc/internal/web/webtest"
)

// closeAttempt đánh dấu mọi submission/feedback hiện có của tính năng là đã khép bởi một bản ghi rewind (như Task 7).
func (f *gfx) closeAttempt(t *testing.T, repo, key string) {
	t.Helper()
	id := f.str(t, "INSERT INTO feature_rewinds(repo_id, feature_key, from_attempt, to_attempt, checkpoint, by_login, request_id) "+
		"VALUES ($1,$2,1,2,'cp','x','req-1') RETURNING id::text", repo, key)
	f.exec(t, "UPDATE submissions SET rewind_id = $1::bigint WHERE repo_id = $2 AND feature_key = $3", id, repo, key)
	f.exec(t, "UPDATE feedback SET rewind_id = $1::bigint WHERE repo_id = $2 AND feature_key = $3", id, repo, key)
}

func TestState_IgnoresClosedRows(t *testing.T) {
	fx := newGfx(t, "rw")
	f := fx.feat(t, "r/rw-1", "K-1", "FULL")
	f.submitOk("BRD")
	if r := f.action("reject", fx.ownerS, "BRD", str("thiếu")); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	f.submitOk("BRD")
	st := fx.envelope(t, f)
	if len(st.Submissions) != 1 || len(st.Feedback) != 1 || st.Attempt != 1 {
		t.Fatalf("trước khi khép: %+v", st)
	}

	fx.closeAttempt(t, "r/rw-1", "K-1")
	fx.exec(t, "UPDATE features SET attempt = 3 WHERE repo_id = 'r/rw-1' AND feature_key = 'K-1'")
	st = fx.envelope(t, f)
	if len(st.Submissions) != 0 || len(st.Feedback) != 0 || len(st.Approvals) != 0 || st.Attempt != 3 {
		t.Fatalf("sau khi khép: %+v", st)
	}
	if n := fx.count(t, "SELECT count(*) FROM submissions WHERE repo_id = 'r/rw-1'"); n != 2 {
		t.Fatal("bản ghi cũ phải còn", n)
	}

	// nộp lại ở lần làm mới: chỉ thấy bản mới; số lần nộp (resubmission) không tính bản đã khép
	f.submitOk("BRD")
	st = fx.envelope(t, f)
	if len(st.Submissions) != 1 || st.Attempt != 3 {
		t.Fatalf("lần mới: %+v", st)
	}
}

func TestState_AttemptDefaultsToOneAndRewoundKindIsStorable(t *testing.T) {
	fx := newGfx(t, "rw2")
	f := fx.feat(t, "r/rw2-1", "K-1", "FULL")
	if st := fx.envelope(t, f); st.Attempt != 1 {
		t.Fatal(st.Attempt)
	}
	fx.exec(t, "INSERT INTO feedback(repo_id, feature_key, gate, kind, by_login, comment) VALUES ('r/rw2-1','K-1','BRD','REWOUND','x','c')")
	fx.exec(t, "UPDATE pending_actions SET status = status WHERE false")
	fx.exec(t, "INSERT INTO pending_actions(repo_id, feature_key, kind, step, created_by, expires_at, status) "+
		"VALUES ('r/rw2-1','K-1','ACCEPT_STEP','S1','x', now(), 'CANCELLED')")
}

// Phong bì cũ (không có "attempt") vẫn đọc được: Attempt = 0.
func TestFeatureGateState_OldEnvelopeWithoutAttempt(t *testing.T) {
	st, err := snapshot.FeatureGateStateFromJSON([]byte(`{"repo":"r","feature":"k","mode":"web","submissions":[],"approvals":[],"feedback":[]}`))
	if err != nil || st.Attempt != 0 {
		t.Fatal(st, err)
	}
	b, _ := snapshot.FeatureGateState{Attempt: 2}.ToJSON()
	st, err = snapshot.FeatureGateStateFromJSON(b)
	if err != nil || st.Attempt != 2 {
		t.Fatal(st, err)
	}
}

func (ft *feat) rewindBody(checkpoint, reqID string, gates ...string) string {
	gj, _ := json.Marshal(gates)
	return fmt.Sprintf(`{"repo":"%s","feature":"%s","checkpoint":"%s","requestId":"%s","gates":%s}`,
		ft.repo, ft.key, checkpoint, reqID, gj)
}

// rewind gọi bằng token approve (chỉ CLI được quay về).
func (ft *feat) rewind(tok, checkpoint, reqID string, gates ...string) webtest.Resp {
	return ft.f.w.PostBearer("/api/gates/rewind", ft.rewindBody(checkpoint, reqID, gates...), tok)
}

func apprOf(st snapshot.FeatureGateState, g string) *snapshot.Appr {
	for i := range st.Approvals {
		if st.Approvals[i].Gate == g {
			return &st.Approvals[i]
		}
	}
	return nil
}

func TestRewind_ToDesign(t *testing.T) {
	fx := newGfx(t, "rw3")
	f := fx.feat(t, "r/rw3-1", "K-1", "FULL")
	f.openGates("BRD", "DESIGN", "CASES")
	before := apprOf(fx.envelope(t, f), "BRD")
	if before == nil {
		t.Fatal("BRD chưa duyệt")
	}
	r := f.rewind(fx.approveOwner, "design", "req-1", "DESIGN", "CASES", "REVIEW")
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	st := fx.verified(t, r.Body)
	if st.Attempt != 2 {
		t.Fatal("attempt", st.Attempt)
	}
	if a := apprOf(st, "BRD"); a == nil || a.Hash != before.Hash {
		t.Fatalf("BRD phải giữ approval: %+v", st.Approvals)
	}
	for _, g := range []string{"DESIGN", "CASES", "REVIEW"} {
		if apprOf(st, g) != nil {
			t.Fatal("approval còn hiệu lực:", g)
		}
	}
	if len(st.Submissions) != 1 || st.Submissions[0].Gate != "BRD" {
		t.Fatalf("submission hiện tại: %+v", st.Submissions)
	}
	if len(st.Feedback) != 0 {
		t.Fatalf("REWOUND không lộ ra trạng thái: %+v", st.Feedback)
	}
	if n := fx.count(t, "SELECT count(*) FROM feature_rewinds WHERE repo_id='r/rw3-1' AND snapshot <> '{}'::jsonb AND from_attempt=1 AND to_attempt=2 AND checkpoint='design'"); n != 1 {
		t.Fatal("feature_rewinds", n)
	}
	if n := fx.count(t, "SELECT count(*) FROM submissions WHERE repo_id='r/rw3-1' AND rewind_id IS NOT NULL"); n != 2 {
		t.Fatal("submission đã khép", n)
	}
	if n := fx.count(t, "SELECT count(*) FROM feedback WHERE repo_id='r/rw3-1' AND kind='REWOUND' AND gate='DESIGN' AND rewind_id IS NULL"); n != 1 {
		t.Fatal("REWOUND", n)
	}
	want := "Tính năng K-1 đã được quay về đầu design bởi " + fx.owner + ". Mọi thứ sau mốc đã bị xoá; bắt đầu lại bằng /aisdlc:design."
	if n := fx.count(t, "SELECT count(*) FROM instructions WHERE repo_id='r/rw3-1' AND text = $1", want); n != 1 {
		t.Fatal("instruction", fx.strs(t, "SELECT text FROM instructions WHERE repo_id='r/rw3-1'"))
	}
	if n := fx.count(t, "SELECT count(*) FROM audit WHERE action='feature.rewind'"); n != 1 {
		t.Fatal("audit", n)
	}

	// nộp lại không bị REWOUND chặn
	f.submitOk("DESIGN")
	st = fx.envelope(t, f)
	found := false
	for _, s := range st.Submissions {
		found = found || s.Gate == "DESIGN"
	}
	if !found || st.Attempt != 2 {
		t.Fatalf("nộp lại: %+v", st)
	}
	f.approveOk("DESIGN")
}

func TestRewind_StepCheckpointCommand(t *testing.T) {
	if got := gates.RewindInstruction("K", "S2", "u"); !strings.HasSuffix(got, "bắt đầu lại bằng aisdlc step start S2.") {
		t.Fatal(got)
	}
	for cp, cmd := range map[string]string{"brd": "/aisdlc:brd", "cases": "/aisdlc:cases", "review": "/aisdlc:review"} {
		if got := gates.RewindInstruction("K", cp, "u"); !strings.HasSuffix(got, "bằng "+cmd+".") {
			t.Fatal(got)
		}
	}
}

func TestRewind_SameRequestIDNoDouble(t *testing.T) {
	fx := newGfx(t, "rw4")
	f := fx.feat(t, "r/rw4-1", "K-1", "FULL")
	f.openGates("BRD", "DESIGN")
	for i := 0; i < 2; i++ {
		r := f.rewind(fx.approveOwner, "design", "same", "DESIGN", "CASES", "REVIEW")
		if r.Status != 200 {
			t.Fatal(r.Status, r.Body)
		}
		if st := fx.verified(t, r.Body); st.Attempt != 2 {
			t.Fatal("attempt", st.Attempt)
		}
	}
	if n := fx.count(t, "SELECT count(*) FROM feature_rewinds WHERE repo_id='r/rw4-1'"); n != 1 {
		t.Fatal(n)
	}
	if n := fx.count(t, "SELECT count(*) FROM feedback WHERE repo_id='r/rw4-1' AND kind='REWOUND'"); n != 1 {
		t.Fatal(n)
	}
}

func TestRewind_CancelsPendingActions(t *testing.T) {
	fx := newGfx(t, "rw5")
	f := fx.feat(t, "r/rw5-1", "K-1", "FULL")
	fx.exec(t, "INSERT INTO pending_actions(repo_id, feature_key, kind, step, created_by, expires_at) VALUES ('r/rw5-1','K-1','ACCEPT_STEP','S1','x', now() + interval '1 day')")
	if r := f.rewind(fx.approveOwner, "brd", "q", "BRD", "DESIGN", "CASES", "REVIEW"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if n := fx.count(t, "SELECT count(*) FROM pending_actions WHERE repo_id='r/rw5-1' AND status='CANCELLED'"); n != 1 {
		t.Fatal(n)
	}
}

// Lệnh điều khiển của lần làm cũ (mọi trạng thái) được gắn rewind_id và không còn hiện ở trang điều khiển hay chặn
// lệnh mới: sau quay về, nút Accept web hiện lại và gửi được lệnh mới.
func TestRewind_ArchivesOldAttemptActions(t *testing.T) {
	fx := newGfx(t, "rw12")
	f := fx.feat(t, "r/rw12-1", "K-1", "FULL")
	f.stepsJSON = `[{"id":"S1","status":"AWAITING_DEV","record":{"endTree":"t1"}}]`
	f.push()
	for _, st := range []string{"PENDING", "APPLIED", "STALE"} {
		fx.exec(t, "INSERT INTO pending_actions(repo_id, feature_key, kind, step, created_by, expires_at, status) "+
			"VALUES ('r/rw12-1','K-1','ACCEPT_STEP','S1','x', now() + interval '1 day', $1)", st)
	}
	if r := f.rewind(fx.approveOwner, "S1", "q", "REVIEW"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if n := fx.count(t, "SELECT count(*) FROM pending_actions WHERE repo_id='r/rw12-1' AND rewind_id IS NOT NULL"); n != 3 {
		t.Fatal("rewind_id", n)
	}
	view := func() []any {
		r := fx.w.GetSession("/api/features/r/rw12-1/K-1/control", fx.ownerS)
		if r.Status != 200 {
			t.Fatal("view", r.Status, r.Body)
		}
		l, _ := r.JSON(t)["actions"].([]any)
		return l
	}
	if l := view(); len(l) != 0 {
		t.Fatalf("lệnh lần làm cũ còn hiện: %v", l)
	}
	// lệnh PENDING của lần cũ (giả lập chưa bị huỷ) không được làm lệnh mới bị gộp vào nó
	fx.exec(t, "UPDATE pending_actions SET status = 'PENDING' WHERE repo_id = 'r/rw12-1'")
	body := `{"repo":"r/rw12-1","feature":"K-1","kind":"ACCEPT_STEP","step":"S1","endTree":"t1"}`
	if r := fx.w.PostSession("/api/control/actions", body, fx.ownerS, fx.ownerS.CSRF); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if l := view(); len(l) != 1 || l[0].(map[string]any)["status"] != "PENDING" {
		t.Fatalf("lệnh mới: %v", l)
	}
	if n := fx.count(t, "SELECT count(*) FROM pending_actions WHERE repo_id='r/rw12-1' AND rewind_id IS NULL"); n != 1 {
		t.Fatal("lệnh mới", n)
	}
}

func TestRewind_ForbiddenForNonOwner(t *testing.T) {
	fx := newGfx(t, "rw6")
	f := fx.feat(t, "r/rw6-1", "K-1", "FULL")
	if r := f.rewind(fx.approveDev, "brd", "q", "BRD", "DESIGN", "CASES", "REVIEW"); r.Status != 403 {
		t.Fatal(r.Status, r.Body)
	}
	if n := fx.count(t, "SELECT count(*) FROM feature_rewinds WHERE repo_id='r/rw6-1'"); n != 0 {
		t.Fatal(n)
	}
}

// Quay về chỉ từ CLI: phiên web (cookie) bị 403 dù là chủ tính năng; không đổi gì.
func TestRewind_SessionForbidden(t *testing.T) {
	fx := newGfx(t, "rw10")
	f := fx.feat(t, "r/rw10-1", "K-1", "FULL")
	body := f.rewindBody("brd", "q", "BRD", "DESIGN", "CASES", "REVIEW")
	for _, s := range []webtest.Session{fx.ownerS, fx.leadS} {
		if r := fx.w.PostSession("/api/gates/rewind", body, s, s.CSRF); r.Status != 403 {
			t.Fatal(r.Status, r.Body)
		}
	}
	if n := fx.count(t, "SELECT count(*) FROM feature_rewinds WHERE repo_id='r/rw10-1'"); n != 0 {
		t.Fatal(n)
	}
}

// Checkpoint hợp lệ không phân biệt hoa thường và khớp cổng đầu.
func TestRewind_CheckpointCaseInsensitive(t *testing.T) {
	fx := newGfx(t, "rw11")
	f := fx.feat(t, "r/rw11-1", "K-1", "FULL")
	for i, cp := range []string{"s2", "Review", "CASES"} {
		gs := []string{"REVIEW"}
		if cp == "CASES" {
			gs = []string{"CASES", "REVIEW"}
		}
		if r := f.rewind(fx.approveOwner, cp, fmt.Sprintf("q%d", i), gs...); r.Status != 200 {
			t.Fatal(cp, r.Status, r.Body)
		}
	}
}

func TestRewind_Validation(t *testing.T) {
	fx := newGfx(t, "rw7")
	f := fx.feat(t, "r/rw7-1", "K-1", "FULL")
	long := strings.Repeat("x", 201)
	for name, r := range map[string]webtest.Resp{
		"gates rỗng":       f.rewind(fx.approveOwner, "brd", "q"),
		"không liên tiếp":  f.rewind(fx.approveOwner, "design", "q", "DESIGN", "REVIEW"),
		"không tới cuối":   f.rewind(fx.approveOwner, "design", "q", "DESIGN", "CASES"),
		"checkpoint rỗng":  f.rewind(fx.approveOwner, " ", "q", "BRD", "DESIGN", "CASES", "REVIEW"),
		"requestId rỗng":   f.rewind(fx.approveOwner, "brd", "", "BRD", "DESIGN", "CASES", "REVIEW"),
		"requestId dài":    f.rewind(fx.approveOwner, "brd", long, "BRD", "DESIGN", "CASES", "REVIEW"),
		"cổng ngoài luồng": f.rewind(fx.approveOwner, "brd", "q", "BRD", "DESIGN", "CASES", "REVIEW", "X"),
		"checkpoint lạ":    f.rewind(fx.approveOwner, "x", "q", "BRD", "DESIGN", "CASES", "REVIEW"),
		"checkpoint S":     f.rewind(fx.approveOwner, "S", "q", "REVIEW"),
		"checkpoint S1x":   f.rewind(fx.approveOwner, "S1x", "q", "REVIEW"),
		"brd lệch cổng":    f.rewind(fx.approveOwner, "brd", "q", "DESIGN", "CASES", "REVIEW"),
		"design lệch cổng": f.rewind(fx.approveOwner, "design", "q", "CASES", "REVIEW"),
		"cases lệch cổng":  f.rewind(fx.approveOwner, "cases", "q", "REVIEW"),
		"review lệch cổng": f.rewind(fx.approveOwner, "review", "q", "CASES", "REVIEW"),
		"Sx lệch cổng":     f.rewind(fx.approveOwner, "S2", "q", "CASES", "REVIEW"),
	} {
		if r.Status != 400 {
			t.Fatal(name, r.Status, r.Body)
		}
	}
	lite := fx.feat(t, "r/rw7-2", "K-1", "LITE")
	if r := lite.rewind(fx.approveOwner, "brd", "q", "BRD", "DESIGN", "CASES", "REVIEW"); r.Status != 400 {
		t.Fatal("lite", r.Status, r.Body)
	}
	if n := fx.count(t, "SELECT count(*) FROM feature_rewinds WHERE repo_id IN ('r/rw7-1','r/rw7-2')"); n != 0 {
		t.Fatal(n)
	}
}

func TestRewind_NoFeatureRow409(t *testing.T) {
	fx := newGfx(t, "rw8")
	f := fx.feat(t, "r/rw8-1", "K-1", "FULL")
	f.key = "K-2" // chưa đồng bộ
	r := f.rewind(fx.approveOwner, "brd", "q", "BRD", "DESIGN", "CASES", "REVIEW")
	if r.Status != 409 || !strings.Contains(fx.errOf(t, r), "aisdlc status") {
		t.Fatal(r.Status, r.Body)
	}
}

func TestRewind_DropsUndeliveredInstructions(t *testing.T) {
	fx := newGfx(t, "rw9")
	f := fx.feat(t, "r/rw9-1", "K-1", "FULL")
	fx.exec(t, "INSERT INTO instructions(repo_id, feature_key, by_login, text, source) VALUES ('r/rw9-1','K-1','x','cũ chưa giao','web')")
	fx.exec(t, "INSERT INTO instructions(repo_id, feature_key, by_login, text, source, delivered_at) VALUES ('r/rw9-1','K-1','x','cũ đã giao','web', now())")
	if r := f.rewind(fx.approveOwner, "brd", "q", "BRD", "DESIGN", "CASES", "REVIEW"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	r := fx.w.GetBearer("/api/cli/control?repo=r/rw9-1&feature=K-1", fx.ingest)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	pk := fx.w.GetBearer("/api/cli/pubkey", fx.ingest)
	pub, err := snapshot.PublicKeyFromBase64(pk.JSON(t)["publicKey"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var se snapshot.SignedEnvelope
	if err := json.Unmarshal([]byte(r.Body), &se); err != nil {
		t.Fatal(err)
	}
	payload, ok := se.Verify(pub)
	if !ok {
		t.Fatal("chữ ký sai")
	}
	c, err := snapshot.FeatureControlFromJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Instructions) != 1 || !strings.Contains(c.Instructions[0].Text, "quay về đầu brd") {
		t.Fatalf("%+v", c.Instructions)
	}
	if n := fx.count(t, "SELECT count(*) FROM instructions WHERE repo_id='r/rw9-1' AND text='cũ chưa giao' AND delivered_at IS NULL AND rewind_id IS NOT NULL"); n != 1 {
		t.Fatal(n)
	}
}
