package gates_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/snapshot"
)

// Port ImportIT (5): nhập duyệt từ local (chưa hiệu lực tới khi owner xác nhận). repoModeChangeOnlyByLead đã ở
// mode_it_test.go (TestRepoModeChangeOnlyByLead, Task 3).

func (ft *feat) importBody(gate string) string {
	return strings.Replace(ft.submissionBody(gate, "[]"), `"findings"`, `"approver":"local.dev","at":"2026-11-01T10:00:00Z","findings"`, 1)
}

func (ft *feat) confirmBody(gate string) string {
	return fmt.Sprintf(`{"repo":"%s","feature":"%s","gate":"%s"}`, ft.repo, ft.key, gate)
}

func (ft *feat) pendingImports() []any {
	ft.t.Helper()
	d := ft.f.w.GetSession("/api/features/"+ft.repo+"/"+ft.key, ft.f.ownerS).JSON(ft.t)
	p, _ := d["pendingImports"].([]any)
	return p
}

func TestImportIsPendingUntilOwnerConfirms(t *testing.T) {
	fx := newGfx(t, "im")
	f := fx.feat(t, "r/im-1", "K-1", "FULL")
	s := fx.okState(t, fx.w.PostBearer("/api/cli/import", f.importBody("BRD"), fx.ingest))
	if len(s.Approvals) != 0 || len(fx.envelope(t, f).Approvals) != 0 {
		t.Fatal(s.Approvals)
	}
	if got := fx.str(t, "SELECT channel || '|' || original_approver || '|' || (original_at = '2026-11-01T10:00:00Z'::timestamptz) "+
		"|| '|' || (confirmed_at IS NULL) || '|' || approver FROM approvals WHERE repo_id = 'r/im-1'"); got != "import|local.dev|true|true|im.owner" {
		t.Fatal(got)
	}
	if got := fx.str(t, "SELECT login || '|' || channel || '|' || target || '|' || (detail->>'originalApprover') || '|' || "+
		"(detail->>'originalAt') FROM audit WHERE action = 'gate.import' AND repo_id = 'r/im-1'"); got != "im.owner|cli|BRD|local.dev|2026-11-01T10:00:00Z" {
		t.Fatal(got)
	}

	ps := f.pendingImports()
	if len(ps) != 1 {
		t.Fatal(ps)
	}
	p := ps[0].(map[string]any)
	if p["gate"] != "BRD" || p["hash"] != f.hash("BRD") || p["originalApprover"] != "local.dev" || p["originalAt"] != "2026-11-01T10:00:00Z" {
		t.Fatal(p)
	}

	// người khác owner (kể cả lead) không xác nhận được; Bearer approve cũng không (chỉ phiên)
	for i, r := range []int{
		fx.w.PostSession("/api/gates/confirm-import", f.confirmBody("BRD"), fx.devS, fx.devS.CSRF).Status,
		fx.w.PostSession("/api/gates/confirm-import", f.confirmBody("BRD"), fx.leadS, fx.leadS.CSRF).Status,
		fx.w.PostBearer("/api/gates/confirm-import", f.confirmBody("BRD"), fx.approveOwner).Status,
		fx.w.PostSession("/api/gates/confirm-import", f.confirmBody("BRD"), fx.ownerS, "").Status,
	} {
		if r != 403 {
			t.Fatal(i, r)
		}
	}
	if len(fx.envelope(t, f).Approvals) != 0 {
		t.Fatal("bản nhập có hiệu lực trước khi xác nhận")
	}

	ok := fx.w.PostSession("/api/gates/confirm-import", f.confirmBody("BRD"), fx.ownerS, fx.ownerS.CSRF)
	if ok.Status != 200 {
		t.Fatal(ok.Status, ok.Body)
	}
	if got := apprKeys(fx.envelope(t, f).Approvals, func(a snapshot.Appr) string { return a.Gate + "|" + a.Channel }); got != "BRD|import" {
		t.Fatal(got)
	}
	if n := fx.count(t, "SELECT count(*) FROM audit WHERE action = 'gate.confirm-import' AND repo_id = 'r/im-1' "+
		"AND login = 'im.owner' AND channel = 'web'"); n != 1 {
		t.Fatal(n)
	}
	if ps := f.pendingImports(); len(ps) != 0 {
		t.Fatal(ps)
	}
	// không còn gì chờ xác nhận
	if again := fx.w.PostSession("/api/gates/confirm-import", f.confirmBody("BRD"), fx.ownerS, fx.ownerS.CSRF); again.Status != 409 {
		t.Fatal(again.Status, again.Body)
	}
}

