package gates_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/gates"
)

// Port CliGateIT: nộp cổng và đọc trạng thái (phần pubkey/mode ở mode_it_test.go, Task 3).

// cliSubmission: body nộp với 2 artifact (1 thiếu) và hash đúng theo thứ tự đó, trừ khi truyền hash khác.
func cliSubmission(repo, feature, g, hashOverride, findings string) string {
	design := []byte("# Design\n")
	hash := model.SHA256Contents([]model.Content{{Path: "aisdlc/features/K/design.md", Data: design},
		{Path: "aisdlc/features/K/design.yaml"}})
	if hashOverride != "" {
		hash = hashOverride
	}
	return fmt.Sprintf(`{"repo":"%s","feature":"%s","gate":"%s","hash":"%s","findings":%s,
	 "artifacts":[{"path":"aisdlc/features/K/design.md","content":"%s"},
	              {"path":"aisdlc/features/K/design.yaml","content":null}]}`, repo, feature, g, hash, findings,
		base64.StdEncoding.EncodeToString(design))
}

func (f *gfx) pin(t *testing.T, repo string) {
	t.Helper()
	if r := f.w.PostBearer("/api/cli/mode", `{"repo":"`+repo+`","configured":"web"}`, f.ingest); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
}

func TestSubmitWithCorrectHashThenReadGates(t *testing.T) {
	fx := newGfx(t, "cli")
	fx.pin(t, "r/cli-sub")
	r := fx.w.PostBearer("/api/cli/submissions", cliSubmission("r/cli-sub", "K-1", "BRD", "", `[{"code":"D1","message":"thiếu"}]`), fx.ingest)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	s := fx.verified(t, r.Body)
	if s.Repo != "r/cli-sub" || s.Feature != "K-1" || s.Mode != "web" || len(s.Submissions) != 1 || s.Submissions[0].Gate != "BRD" ||
		len(s.Submissions[0].Findings) != 1 || s.Submissions[0].Findings[0].Code != "D1" || len(s.Approvals) != 0 {
		t.Fatalf("%+v", s)
	}
	// lưu toàn văn artifact, audit gate.submit (không chứa token)
	if a := fx.str(t, "SELECT artifacts::text FROM submissions WHERE repo_id = 'r/cli-sub'"); !strings.Contains(a, "design.md") {
		t.Fatal(a)
	}
	audit := fx.strs(t, "SELECT a::text FROM audit a WHERE action = 'gate.submit' AND repo_id = 'r/cli-sub'")
	if len(audit) != 1 || strings.Contains(audit[0], fx.ingest) {
		t.Fatal(audit)
	}
	if s := fx.str(t, "SELECT login || '|' || channel || '|' || target || '|' || feature_key FROM audit WHERE action = 'gate.submit' "+
		"AND repo_id = 'r/cli-sub'"); s != fx.owner+"|cli|BRD|K-1" {
		t.Fatal(s)
	}
	g := fx.w.GetBearer("/api/cli/gates?repo=r/cli-sub&feature=K-1", fx.ingest)
	if g.Status != 200 {
		t.Fatal(g.Status, g.Body)
	}
	gs := fx.verified(t, g.Body)
	if len(gs.Submissions) != 1 || gs.Submissions[0].Gate != "BRD" || gs.IssuedAt.IsZero() {
		t.Fatalf("%+v", gs)
	}
}

