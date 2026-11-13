package gates_test

import (
	"testing"

	"aisdlc/internal/snapshot"
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
