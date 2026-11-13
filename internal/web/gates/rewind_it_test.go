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

func (ft *feat) rewind(s webtest.Session, checkpoint, reqID string, gates ...string) webtest.Resp {
	gj, _ := json.Marshal(gates)
	return ft.f.w.PostSession("/api/gates/rewind", fmt.Sprintf(`{"repo":"%s","feature":"%s","checkpoint":"%s","requestId":"%s","gates":%s}`,
		ft.repo, ft.key, checkpoint, reqID, gj), s, s.CSRF)
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
	r := f.rewind(fx.ownerS, "design", "req-1", "DESIGN", "CASES", "REVIEW")
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
		r := f.rewind(fx.ownerS, "design", "same", "DESIGN", "CASES", "REVIEW")
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
	if r := f.rewind(fx.ownerS, "brd", "q", "BRD", "DESIGN", "CASES", "REVIEW"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if n := fx.count(t, "SELECT count(*) FROM pending_actions WHERE repo_id='r/rw5-1' AND status='CANCELLED'"); n != 1 {
		t.Fatal(n)
	}
}

func TestRewind_ForbiddenForNonOwner(t *testing.T) {
	fx := newGfx(t, "rw6")
	f := fx.feat(t, "r/rw6-1", "K-1", "FULL")
	if r := f.rewind(fx.devS, "brd", "q", "BRD", "DESIGN", "CASES", "REVIEW"); r.Status != 403 {
		t.Fatal(r.Status, r.Body)
	}
	if n := fx.count(t, "SELECT count(*) FROM feature_rewinds WHERE repo_id='r/rw6-1'"); n != 0 {
		t.Fatal(n)
	}
}

func TestRewind_Validation(t *testing.T) {
	fx := newGfx(t, "rw7")
	f := fx.feat(t, "r/rw7-1", "K-1", "FULL")
	long := strings.Repeat("x", 201)
	for name, r := range map[string]webtest.Resp{
		"gates rỗng":       f.rewind(fx.ownerS, "brd", "q"),
		"không liên tiếp":  f.rewind(fx.ownerS, "design", "q", "DESIGN", "REVIEW"),
		"không tới cuối":   f.rewind(fx.ownerS, "design", "q", "DESIGN", "CASES"),
		"checkpoint rỗng":  f.rewind(fx.ownerS, " ", "q", "BRD", "DESIGN", "CASES", "REVIEW"),
		"requestId rỗng":   f.rewind(fx.ownerS, "brd", "", "BRD", "DESIGN", "CASES", "REVIEW"),
		"requestId dài":    f.rewind(fx.ownerS, "brd", long, "BRD", "DESIGN", "CASES", "REVIEW"),
		"cổng ngoài luồng": f.rewind(fx.ownerS, "brd", "q", "BRD", "DESIGN", "CASES", "REVIEW", "X"),
	} {
		if r.Status != 400 {
			t.Fatal(name, r.Status, r.Body)
		}
	}
	lite := fx.feat(t, "r/rw7-2", "K-1", "LITE")
	if r := lite.rewind(fx.ownerS, "brd", "q", "BRD", "DESIGN", "CASES", "REVIEW"); r.Status != 400 {
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
	r := f.rewind(fx.ownerS, "brd", "q", "BRD", "DESIGN", "CASES", "REVIEW")
	if r.Status != 409 || !strings.Contains(fx.errOf(t, r), "aisdlc status") {
		t.Fatal(r.Status, r.Body)
	}
}