func TestWrongHashIs400AndLocalRepoIs409(t *testing.T) {
	fx := newGfx(t, "cli")
	fx.pin(t, "r/cli-bad")
	bad := fx.w.PostBearer("/api/cli/submissions", cliSubmission("r/cli-bad", "K-1", "DESIGN", "deadbeef", "[]"), fx.ingest)
	if bad.Status != 400 || fx.errOf(t, bad) != "Hash không khớp nội dung" {
		t.Fatal(bad.Status, bad.Body)
	}
	if r := fx.w.PostBearer("/api/cli/submissions", cliSubmission("r/cli-bad", "K-1", "NOPE", "", "[]"), fx.ingest); r.Status != 400 {
		t.Fatal(r.Status, r.Body)
	}
	local := fx.w.PostBearer("/api/cli/submissions", cliSubmission("r/cli-loc", "K-1", "DESIGN", "", "[]"), fx.ingest)
	if local.Status != 409 || fx.errOf(t, local) != "Repo không ở chế độ duyệt trên web" || local.JSON(t)["code"] != "NOT_WEB_MODE" {
		t.Fatal(local.Status, local.Body)
	}
	if n := fx.count(t, "SELECT count(*) FROM submissions WHERE repo_id IN ('r/cli-bad','r/cli-loc')"); n != 0 {
		t.Fatal(n)
	}
	// đọc trạng thái repo local: mode local, danh sách rỗng
	gs := fx.verified(t, fx.w.GetBearer("/api/cli/gates?repo=r/cli-loc&feature=K-1", fx.ingest).Body)
	if gs.Mode != "local" || len(gs.Submissions) != 0 || len(gs.Approvals) != 0 || len(gs.Feedback) != 0 {
		t.Fatalf("%+v", gs)
	}
}

func TestSubmissionBeforeLatestFeedbackOfGateIsDropped(t *testing.T) {
	fx := newGfx(t, "cli")
	fx.pin(t, "r/cli-fb")
	if r := fx.w.PostBearer("/api/cli/submissions", cliSubmission("r/cli-fb", "K-1", "BRD", "", "[]"), fx.ingest); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	fx.exec(t, "INSERT INTO feedback(repo_id, feature_key, gate, kind, by_login, at, comment) "+
		"VALUES ('r/cli-fb','K-1','BRD','REJECTED','lead.l', now() + interval '1 second','thiếu')")
	gs := fx.verified(t, fx.w.GetBearer("/api/cli/gates?repo=r/cli-fb&feature=K-1", fx.ingest).Body)
	if len(gs.Submissions) != 0 || len(gs.Feedback) != 1 || gs.Feedback[0].Kind != "REJECTED" {
		t.Fatalf("%+v", gs)
	}
}

// Thêm: body và tham số sai của API nộp; giới hạn 8 MB với câu lỗi "Payload quá lớn".
func TestSubmissionBodyValidation(t *testing.T) {
	fx := newGfx(t, "cli")
	fx.pin(t, "r/cli-v")
	cases := map[string]string{
		`[]`: "Body phải là object JSON",
		`{`:  "JSON không hợp lệ",
		`{"repo":"r/cli-v","feature":"K","gate":"BRD","hash":"h","findings":{},"artifacts":[]}`:                                  "findings phải là mảng",
		`{"repo":"r/cli-v","feature":"K","gate":"BRD","hash":"h","findings":["x"],"artifacts":[]}`:                               "finding phải là object {code, message}",
		`{"repo":"r/cli-v","feature":"K","gate":"BRD","hash":"h","findings":[{"message":"m"}],"artifacts":[]}`:                   "finding thiếu code",
		`{"repo":"r/cli-v","feature":"K","gate":"BRD","hash":"h","artifacts":{}}`:                                                "Thiếu artifacts (mảng)",
		`{"repo":"r/cli-v","feature":"K","gate":"BRD","hash":"h","artifacts":[{"path":"a","content":1}]}`:                        "content phải là base64 hoặc null",
		`{"repo":"r/cli-v","feature":"K","gate":"BRD","hash":"h","artifacts":[{"path":"a","content":"@@"}]}`:                     "content không phải base64: a",
		`{"repo":"r/cli-v","feature":"K","gate":"BRD","hash":"h","artifacts":[{"path":"a","content":null}],"risk":1}`:            "risk phải là object",
		`{"repo":"r/cli-v","feature":"K","gate":"BRD","hash":"h","artifacts":[{"path":"a","content":null}],"risk":{"level":""}}`: "risk.level phải là chuỗi",
		`{"repo":"r/cli-v","feature":"K\u0000","gate":"BRD","hash":"h","artifacts":[]}`:                                          "feature chứa ký tự NUL (U+0000)",
		`{"repo":"r/cli-v","feature":" ","gate":"BRD","hash":"h","artifacts":[]}`:                                                "Thiếu feature",
	}
	for body, want := range cases {
		r := fx.w.PostBearer("/api/cli/submissions", body, fx.ingest)
		if r.Status != 400 || fx.errOf(t, r) != want {
			t.Errorf("%s: %d %s, muốn %q", body, r.Status, r.Body, want)
		}
	}
	big := `{"repo":"r/cli-v","pad":"` + strings.Repeat("x", gates.MaxBytes) + `"}`
	if r := fx.w.PostBearer("/api/cli/submissions", big, fx.ingest); r.Status != 413 || fx.errOf(t, r) != "Payload quá lớn" {
		t.Fatal(r.Status, r.Body)
	}
	// dưới 8 MB nhưng trên giới hạn JSON mặc định 1 MB vẫn được đọc (lỗi nghiệp vụ, không phải 413)
	mid := `{"repo":"r/cli-v","pad":"` + strings.Repeat("x", 2<<20) + `"}`
	if r := fx.w.PostBearer("/api/cli/submissions", mid, fx.ingest); r.Status != 400 || fx.errOf(t, r) != "Thiếu feature" {
		t.Fatal(r.Status, r.Body)
	}
	if n := fx.count(t, "SELECT count(*) FROM submissions"); n != 0 {
		t.Fatal(n)
	}
}

