package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/risk"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
	"aisdlc/internal/webclient/fakeweb"
)

// FlowLocalTest, các test chế độ web: CLI không tự duyệt; server tự duyệt theo mức risk.yaml gửi kèm bản nộp; event
// nâng mức đi sau bản nộp.

// flowWeb như đầu mỗi test webMode_* của FlowLocalTest: repo flowRepo, fakeweb của đúng repo, đã login (khóa ghim),
// chính sách trên web = file trong repo (v1) khi withPolicy, rồi chuyển approvals: web.
func flowWeb(t *testing.T, withPolicy bool) (*testRepo, *fakeweb.Server) {
	r := flowRepo(t)
	repoID := NewWebCache(r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), r.root).RepoID()
	web := fakeweb.New(t)
	web.SetFeature(repoID, "PAY-1")
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "ingest-tok"
	must(t, webclient.Pin(r.env["HOME"], web.Pub))
	if withPolicy {
		yaml := r.read(policyFile)
		web.Update(func(s *fakeweb.Server) { s.PolicyYAML, s.PolicyVersion = yaml, 1 })
	}
	r.write("aisdlc/config.yaml", r.read("aisdlc/config.yaml")+"approvals: web\n")
	return r, web
}

func (r *testRepo) okRun(args ...string) {
	r.t.Helper()
	if code := r.run(args...); code != 0 {
		r.t.Fatalf("%s: exit %d\n%s%s", strings.Join(args, " "), code, r.out, r.err)
	}
}

func noApproveRequest(t *testing.T, web *fakeweb.Server) {
	t.Helper()
	for _, q := range web.Requests() {
		if strings.Contains(q.Path, "approve") {
			t.Fatalf("CLI không được gọi %s", q.Path)
		}
	}
}

// serverApproveBRD: dev đã duyệt BRD trên web (BRD luôn duyệt tay) với bản nộp đầu.
func serverApproveBRD(web *fakeweb.Server) {
	web.Update(func(s *fakeweb.Server) {
		h := s.State.Submissions[0].Hash
		s.State.Approvals = []snapshot.Appr{{Gate: "BRD", Hash: h, Approver: "dev", At: fakeweb.T, Channel: "web"}}
	})
}

func submissionBody(t *testing.T, web *fakeweb.Server, i int) map[string]any {
	t.Helper()
	var m map[string]any
	must(t, web.RequestsFor("POST", "/api/cli/submissions")[i].JSON(&m))
	return m
}

func TestWebModeCliDoesNotAutoApprove(t *testing.T) {
	r, web := flowWeb(t, false)
	r.okRun("submit", "brd")
	contains(t, r.out, "Đã nộp cổng BRD lên web")
	notContains(t, r.out, "tự duyệt")
	eq(t, len(web.RequestsFor("POST", "/api/cli/submissions")), 1)
	noApproveRequest(t, web)
	eq(t, r.exists("aisdlc/features/PAY-1/gates.json"), false)
	eq(t, r.state(gate.BRD), "AWAITING_APPROVAL")
}

func TestWebModeSubmitSendsRiskLevelAndShowsServerAutoApproval(t *testing.T) {
	r, web := flowWeb(t, true)
	r.okRun("submit", "brd")
	// BRD không gửi risk: cổng BRD luôn duyệt tay
	_, has := submissionBody(t, web, 0)["risk"]
	eq(t, has, false)
	serverApproveBRD(web)
	r.ok(noConsole(), "status")
	matches(t, r.out, `BRD +APPROVED`)
	r.okRun("submit", "design")
	d := submissionBody(t, web, 1)
	eq(t, d["gate"], any("DESIGN"))
	eq(t, d["risk"].(map[string]any)["level"], any("low"))
	noApproveRequest(t, web)
}

func TestWebModeSubmitPrintsServerAutoApprovalInsteadOfAskingDev(t *testing.T) {
	r, web := flowWeb(t, true)
	r.okRun("submit", "brd")
	contains(t, r.out, "aisdlc approve brd") // BRD luôn duyệt tay
	serverApproveBRD(web)
	r.ok(noConsole(), "status")
	web.Update(func(s *fakeweb.Server) { s.AutoLevel = "low" }) // phong bì trả về đã có bản tự duyệt của server
	r.okRun("submit", "design")
	contains(t, r.out, "Đã nộp cổng DESIGN lên web, không có phát hiện: tự duyệt (mức low).")
	notContains(t, r.out, "aisdlc approve design")
	noApproveRequest(t, web)
	eq(t, r.exists("aisdlc/features/PAY-1/gates.json"), false)
}

