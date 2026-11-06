package control_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"aisdlc/internal/fsx"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/web"
	"aisdlc/internal/web/signing"
	"aisdlc/internal/web/webtest"
)

// steps của ControlIT: S1 chờ accept (endTree t1), S2 đang làm.
const stepsJSON = `[{"id":"S1","status":"AWAITING_DEV","record":{"endTree":"t1"}},{"id":"S2","status":"IN_PROGRESS"}]`

// fx là GateFixture.java rút gọn: owner, dev, lead với phiên và token; tính năng gửi qua ingest thật.
type fx struct {
	e                                *webtest.Env
	w                                *webtest.Client
	keys                             *signing.Keys
	owner, dev, lead, ownerEmail     string
	ownerS, devS, leadS              webtest.Session
	ingest, approveOwner, ownerLogin string
	seq                              int
}

func newFx(t *testing.T) *fx {
	t.Helper()
	f := &fx{}
	f.e = webtest.New(t, func(d web.Deps) http.Handler {
		k, err := signing.Load(filepath.Join(t.TempDir(), "signing-key"), "")
		if err != nil {
			t.Fatal(err)
		}
		f.keys = k
		d.Keys = k
		d.Now = func() time.Time { return time.Now().UTC() }
		return web.NewHandler(d)
	})
	f.w = f.e.Client(t)
	f.owner, f.dev, f.lead = "ct.owner", "ct.dev", "ct.lead"
	f.ownerEmail = "ct-owner@corp"
	f.ownerLogin = webtest.CliToken(t, f.e.Pool, "add", f.owner, "Owner", "--email", f.ownerEmail)
	devT := webtest.CliToken(t, f.e.Pool, "add", f.dev, "Dev", "--email", "ct-dev@corp")
	leadT := webtest.CliToken(t, f.e.Pool, "add", f.lead, "Lead", "--role", "lead")
	f.ownerS, f.devS, f.leadS = f.w.Session(f.ownerLogin), f.w.Session(devT), f.w.Session(leadT)
	_, f.ingest = f.w.CreateToken(f.ownerS, "ingest", "cli")
	_, f.approveOwner = f.w.CreateToken(f.ownerS, "approve", "term")
	return f
}

type feat struct{ repo, key string }

// feat đẩy một snapshot FULL với steps của ControlIT; owner = email của owner.
func (f *fx) feat(t *testing.T, repo string) feat {
	t.Helper()
	f.seq++
	s := map[string]any{"schemaVersion": 1, "repo": map[string]any{"id": repo, "branch": "b"}, "clientId": "A",
		"capturedAt": time.Now().UTC().Add(time.Duration(f.seq) * time.Second).Format(time.RFC3339Nano), "sender": "x",
		"feature": map[string]any{"key": "K-1", "title": "t", "flow": "FULL", "owner": f.ownerEmail},
		"gates":   []any{}, "steps": json.RawMessage(stepsJSON), "cases": []any{}, "openQuestions": []any{},
		"artifacts": map[string]any{}, "eval": nil, "review": nil, "truncated": false}
	b, _ := json.Marshal(s)
	r := f.w.PostBearer("/api/ingest/snapshot", string(b), f.ingest)
	if r.Status != 200 || r.JSON(t)["accepted"] != true {
		t.Fatal("snapshot", r.Status, r.Body)
	}
	return feat{repo, "K-1"}
}