func TestMigrateWholeFullFeatureThenConfirmInOrder(t *testing.T) {
	fx := newGfx(t, "im")
	f := fx.feat(t, "r/im-full", "K-1", "FULL")
	// web migrate gửi liền BRD, DESIGN, CASES: cổng trước mới chỉ là bản nhập chờ xác nhận
	for _, g := range []string{"BRD", "DESIGN", "CASES"} {
		if r := fx.w.PostBearer("/api/cli/import", f.importBody(g), fx.ingest); r.Status != 200 {
			t.Fatal(g, r.Status, r.Body)
		}
	}
	if ps := f.pendingImports(); len(ps) != 3 {
		t.Fatal(ps)
	}
	if len(fx.envelope(t, f).Approvals) != 0 {
		t.Fatal("bản nhập có hiệu lực")
	}
	// xác nhận theo thứ tự cổng
	early := fx.w.PostSession("/api/gates/confirm-import", f.confirmBody("DESIGN"), fx.ownerS, fx.ownerS.CSRF)
	if early.Status != 409 || fx.errOf(t, early) != "Xác nhận cổng trước trước" {
		t.Fatal(early.Status, early.Body)
	}
	if len(fx.envelope(t, f).Approvals) != 0 {
		t.Fatal("xác nhận sai thứ tự mà có hiệu lực")
	}
	for _, g := range []string{"BRD", "DESIGN", "CASES"} {
		if r := fx.w.PostSession("/api/gates/confirm-import", f.confirmBody(g), fx.ownerS, fx.ownerS.CSRF); r.Status != 200 {
			t.Fatal(g, r.Status, r.Body)
		}
	}
	if got := gatesOf(fx.envelope(t, f).Approvals); got != "BRD,DESIGN,CASES" {
		t.Fatal(got)
	}
	if ps := f.pendingImports(); len(ps) != 0 {
		t.Fatal(ps)
	}
}