func TestWebModeSubmitDesignRaiseSubmitsBeforeRiskEvent(t *testing.T) {
	r, web := flowWeb(t, true)
	web.Update(func(s *fakeweb.Server) { s.ServerRules = true })
	r.okRun("submit", "brd")
	serverApproveBRD(web)
	r.ok(noConsole(), "status")
	r.llmLevel = "high"
	r.okRun("submit", "design")
	contains(t, r.out, "Rủi ro: high", "Đã nộp cổng DESIGN lên web")
	// bản nộp tới server trước event nâng mức: server giữ bản nộp, chỉ thay thế bản tự duyệt
	submitAt, riskAt := -1, -1
	for i, q := range web.Requests() {
		if q.Path == "/api/cli/submissions" && strings.Contains(q.Body, `"DESIGN"`) {
			submitAt = i
		}
		if q.Path == "/api/ingest/events" && strings.Contains(q.Body, "RISK_CLASSIFIED") && riskAt < 0 {
			riskAt = i
		}
	}
	eq(t, submitAt >= 0, true)
	eq(t, riskAt > submitAt, true)
	var gates []string
	var apprs int
	web.Update(func(s *fakeweb.Server) {
		for _, x := range s.State.Submissions {
			gates = append(gates, x.Gate)
		}
		apprs = len(s.State.Approvals)
	})
	contains(t, strings.Join(gates, ","), "DESIGN")
	eq(t, apprs, 1) // bản duyệt tay BRD giữ nguyên
	eq(t, r.state(gate.BRD), "APPROVED")
	eq(t, r.state(gate.DESIGN), "AWAITING_APPROVAL")
}

// BRD luôn duyệt tay: bản nộp BRD không gửi mức, kể cả khi đã có risk.yaml.
func TestWebModeBrdSubmissionNeverSendsRiskLevel(t *testing.T) {
	r, web := flowWeb(t, true)
	r.okRun("submit", "brd")
	serverApproveBRD(web)
	r.ok(noConsole(), "status")
	r.llmLevel = "high"
	r.okRun("submit", "design")
	r.okRun("submit", "brd")
	b := submissionBody(t, web, 2)
	eq(t, b["gate"], any("BRD"))
	_, has := b["risk"]
	eq(t, has, false)
}

func TestWebModeCasesRaiseThenHarnessFailsStillSendsRiskEvent(t *testing.T) {
	r, web := flowWeb(t, true)
	web.Update(func(s *fakeweb.Server) { s.AutoLevel = "low" })
	r.okRun("submit", "brd")
	serverApproveBRD(web)
	r.ok(noConsole(), "status")
	r.okRun("submit", "design")
	web.Update(func(s *fakeweb.Server) { s.AutoLevel = "" })
	// chưa có risk.yaml lúc nộp Case: xếp loại từ risk-agent.json (subagent nay ra medium)
	must(t, os.Remove(filepath.Join(r.root, riskFile)))
	r.llmLevel = "medium"
	before := len(web.RequestsFor("POST", "/api/ingest/events"))
	r.runner.err = errors.New("harness chết giữa chừng")
	if r.run("submit", "cases") == 0 {
		t.Fatal("phải lỗi")
	}
	ev := web.RequestsFor("POST", "/api/ingest/events")[before:]
	n := 0
	for _, q := range ev {
		if strings.Contains(q.Body, "RISK_CLASSIFIED") {
			n++
			contains(t, q.Body, "medium")
		}
	}
	eq(t, n, 1)
	// lần sau risk.yaml không đổi: không gửi lại
	r.runner.err = nil
	r.runner.cases = failingCases
	after := len(web.RequestsFor("POST", "/api/ingest/events"))
	r.run("submit", "cases")
	for _, q := range web.RequestsFor("POST", "/api/ingest/events")[after:] {
		notContains(t, q.Body, "RISK_CLASSIFIED")
	}
}

func TestWebModeStatusShowsSupersededGate(t *testing.T) {
	r, web := flowWeb(t, true)
	r.okRun("submit", "brd")
	// server đã thay thế bản tự duyệt BRD khi nâng mức: phong bì mang bản đó trong superseded
	web.Update(func(s *fakeweb.Server) {
		h := s.State.Submissions[0].Hash
		s.State.Approvals = nil
		s.State.Superseded = []snapshot.Appr{{Gate: "BRD", Hash: h, Approver: "auto", At: fakeweb.T, Channel: "auto",
			Rule: "flow:low"}}
	})
	r.ok(noConsole(), "status")
	matches(t, r.out, `BRD +AWAITING_APPROVAL +chờ dev duyệt \(bản tự duyệt \(mức low\) đã bị thay thế khi nâng mức\)`)
	eq(t, r.state(gate.BRD), "AWAITING_APPROVAL")
	eq(t, r.run("step", "start", "S1") != 0, true)
}