// TestGatesEnvelopeVerifiesInCLI: phong bì GET /api/cli/gates đọc được bằng code CLI Go (ReadWebGateState / WebGateStore
// với khóa ghim từ /api/cli/pubkey).
func TestGatesEnvelopeVerifiesInCLI(t *testing.T) {
	fx := newGfx(t, "env")
	f := fx.feat(t, "r/env-1", "K-1", "FULL")
	f.openGates("BRD")
	f.submitOk("DESIGN")
	if r := f.action("reject", fx.ownerS, "DESIGN", str("thiếu")); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	body := fx.envelopeRaw(t, f)
	cache := filepath.Join(t.TempDir(), "gates.json")
	if err := os.WriteFile(cache, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	pub, err := snapshot.PublicKeyFromBase64(fx.w.GetBearer("/api/cli/pubkey", fx.ingest).JSON(t)["publicKey"].(string))
	if err != nil {
		t.Fatal(err)
	}
	st, ok := gate.ReadWebGateState(cache, pub, f.repo, model.FeatureKey(f.key))
	if !ok {
		t.Fatal("CLI không đọc được phong bì", body)
	}
	if st.Mode != "web" || len(st.Approvals) != 1 || st.Approvals[0].Approver != fx.owner || len(st.Feedback) != 1 ||
		st.Feedback[0].Comment != "thiếu" || st.IssuedAt.IsZero() {
		t.Fatalf("%+v", st)
	}
	store := gate.NewWebGateStore(cache, pub, f.repo, model.FeatureKey(f.key))
	if a, ok, _ := store.Approval(gate.BRD); !ok || a.Hash != f.hash("BRD") || a.Channel != "web" {
		t.Fatalf("%+v", a)
	}
	if _, ok, _ := store.Submission(gate.DESIGN); ok {
		t.Fatal("bản nộp DESIGN đã bị trả lại")
	}
	// phong bì của tính năng khác hoặc khóa khác: CLI không nhận
	if _, ok := gate.ReadWebGateState(cache, pub, f.repo, model.FeatureKey("K-2")); ok {
		t.Fatal("sai tính năng vẫn nhận")
	}
}

// TestConcurrentApproveOneWins (Review Focus 4): hai lead/owner duyệt cùng phiên bản cùng lúc. Khóa theo tính năng
// tuần tự hóa: đúng một bản ghi approval và một dòng audit; bản đến sau thấy approval cùng hash nên trả trạng thái
// idempotent (200, như luật 7 của Java), không ghi trùng; cả hai thấy cùng một người duyệt.
func TestConcurrentApproveOneWins(t *testing.T) {
	fx := newGfx(t, "cc")
	for round := 0; round < 5; round++ {
		repo := fmt.Sprintf("r/cc-%d", round)
		f := fx.feat(t, repo, "K-1", "FULL")
		f.submitOk("BRD")
		sessions := []struct{ s, csrf string }{{fx.ownerS.ID, fx.ownerS.CSRF}, {fx.leadS.ID, fx.leadS.CSRF}, {fx.ownerS.ID, fx.ownerS.CSRF}, {fx.leadS.ID, fx.leadS.CSRF}}
		var wg sync.WaitGroup
		res := make([]httpResult, len(sessions))
		start := make(chan struct{})
		for i, s := range sessions {
			wg.Add(1)
			go func(i int, id, csrf string) { // không gọi t.Fatal trong goroutine: chỉ ghi kết quả
				defer wg.Done()
				<-start
				res[i] = fx.post(context.Background(), "/api/gates/approve", f.approveBody("BRD", f.hash("BRD")), sessionHdr(id, csrf))
			}(i, s.s, s.csrf)
		}
		close(start)
		wg.Wait()
		approvers := make([]string, len(res))
		status := make([]int, len(res))
		for i, r := range res {
			if r.err != nil {
				t.Fatalf("vòng %d: %v", round, r.err)
			}
			status[i] = r.status
			if r.status == 200 {
				approvers[i] = apprKeys(fx.verified(t, r.body).Approvals, func(a snapshot.Appr) string { return a.Approver })
			}
		}
		for i := range status {
			if status[i] != 200 || approvers[i] != approvers[0] || approvers[i] == "" {
				t.Fatalf("vòng %d: %v %v", round, status, approvers)
			}
		}
		if n := fx.approvals(t, repo, "BRD"); n != 1 {
			t.Fatalf("vòng %d: %d approval", round, n)
		}
		if n := fx.count(t, "SELECT count(*) FROM audit WHERE action = 'gate.approve' AND repo_id = $1", repo); n != 1 {
			t.Fatalf("vòng %d: %d audit", round, n)
		}
		if a := fx.str(t, "SELECT approver FROM approvals WHERE repo_id = $1", repo); a != approvers[0] {
			t.Fatal(a, approvers[0])
		}
	}
}

// Duyệt đúng lúc bản nộp đổi: bản duyệt hash cũ hoặc thắng trước khi nộp lại (rồi hiển thị STALE khi snapshot đổi), hoặc
// nhận 409 "Nội dung đã đổi kể từ lúc nộp, cần nộp lại"; không bao giờ có approval cho hash không phải bản nộp lúc đó.
func TestApproveRacingResubmissionNeverApprovesChangedContent(t *testing.T) {
	fx := newGfx(t, "rc")
	for round := 0; round < 5; round++ {
		repo := fmt.Sprintf("r/rc-%d", round)
		f := fx.feat(t, repo, "K-1", "FULL")
		f.submitOk("BRD")
		oldHash := f.hash("BRD")
		f.text["BRD"] = "BRD v2"
		f.push() // snapshot mới: currentHash = v2; bản nộp hiện tại vẫn là v1
		var wg sync.WaitGroup
		var ar, sr httpResult
		wg.Add(2)
		go func() {
			defer wg.Done()
			ar = fx.post(context.Background(), "/api/gates/approve", f.approveBody("BRD", f.hash("BRD")), sessionHdr(fx.ownerS.ID, fx.ownerS.CSRF))
		}()
		go func() {
			defer wg.Done()
			sr = fx.post(context.Background(), "/api/cli/submissions", f.submissionBody("BRD", "[]"), bearerHdr(fx.ingest))
		}()
		wg.Wait()
		if ar.err != nil || sr.err != nil || sr.status != 200 {
			t.Fatal("submit", sr.status, sr.err, ar.err)
		}
		approve, approveErr := ar.status, ar.body
		switch approve {
		case 200: // nộp lại thắng trước: duyệt đúng bản v2
			if h := fx.str(t, "SELECT hash FROM approvals WHERE repo_id = $1", repo); h != f.hash("BRD") || h == oldHash {
				t.Fatal(h)
			}
		case 409:
			if !strings.Contains(approveErr, "Nội dung đã đổi kể từ lúc nộp, cần nộp lại") {
				t.Fatal(approveErr)
			}
			if n := fx.approvals(t, repo, "BRD"); n != 0 {
				t.Fatal(n)
			}
		default:
			t.Fatal(approve, approveErr)
		}
	}
	if n := fx.count(t, "SELECT count(*) FROM approvals a JOIN submissions s ON s.id = a.submission_id WHERE a.hash <> s.hash"); n != 0 {
		t.Fatal(n)
	}
}