func TestImportChecksHashPreviousGatesAndArtifacts(t *testing.T) {
	fx := newGfx(t, "im")
	f := fx.feat(t, "r/im-2", "K-1", "FULL")
	locked := fx.w.PostBearer("/api/cli/import", f.importBody("DESIGN"), fx.ingest)
	if locked.Status != 409 || fx.errOf(t, locked) != "Cổng trước chưa được duyệt" {
		t.Fatal(locked.Status, locked.Body)
	}
	badHash := fx.w.PostBearer("/api/cli/import", strings.ReplaceAll(f.importBody("BRD"), f.hash("BRD"), "deadbeef"), fx.ingest)
	if badHash.Status != 400 || fx.errOf(t, badHash) != "Hash không khớp nội dung" {
		t.Fatal(badHash.Status, badHash.Body)
	}
	noArtifacts := regexp.MustCompile(`"artifacts":\[.*]`).ReplaceAllString(f.importBody("BRD"), `"artifacts":[]`)
	if r := fx.w.PostBearer("/api/cli/import", noArtifacts, fx.ingest); r.Status != 400 {
		t.Fatal(r.Status, r.Body)
	}
	noApprover := fx.w.PostBearer("/api/cli/import", strings.Replace(f.importBody("BRD"), `"approver":"local.dev",`, "", 1), fx.ingest)
	if noApprover.Status != 400 || fx.errOf(t, noApprover) != "Thiếu approver" {
		t.Fatal(noApprover.Status, noApprover.Body)
	}
	badAt := fx.w.PostBearer("/api/cli/import", strings.Replace(f.importBody("BRD"), "2026-11-01T10:00:00Z", "hôm qua", 1), fx.ingest)
	if badAt.Status != 400 || fx.errOf(t, badAt) != "at không phải thời điểm ISO-8601: hôm qua" {
		t.Fatal(badAt.Status, badAt.Body)
	}
	findings := fx.w.PostBearer("/api/cli/import", strings.Replace(f.importBody("BRD"), `"findings":[]`,
		`"findings":[{"code":"C","message":"m"}]`, 1), fx.ingest)
	if findings.Status != 409 || fx.errOf(t, findings) != "Bản nộp còn phát hiện" {
		t.Fatal(findings.Status, findings.Body)
	}
	if r := fx.w.PostBearer("/api/cli/import", f.importBody("BRD"), fx.approveOwner); r.Status != 403 {
		t.Fatal(r.Status, r.Body)
	}
	if n := fx.count(t, "SELECT count(*) FROM approvals WHERE repo_id = 'r/im-2'"); n != 0 {
		t.Fatal(n)
	}
	// repo local: không nhập được
	fx.exec(t, "UPDATE repo_settings SET approvals = 'local' WHERE repo_id = 'r/im-2'")
	local := fx.w.PostBearer("/api/cli/import", f.importBody("BRD"), fx.ingest)
	if local.Status != 409 || fx.errOf(t, local) != "Repo không ở chế độ duyệt trên web" {
		t.Fatal(local.Status, local.Body)
	}
	// thời điểm có múi giờ lệch: quy về UTC
	fx.exec(t, "UPDATE repo_settings SET approvals = 'web' WHERE repo_id = 'r/im-2'")
	off := fx.w.PostBearer("/api/cli/import", strings.Replace(f.importBody("BRD"), "2026-11-01T10:00:00Z", "2026-11-01T17:00:00+07:00", 1), fx.ingest)
	if off.Status != 200 {
		t.Fatal(off.Status, off.Body)
	}
	if got := fx.str(t, "SELECT (original_at = '2026-11-01T10:00:00Z'::timestamptz)::text FROM approvals WHERE repo_id = 'r/im-2'"); got != "true" {
		t.Fatal(got)
	}
}

func TestImportRefusesReviewAndOverlongStrings(t *testing.T) {
	fx := newGfx(t, "im")
	f := fx.feat(t, "r/im-rv", "K-1", "LITE")
	f.openGates("CASES")
	rv := fx.w.PostBearer("/api/cli/import", f.importBody("REVIEW"), fx.ingest)
	if rv.Status != 409 || fx.errOf(t, rv) != "REVIEW phải duyệt lại trên web" {
		t.Fatal(rv.Status, rv.Body)
	}

	g := fx.feat(t, "r/im-long", "K-1", "FULL")
	longApprover := fx.w.PostBearer("/api/cli/import", strings.Replace(g.importBody("BRD"), "local.dev", strings.Repeat("a", 201), 1), fx.ingest)
	if longApprover.Status != 400 || fx.errOf(t, longApprover) != "approver quá dài (tối đa 200 ký tự)" {
		t.Fatal(longApprover.Status, longApprover.Body)
	}
	longAt := fx.w.PostBearer("/api/cli/import", strings.Replace(g.importBody("BRD"), "2026-11-01T10:00:00Z",
		"2026-11-01T10:00:00"+strings.Repeat("0", 200)+"Z", 1), fx.ingest)
	if longAt.Status != 400 || fx.errOf(t, longAt) != "at quá dài (tối đa 200 ký tự)" {
		t.Fatal(longAt.Status, longAt.Body)
	}
	if r := fx.w.PostBearer("/api/cli/import", strings.Replace(g.importBody("BRD"), "local.dev", strings.Repeat("a", 200), 1), fx.ingest); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if n := fx.count(t, "SELECT count(*) FROM approvals WHERE repo_id IN ('r/im-rv','r/im-long') AND channel = 'import'"); n != 1 {
		t.Fatal(n)
	}
}