func (f *fx) post(s webtest.Session, what, body string) webtest.Resp {
	return f.w.PostSession("/api/control/"+what, body, s, s.CSRF)
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func (f *fx) pause(s webtest.Session, ft feat, paused bool) webtest.Resp {
	return f.post(s, "pause", fmt.Sprintf(`{"repo":%s,"feature":%s,"paused":%v}`, js(ft.repo), js(ft.key), paused))
}

func (f *fx) instruct(s webtest.Session, ft feat, text string) webtest.Resp {
	return f.post(s, "instructions", fmt.Sprintf(`{"repo":%s,"feature":%s,"text":%s}`, js(ft.repo), js(ft.key), js(text)))
}

// act: ACCEPT_STEP gửi kèm endTree trang đang hiện ("t1" của S1).
func (f *fx) act(s webtest.Session, ft feat, kind, step string, path *string) webtest.Resp {
	var end *string
	if kind == "ACCEPT_STEP" {
		end = sp("t1")
	}
	return f.actEnd(s, ft, kind, step, path, end)
}

func (f *fx) actEnd(s webtest.Session, ft feat, kind, step string, path, endTree *string) webtest.Resp {
	extra := ""
	if path != nil {
		extra += `,"path":` + js(*path)
	}
	if endTree != nil {
		extra += `,"endTree":` + js(*endTree)
	}
	return f.post(s, "actions", fmt.Sprintf(`{"repo":%s,"feature":%s,"kind":%s,"step":%s%s}`,
		js(ft.repo), js(ft.key), js(kind), js(step), extra))
}

func sp(s string) *string { return &s }

func (f *fx) view(t *testing.T, ft feat) map[string]any {
	t.Helper()
	r := f.w.GetSession("/api/features/"+ft.repo+"/"+ft.key+"/control", f.ownerS)
	if r.Status != 200 {
		t.Fatal("view", r.Status, r.Body)
	}
	return r.JSON(t)
}

func list(v any) []any { l, _ := v.([]any); return l }

func (f *fx) errOf(t *testing.T, r webtest.Resp) string {
	t.Helper()
	s, _ := r.JSON(t)["error"].(string)
	return s
}

func (f *fx) signedRaw(t *testing.T, repo, key string) string {
	t.Helper()
	r := f.w.GetBearer("/api/cli/control?repo="+repo+"&feature="+key, f.ingest)
	if r.Status != 200 {
		t.Fatal("control", r.Status, r.Body)
	}
	return r.Body
}

// controlOf đọc như CLI: xác minh bằng khóa công khai từ /api/cli/pubkey rồi FeatureControlFromJSON.
func (f *fx) controlOf(t *testing.T, repo, key string) snapshot.FeatureControl {
	t.Helper()
	pk := f.w.GetBearer("/api/cli/pubkey", f.ingest)
	pub, err := snapshot.PublicKeyFromBase64(pk.JSON(t)["publicKey"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var se snapshot.SignedEnvelope
	if err := json.Unmarshal([]byte(f.signedRaw(t, repo, key)), &se); err != nil {
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
	return c
}

func (f *fx) control(t *testing.T, ft feat) snapshot.FeatureControl {
	return f.controlOf(t, ft.repo, ft.key)
}

func (f *fx) events(repo, version, sessionID, typ string, detail any) int {
	v := ""
	if version != "" {
		v = `"version":` + version + `,`
	}
	sid := ""
	if sessionID != "" {
		sid = `,"sessionId":"` + sessionID + `"`
	}
	d := "null"
	if detail != nil {
		d = js(detail)
	}
	return f.w.PostBearer("/api/ingest/events", fmt.Sprintf(`{%s"repoId":"%s","events":[{"type":"%s","at":"2026-11-04T10:00:00Z","feature":"K-1","step":"S1","detail":%s%s}]}`,
		v, repo, typ, d, sid), f.ingest).Status
}

func (f *fx) strs(t *testing.T, sql string, args ...any) []string {
	t.Helper()
	rows, err := f.e.Pool.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func (f *fx) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.e.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func eq[T comparable](t *testing.T, got, want T, what ...any) {
	t.Helper()
	if got != want {
		t.Fatalf("%v: got %v, want %v", what, got, want)
	}
}

func TestPauseResume(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-pause")
	r := f.pause(f.ownerS, ft, true)
	eq(t, r.Status, 200, r.Body)
	eq(t, r.JSON(t)["paused"], any(true))
	eq(t, f.view(t, ft)["pausedBy"], any(f.owner))
	c := f.control(t, ft)
	if c.Pause == nil {
		t.Fatal("pause nil")
	}
	eq(t, c.Pause.By, f.owner)
	eq(t, c.Repo, ft.repo)

	// bật lại khi đã bật: giữ người/giờ cũ
	at := f.view(t, ft)["pausedAt"]
	eq(t, f.pause(f.leadS, ft, true).Status, 200)
	eq(t, f.view(t, ft)["pausedBy"], any(f.owner))
	eq(t, f.view(t, ft)["pausedAt"], at)

	eq(t, f.pause(f.ownerS, ft, false).Status, 200)
	eq(t, f.view(t, ft)["paused"], any(false))
	if f.control(t, ft).Pause != nil {
		t.Fatal("còn pause")
	}
	got := strings.Join(f.strs(t, "SELECT action FROM audit WHERE repo_id = $1 AND action LIKE 'control.%' ORDER BY id", ft.repo), ",")
	eq(t, got, "control.pause,control.pause,control.resume")
}

func TestNonOwnerForbidden(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-perm")
	dev := f.pause(f.devS, ft, true)
	eq(t, dev.Status, 403)
	eq(t, f.errOf(t, dev), "Chỉ chủ tính năng hoặc lead được điều khiển agent")
	eq(t, f.instruct(f.devS, ft, "x").Status, 403)
	eq(t, f.act(f.devS, ft, "ACCEPT_STEP", "S1", nil).Status, 403)
	if f.control(t, ft).Pause != nil {
		t.Fatal("dev tạm dừng được")
	}
	eq(t, f.pause(f.leadS, ft, true).Status, 200)
}

func TestBearerForbidden(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-bearer")
	body := fmt.Sprintf(`{"repo":"%s","feature":"%s","paused":true}`, ft.repo, ft.key)
	for _, tok := range []string{f.approveOwner, f.ingest, f.ownerLogin} {
		eq(t, f.w.PostBearer("/api/control/pause", body, tok).Status, 403)
	}
	eq(t, f.w.PostSession("/api/control/pause", body, f.ownerS, "").Status, 403)
	eq(t, f.w.PostSession("/api/control/pause", body, f.ownerS, "sai").Status, 403)
	if f.control(t, ft).Pause != nil {
		t.Fatal("tạm dừng qua đường bị cấm")
	}
}

func TestInstructionLimits(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-instr")
	eq(t, f.instruct(f.ownerS, ft, strings.Repeat("a", 2000)).Status, 200)
	big := f.instruct(f.ownerS, ft, strings.Repeat("a", 2001))
	eq(t, big.Status, 400)
	eq(t, f.errOf(t, big), "Chỉ dẫn phải từ 1 đến 2000 ký tự")
	eq(t, f.instruct(f.ownerS, ft, "   ").Status, 400)
	eq(t, f.instruct(f.ownerS, ft, "  bí mật  ").Status, 200)
	i0 := list(f.view(t, ft)["instructions"])[0].(map[string]any)
	eq(t, i0["text"], any("bí mật"))
	eq(t, i0["source"], any("web"))
	audits := f.strs(t, "SELECT detail::text FROM audit WHERE repo_id = $1 AND action = 'control.instruction'", ft.repo)
	eq(t, len(audits), 2)
	for _, a := range audits {
		if strings.Contains(a, "bí mật") || strings.Contains(a, "aaaa") || !strings.Contains(a, "length") {
			t.Fatalf("audit chép nội dung hoặc thiếu length: %s", a)
		}
	}
}

func TestAcceptAction(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-accept")
	r := f.act(f.ownerS, ft, "ACCEPT_STEP", "S1", nil)
	eq(t, r.Status, 200, r.Body)
	a := list(r.JSON(t)["actions"])[0].(map[string]any)
	eq(t, a["kind"], any("ACCEPT_STEP"))
	eq(t, a["status"], any("PENDING"))
	id := a["id"].(float64)
	ca := f.control(t, ft).Actions[0]
	eq(t, ca.EndTree, "t1")
	eq(t, float64(ca.ID), id)
	eq(t, ca.ExpiresAt.Sub(ca.At), 7*24*time.Hour)

	s2 := f.act(f.ownerS, ft, "ACCEPT_STEP", "S2", nil)
	eq(t, s2.Status, 409)
	if !strings.Contains(f.errOf(t, s2), "AWAITING_DEV") {
		t.Fatal(s2.Body)
	}
	eq(t, f.act(f.ownerS, ft, "NOPE", "S1", nil).Status, 400)

	again := list(f.act(f.ownerS, ft, "ACCEPT_STEP", "S1", nil).JSON(t)["actions"])
	eq(t, len(again), 1)
	eq(t, again[0].(map[string]any)["id"].(float64), id)
}

func TestAcceptBoundToSeenEndTree(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-seen")
	stale := f.actEnd(f.ownerS, ft, "ACCEPT_STEP", "S1", nil, sp("t0"))
	eq(t, stale.Status, 409, stale.Body)
	eq(t, f.errOf(t, stale), "Bước đã đổi kể từ lúc bạn xem; tải lại trang")
	missing := f.actEnd(f.ownerS, ft, "ACCEPT_STEP", "S1", nil, nil)
	eq(t, missing.Status, 400)
	if !strings.Contains(f.errOf(t, missing), "endTree") {
		t.Fatal(missing.Body)
	}
	eq(t, len(list(f.view(t, ft)["actions"])), 0)
	ok := f.actEnd(f.ownerS, ft, "ACCEPT_STEP", "S1", nil, sp("t1"))
	eq(t, ok.Status, 200, ok.Body)
	eq(t, f.control(t, ft).Actions[0].EndTree, "t1")
}

func TestAllowAction(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-allow")
	ok := f.act(f.ownerS, ft, "ALLOW_SCOPE", "S2", sp("src/a/B.java"))
	eq(t, ok.Status, 200, ok.Body)
	eq(t, f.act(f.ownerS, ft, "ALLOW_SCOPE", "S1", sp("src/a/B.java")).Status, 200)
	eq(t, f.act(f.ownerS, ft, "ALLOW_SCOPE", "S9", sp("src/a/B.java")).Status, 409)
	for _, bad := range []string{"../x", "/x", `a\b`, ".aisdlc/x", strings.Repeat("a", 501), "a/../b", "", "**", ".aisdl?/**",
		"{.aisdlc,src}/**", "./.aisdlc/x", "C:/x", "a//b", "a/./b"} {
		eq(t, f.act(f.ownerS, ft, "ALLOW_SCOPE", "S2", sp(bad)).Status, 400, bad)
	}
	eq(t, f.act(f.ownerS, ft, "ALLOW_SCOPE", "S2", nil).Status, 400)
	a := f.control(t, ft).Actions[0]
	eq(t, a.Path, "src/a/B.java")
	eq(t, a.EndTree, "")
}

func TestCliEnvelope(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-env")
	f.instruct(f.ownerS, ft, "một")
	f.act(f.ownerS, ft, "ACCEPT_STEP", "S1", nil)
	f.act(f.ownerS, ft, "ALLOW_SCOPE", "S2", sp("src/x.txt"))
	c := f.control(t, ft)
	eq(t, len(c.Instructions), 1)
	eq(t, c.Instructions[0].Text, "một")
	eq(t, len(c.Actions), 2)
	if d := time.Since(c.IssuedAt); d < -time.Minute || d > time.Minute {
		t.Fatal("issuedAt lệch", c.IssuedAt)
	}

	if _, err := f.e.Pool.Exec(context.Background(),
		"UPDATE pending_actions SET expires_at = now() - interval '1 minute' WHERE repo_id = $1 AND kind = 'ALLOW_SCOPE'", ft.repo); err != nil {
		t.Fatal(err)
	}
	c = f.control(t, ft)
	eq(t, len(c.Actions), 1)
	eq(t, c.Actions[0].Kind, "ACCEPT_STEP")
	var kinds []string
	for _, a := range list(f.view(t, ft)["actions"]) {
		m := a.(map[string]any)
		kinds = append(kinds, m["kind"].(string)+":"+m["status"].(string))
	}
	sort.Strings(kinds)
	eq(t, strings.Join(kinds, ","), "ACCEPT_STEP:PENDING,ALLOW_SCOPE:EXPIRED")

	// tính năng không tồn tại: phong bì rỗng, không 404
	e := f.controlOf(t, "r/none", "Z-9")
	if e.Pause != nil || len(e.Instructions) != 0 || len(e.Actions) != 0 {
		t.Fatalf("không rỗng: %+v", e)
	}
	// phiên không gọi được /api/cli
	eq(t, f.w.GetSession("/api/cli/control?repo=r/none&feature=Z-9", f.ownerS).Status, 403)
	// thiếu tham số
	eq(t, f.w.GetBearer("/api/cli/control?feature=Z-9", f.ingest).Status, 400)
	eq(t, f.w.GetBearer("/api/cli/control?repo=r/x", f.ingest).Status, 400)
}

func TestDeliveredEventMarksInstruction(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-deliv")
	other := f.feat(t, "r/ct-deliv2")
	f.instruct(f.ownerS, ft, "một")
	f.instruct(f.ownerS, other, "khác")
	id := f.control(t, ft).Instructions[0].ID
	otherID := f.control(t, other).Instructions[0].ID

	// id của tính năng khác: không đổi gì
	eq(t, f.events(ft.repo, "2", "", "INSTRUCTION_DELIVERED", fmt.Sprint(otherID)), 200)
	eq(t, list(f.view(t, ft)["instructions"])[0].(map[string]any)["deliveredAt"], nil)
	eq(t, list(f.view(t, other)["instructions"])[0].(map[string]any)["deliveredAt"], nil)
	// detail hỏng: event vẫn lưu
	eq(t, f.events(ft.repo, "2", "", "INSTRUCTION_DELIVERED", "abc"), 200)

	eq(t, f.events(ft.repo, "2", "", "INSTRUCTION_DELIVERED", fmt.Sprint(id)), 200)
	d, _ := list(f.view(t, ft)["instructions"])[0].(map[string]any)["deliveredAt"].(string)
	if !strings.HasPrefix(d, "2026-11-04T10:00:00") {
		t.Fatal("deliveredAt", d)
	}
	eq(t, len(f.control(t, ft).Instructions), 0)
	eq(t, len(f.control(t, other).Instructions), 1)
	eq(t, f.count(t, "SELECT count(*) FROM events WHERE repo_id = $1 AND type = 'INSTRUCTION_DELIVERED'", ft.repo), 3)
}

func TestActionResultEvent(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-result")
	f.act(f.ownerS, ft, "ACCEPT_STEP", "S1", nil)
	id := f.control(t, ft).Actions[0].ID
	res := func(status string, reason any) int {
		return f.events(ft.repo, "2", "", "ACTION_RESULT", js(map[string]any{"id": id, "status": status, "reason": reason}))
	}
	first := func() map[string]any { return list(f.view(t, ft)["actions"])[0].(map[string]any) }
	eq(t, res("STALE", "r"), 200)
	a := first()
	eq(t, a["status"], any("STALE"))
	eq(t, a["reason"], any("r"))
	if a["resolvedAt"] == nil {
		t.Fatal("resolvedAt nil")
	}
	// clone/worktree khác báo STALE trước; máy thật báo APPLIED sau: APPLIED thắng
	eq(t, res("APPLIED", nil), 200)
	eq(t, first()["status"], any("APPLIED"))
	eq(t, first()["reason"], nil)
	// STALE không bao giờ ghi đè APPLIED
	eq(t, res("STALE", "r2"), 200)
	eq(t, first()["status"], any("APPLIED"))
	eq(t, len(f.control(t, ft).Actions), 0)
	eq(t, f.events(ft.repo, "2", "", "SCOPE_DENIED", nil), 200)
}

func TestEventV2Session(t *testing.T) {
	f := newFx(t)
	repo := "r/ct-v2"
	f.feat(t, repo)
	eq(t, f.events(repo, "2", "sess-1", "COMMAND", "x"), 200)
	var sid string
	if err := f.e.Pool.QueryRow(context.Background(), "SELECT session_id FROM events WHERE repo_id = $1", repo).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	eq(t, sid, "sess-1")
	eq(t, f.events(repo, "", "", "COMMAND", "y"), 200)
	eq(t, f.events(repo, "1", "", "COMMAND", "y"), 200)
	eq(t, f.events(repo, "3", "", "COMMAND", "y"), 400)
	eq(t, f.events(repo, "2", strings.Repeat("s", 101), "COMMAND", "y"), 400)
	eq(t, f.events(repo, "2", strings.Repeat("s", 100), "COMMAND", "y"), 200)
	eq(t, f.count(t, "SELECT count(*) FROM events WHERE repo_id = $1", repo), 4)
}

func TestModeEnvelopeHasIssuedAt(t *testing.T) {
	f := newFx(t)
	r := f.w.PostBearer("/api/cli/mode", `{"repo":"r/ct-mode","configured":"local"}`, f.ingest)
	eq(t, r.Status, 200, r.Body)
	var se snapshot.SignedEnvelope
	if err := json.Unmarshal([]byte(r.Body), &se); err != nil {
		t.Fatal(err)
	}
	payload, ok := se.Verify(f.keys.Public())
	if !ok {
		t.Fatal("chữ ký sai")
	}
	var p struct{ IssuedAt time.Time }
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(p.IssuedAt); d < -time.Minute || d > time.Minute {
		t.Fatal("issuedAt lệch", p.IssuedAt)
	}
}

// TestControlEnvelopeVerifiesInCLI: payload đọc được bằng snapshot.FeatureControlFromJSON, chữ ký hợp lệ với khóa
// công khai của server, và sửa payload thì chữ ký sai.
func TestControlEnvelopeVerifiesInCLI(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-cli")
	f.pause(f.ownerS, ft, true)
	f.instruct(f.ownerS, ft, "hãy dừng")
	f.act(f.ownerS, ft, "ALLOW_SCOPE", "S2", sp("src/x.txt"))
	var se snapshot.SignedEnvelope
	if err := json.Unmarshal([]byte(f.signedRaw(t, ft.repo, ft.key)), &se); err != nil {
		t.Fatal(err)
	}
	payload, ok := se.Verify(f.keys.Public())
	if !ok {
		t.Fatal("chữ ký sai")
	}
	c, err := snapshot.FeatureControlFromJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	if c.Pause == nil || c.Pause.By != f.owner || len(c.Instructions) != 1 || len(c.Actions) != 1 ||
		c.Actions[0].Path != "src/x.txt" || c.Actions[0].ExpiresAt == nil {
		t.Fatalf("payload: %+v", c)
	}
	first := "A"
	if strings.HasPrefix(se.Payload, "A") {
		first = "B"
	}
	se.Payload = first + se.Payload[1:]
	if _, ok := se.Verify(f.keys.Public()); ok {
		t.Fatal("sửa payload mà chữ ký vẫn đúng")
	}
}

// TestConcurrentWrites: các ghi đồng thời của cùng tính năng tuần tự theo khóa, không deadlock, lệnh trùng chỉ một.
func TestConcurrentWrites(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-conc")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				f.pause(f.ownerS, ft, i%2 == 0)
			case 1:
				f.instruct(f.ownerS, ft, fmt.Sprint("c", i))
			default:
				f.act(f.ownerS, ft, "ALLOW_SCOPE", "S2", sp("src/a.txt"))
			}
		}(i)
	}
	wg.Wait()
	eq(t, f.count(t, "SELECT count(*) FROM instructions WHERE repo_id = $1", ft.repo), 4)
	eq(t, f.count(t, "SELECT count(*) FROM pending_actions WHERE repo_id = $1", ft.repo), 1)
}

// Phía tiêu thụ: ghi phong bì của server ra file bộ nhớ đệm như CLI rồi đọc bằng snapshot.ReadControlCache với khóa ghim.
func TestControlEnvelopeConsumedByCliCache(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-cache")
	f.instruct(f.ownerS, ft, "một")
	raw := f.signedRaw(t, ft.repo, ft.key)
	pk := f.w.GetBearer("/api/cli/pubkey", f.ingest)
	pub, err := snapshot.PublicKeyFromBase64(pk.JSON(t)["publicKey"].(string))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	file := snapshot.ControlCacheFile(root, model.FeatureKey(ft.key))
	if err := fsx.WriteAtomic(file, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	c, ok := snapshot.ReadControlCache(file, pub, ft.repo, model.FeatureKey(ft.key))
	if !ok || len(c.Instructions) != 1 || c.Instructions[0].Text != "một" {
		t.Fatalf("%v %+v", ok, c)
	}
	if _, ok := snapshot.ReadControlCache(file, pub, ft.repo, model.FeatureKey("K-2")); ok {
		t.Fatal("sai tính năng phải bị từ chối")
	}
	if _, ok := snapshot.ReadControlCache(file, pub, "r/khac", model.FeatureKey(ft.key)); ok {
		t.Fatal("sai repo phải bị từ chối")
	}
}