func TestWebModeFailedSubmitStillReportsRiskLevel(t *testing.T) {
	r, web := flowWeb(t, true)
	r.okRun("submit", "brd")
	serverApproveBRD(web)
	r.ok(noConsole(), "status")
	web.Update(func(s *fakeweb.Server) {
		s.Override["POST /api/cli/submissions"] = fakeweb.JSONResp(500, `{"error":"hỏng"}`)
	})
	r.llmLevel = "high"
	eq(t, r.run("submit", "design"), 2)
	found := false
	for _, q := range web.RequestsFor("POST", "/api/ingest/events") {
		found = found || strings.Contains(q.Body, "RISK_CLASSIFIED")
	}
	eq(t, found, true)
}

// Chế độ web: `risk set` chạy (không còn dừng như bản 4a), ghi risk.yaml và gửi RISK_OVERRIDDEN lên web để server
// thay thế bản tự duyệt; `risk reclassify` khi cổng Case đã duyệt trên web mà chưa có token approve thì dừng kèm link.
func TestWebModeRiskSetSendsOverrideAndReclassifyNeedsApproveToken(t *testing.T) {
	r, web := flowWeb(t, true)
	r.okRun("submit", "brd")
	serverApproveBRD(web)
	r.okRun("submit", "design")
	eq(t, r.risk().Level, "low")
	if code := r.runC(typed("PAY-1"), "risk", "set", "high"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	eq(t, r.risk().Level, "high")
	found := false
	for _, q := range web.RequestsFor("POST", "/api/ingest/events") {
		found = found || strings.Contains(q.Body, "RISK_OVERRIDDEN")
	}
	eq(t, found, true)
	eq(t, r.exists("aisdlc/features/PAY-1/gates.json"), false)
	// cổng Case đã duyệt trên web
	web.Update(func(s *fakeweb.Server) {
		s.State.Approvals = append(s.State.Approvals, snapshot.Appr{Gate: "CASES", Hash: "h", Approver: "dev",
			At: fakeweb.T, Channel: "web"})
	})
	eq(t, r.runC(typed("PAY-1"), "risk", "reclassify"), 2)
	contains(t, r.err, "Chưa có token approve. Duyệt trên web: "+web.URL()+"/f/", "?g=CASES")
	eq(t, len(web.RequestsFor("POST", "/api/gates/reopen")), 0)
}

// Chế độ web, có token approve: `risk reclassify` khi cổng Case đã duyệt trên web gửi /api/gates/reopen bằng token
// approve, nhận phong bì mới (Case không còn duyệt) và ghi risk.yaml.
func TestWebModeReclassifyReopensCasesWithApproveToken(t *testing.T) {
	r, web := flowWeb(t, true)
	r.okRun("submit", "brd")
	serverApproveBRD(web)
	r.okRun("submit", "design")
	web.Update(func(s *fakeweb.Server) {
		s.State.Approvals = append(s.State.Approvals, snapshot.Appr{Gate: "CASES", Hash: "h", Approver: "dev",
			At: fakeweb.T, Channel: "web"})
	})
	r.ok(noConsole(), "status")
	must(t, webclient.SaveApproveToken(r.env["HOME"], "appr-tok"))
	// phong bì server trả cho reopen: bỏ approval CASES, issuedAt mới hơn
	var payload []byte
	web.Update(func(s *fakeweb.Server) {
		st := s.State
		var keep []snapshot.Appr
		for _, a := range st.Approvals {
			if a.Gate != "CASES" {
				keep = append(keep, a)
			}
		}
		st.Approvals, st.IssuedAt = keep, fakeweb.T.Add(time.Minute)
		var err error
		payload, err = st.ToJSON()
		must(t, err)
	})
	reopened := web.Envelope(payload) // Envelope tự giữ khóa của server: gọi ngoài Update
	web.Update(func(s *fakeweb.Server) {
		s.Override["POST /api/gates/reopen"] = fakeweb.Resp{Status: 200, Body: reopened}
	})
	if code := r.runC(typed("PAY-1"), "risk", "reclassify"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	contains(t, r.out, "Cổng Case đã duyệt: lệnh này sẽ mở lại cổng Case.", "Cổng Case đã mở lại")
	qs := web.RequestsFor("POST", "/api/gates/reopen")
	eq(t, len(qs), 1)
	eq(t, qs[0].Auth, "Bearer appr-tok")
	eq(t, qs[0].Field("gate"), "CASES")
	eq(t, qs[0].Field("channel"), "terminal")
	notContains(t, r.out+r.err, "appr-tok")
	eq(t, r.exists("aisdlc/features/PAY-1/risk.yaml"), true)
}

// Chế độ web, chính sách trên server còn schema 1: dev không tự sửa được, exit 2 chỉ sang quản trị.
func TestWebModeSchema1PolicyAbortsClassification(t *testing.T) {
	r, web := flowWeb(t, true)
	y, err := risk.PresetYAML("generic", "2")
	must(t, err)
	web.Update(func(s *fakeweb.Server) { s.PolicyYAML, s.PolicyVersion = y, 2 })
	r.ok(noConsole(), "policy", "pull")
	eq(t, r.run("risk", "inputs"), 2)
	contains(t, r.err, "Chính sách dùng schema 1 (luật so khớp); quản trị cần chuyển chính sách sang schema 2 trên web.")
	r.okRun("submit", "brd")
	serverApproveBRD(web)
	r.ok(noConsole(), "status")
	eq(t, r.run("submit", "design"), 2)
	contains(t, r.err, "quản trị cần chuyển chính sách sang schema 2 trên web.")
	eq(t, r.exists(riskFile), false)
}

// ---- aisdlc rewind ở chế độ web: server trước, rồi mới xoá cục bộ ----

// rewindWebRepo: repo đã làm xong S1, S2 ở chế độ cục bộ rồi chuyển sang web; server giả giữ BRD/DESIGN/CASES đã duyệt.
func rewindWebRepo(t *testing.T) (*testRepo, *fakeweb.Server) {
	r := rewindRepo(t)
	repoID := NewWebCache(r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), r.root).RepoID()
	web := fakeweb.New(t)
	web.SetFeature(repoID, "PAY-1")
	home := t.TempDir()
	r.env["HOME"] = home
	must(t, webclient.Pin(home, web.Pub))
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "ingest-tok"
	must(t, webclient.SaveConfig(home, webclient.Config{URL: web.URL(), Token: "ingest-tok", ApproveToken: "appr-tok"}))
	r.write("aisdlc/config.yaml", r.read("aisdlc/config.yaml")+"approvals: web\n")
	paths := model.FeaturePaths{RepoRoot: r.root, Key: "PAY-1"}
	var subs []snapshot.Sub
	var apprs []snapshot.Appr
	for _, g := range []gate.Gate{gate.BRD, gate.DESIGN, gate.CASES} {
		c, err := g.ArtifactContents(paths, model.FlowFull)
		must(t, err)
		h := model.SHA256Contents(c)
		subs = append(subs, snapshot.Sub{Gate: string(g), Hash: h, At: fakeweb.T})
		apprs = append(apprs, snapshot.Appr{Gate: string(g), Hash: h, Approver: "dev", At: fakeweb.T, Channel: "web"})
	}
	web.Update(func(s *fakeweb.Server) {
		s.State = snapshot.FeatureGateState{Repo: repoID, Feature: "PAY-1", Mode: "web", IssuedAt: fakeweb.T,
			Submissions: subs, Approvals: apprs}
	})
	r.ok(noConsole(), "status") // nạp cache cổng
	return r, web
}

func TestRewindWeb_ServerFirst(t *testing.T) {
	r, web := rewindWebRepo(t)
	web.Update(func(s *fakeweb.Server) {
		s.Override["POST /api/gates/rewind"] = fakeweb.JSONResp(500, `{"error":"hỏng"}`)
	})
	s2 := "internal/limit/service/limit_service.go"
	stepsBefore := r.read("aisdlc/features/PAY-1/steps.json")
	eq(t, r.runC(typed("PAY-1"), "rewind", "S2"), 2)
	if !r.exists(s2) {
		t.Fatal("server lỗi: working tree không được đổi")
	}
	eq(t, r.read("aisdlc/features/PAY-1/steps.json"), stepsBefore)
	if r.exists("aisdlc/features/PAY-1/rewind-pending.json") {
		t.Fatal("lỗi server ở lần chạy mới: dấu đang dở phải được gỡ")
	}
	ref := strings.TrimSpace(r.backupRefs())
	if !strings.Contains(ref, "refs/aisdlc/backup/PAY-1/") {
		t.Fatal("thiếu ref dự phòng")
	}
	contains(t, r.out+r.err, "refs/aisdlc/backup/PAY-1/")
	eq(t, r.status().Steps["S2"], "ACCEPTED")
	eq(t, len(web.RequestsFor("POST", "/api/gates/rewind")), 1)
}

func TestRewindWeb_ServerErrorOnResumeKeepsMarker(t *testing.T) {
	r, web := rewindWebRepo(t)
	r.write("aisdlc/features/PAY-1/rewind-pending.json", `{"backupRef":"refs/aisdlc/backup/PAY-1/old","by":"x",
"at":"2026-01-01T00:00:00Z","plan":{"Target":{"ID":"S2","Gate":"","Step":"S2"},"ClearGates":["REVIEW"],
"DropSteps":["S2"],"ScopeSteps":["S2"]}}`)
	web.Update(func(s *fakeweb.Server) {
		s.Override["POST /api/gates/rewind"] = fakeweb.JSONResp(500, `{"error":"hỏng"}`)
	})
	eq(t, r.runC(typed("PAY-1"), "rewind", "S2"), 2)
	if !r.exists("aisdlc/features/PAY-1/rewind-pending.json") {
		t.Fatal("chạy tiếp mà server lỗi: phải giữ dấu đang dở")
	}
	eq(t, r.status().Steps["S2"], "ACCEPTED")
}

func TestRewindWeb_Success(t *testing.T) {
	r, web := rewindWebRepo(t)
	if c := r.runC(typed("PAY-1"), "rewind", "cases"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	contains(t, r.out, "Đã quay PAY-1 về đầu cases.")
	reqs := web.RequestsFor("POST", "/api/gates/rewind")
	eq(t, len(reqs), 1)
	var body struct {
		Repo, Feature, Checkpoint, RequestID, Channel string
		Gates                                         []string
	}
	must(t, reqs[0].JSON(&body))
	eq(t, body.Feature, "PAY-1")
	eq(t, body.Checkpoint, "cases")
	eq(t, body.Channel, "terminal")
	eq(t, strings.Join(body.Gates, ","), "CASES,REVIEW")
	if !strings.HasPrefix(body.RequestID, "refs/aisdlc/backup/PAY-1/") || !strings.Contains(r.backupRefs(), body.RequestID) {
		t.Fatalf("requestId phải là tên ref dự phòng: %q", body.RequestID)
	}
	eq(t, reqs[0].Auth, "Bearer appr-tok")

	home := r.env["HOME"]
	wc := NewWebCache(r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), r.root)
	st, ok := gate.ReadWebGateState(wc.CacheFile("PAY-1"), webclient.LoadPinnedKey(home), wc.RepoID(), "PAY-1")
	if !ok || st.Attempt != 2 {
		t.Fatalf("cache cổng phải có attempt=2: %+v ok=%v", st, ok)
	}
	s := r.status()
	eq(t, s.Gates["BRD"], "APPROVED")
	eq(t, s.Gates["DESIGN"], "APPROVED")
	if s.Gates["CASES"] == "APPROVED" {
		t.Fatal("CASES phải được mở lại")
	}
	eq(t, s.Steps["S1"], "NOT_STARTED")
	eq(t, r.exists("aisdlc/features/PAY-1/rewind-pending.json"), false)
	eq(t, r.exists("aisdlc/features/PAY-1/cases.yaml"), false)
}

// Chạy lại sau khi đứt giữa chừng: dùng lại ref dự phòng của dấu đang dở làm requestId (server idempotent).
func TestRewindWeb_ResumeReusesBackupRefAsRequestID(t *testing.T) {
	r, web := rewindWebRepo(t)
	r.write("aisdlc/features/PAY-1/rewind-pending.json", `{"backupRef":"refs/aisdlc/backup/PAY-1/old","by":"x",
"at":"2026-01-01T00:00:00Z","plan":{"Target":{"ID":"S2","Gate":"","Step":"S2"},"ClearGates":["REVIEW"],
"DropSteps":["S2"],"ScopeSteps":["S2"]}}`)
	eq(t, r.runC(typed("PAY-1"), "rewind", "S2"), 0)
	reqs := web.RequestsFor("POST", "/api/gates/rewind")
	eq(t, len(reqs), 1)
	eq(t, reqs[0].Field("requestId"), "refs/aisdlc/backup/PAY-1/old")
	eq(t, r.backupRefs(), "")
}
