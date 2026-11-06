package gates_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/web"
	"aisdlc/internal/web/signing"
	"aisdlc/internal/web/webtest"
)

// gfx là GateFixture.java: ba người (owner, dev, lead) với phiên + token, tính năng ở repo chế độ web, nộp/duyệt qua
// API thật. Đồng hồ server theo giờ thật và tăng ngặt (thứ tự nộp/feedback trong DB như Java dùng now()).
type gfx struct {
	e                                *webtest.Env
	w                                *webtest.Client
	keys                             *signing.Keys
	owner, dev, lead, ownerEmail     string
	ownerS, devS, leadS              webtest.Session
	ingest, approveOwner, approveDev string
	seq                              int
	onChanged                        func(repo, feature string) // quan sát Deps.Changed; đặt sau newGfx, trước khi gọi API
}

// tickingClock: giờ thật, cắt micro giây, luôn tăng ngặt giữa các lần gọi.
func tickingClock() func() time.Time {
	var mu sync.Mutex
	var last time.Time
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t := time.Now().UTC().Truncate(time.Microsecond)
		if !t.After(last) {
			t = last.Add(time.Microsecond)
		}
		last = t
		return t
	}
}

func newGfx(t *testing.T, prefix string) *gfx {
	t.Helper()
	f := &gfx{}
	f.e = webtest.New(t, func(d web.Deps) http.Handler {
		k, err := signing.Load(filepath.Join(t.TempDir(), "signing-key"), "")
		if err != nil {
			t.Fatal(err)
		}
		f.keys = k
		d.Keys = k
		d.Now = tickingClock()
		d.Changed = func(r, k string) {
			if f.onChanged != nil {
				f.onChanged(r, k)
			}
		}
		return web.NewHandler(d)
	})
	f.w = f.e.Client(t)
	f.owner, f.dev, f.lead = prefix+".owner", prefix+".dev", prefix+".lead"
	f.ownerEmail = prefix + "-owner@corp"
	ownerT := webtest.CliToken(t, f.e.Pool, "add", f.owner, "Owner", "--email", f.ownerEmail)
	devT := webtest.CliToken(t, f.e.Pool, "add", f.dev, "Dev", "--email", prefix+"-dev@corp")
	leadT := webtest.CliToken(t, f.e.Pool, "add", f.lead, "Lead", "--role", "lead")
	f.ownerS, f.devS, f.leadS = f.w.Session(ownerT), f.w.Session(devT), f.w.Session(leadT)
	_, f.ingest = f.w.CreateToken(f.ownerS, "ingest", "cli")
	_, f.approveOwner = f.w.CreateToken(f.ownerS, "approve", "term")
	_, f.approveDev = f.w.CreateToken(f.devS, "approve", "term")
	return f
}

func gpath(gate string) string { return "aisdlc/features/K/" + strings.ToLower(gate) + ".md" }

func ghash(gate, text string) string {
	return model.SHA256Contents([]model.Content{{Path: gpath(gate), Data: []byte(text)}})
}

// feat là một tính năng: nội dung từng cổng (hash = sha256 nội dung), eval, tree, steps.
type feat struct {
	f                   *gfx
	t                   *testing.T
	repo, key, flow     string
	gates               []string
	text                map[string]string
	evalJSON, stepsJSON string
	tree                string // "" = không gửi
	cardsJSON           string // reviewCards; "" = không gửi
}

func (f *gfx) feat(t *testing.T, repo, key, flow string) *feat {
	t.Helper()
	ft := &feat{f: f, t: t, repo: repo, key: key, flow: flow, text: map[string]string{}, evalJSON: "null", stepsJSON: "[]"}
	ft.gates = []string{"BRD", "DESIGN", "CASES", "REVIEW"}
	if flow != "FULL" {
		ft.gates = []string{"CASES", "REVIEW"}
	}
	for _, g := range ft.gates {
		ft.text[g] = g + " v1"
	}
	if r := f.w.PostBearer("/api/cli/mode", `{"repo":"`+repo+`","configured":"web"}`, f.ingest); r.Status != 200 {
		t.Fatal("mode", r.Status, r.Body)
	}
	ft.push()
	return ft
}

func (ft *feat) hash(gate string) string { return ghash(gate, ft.text[gate]) }

// push gửi snapshot mới nhất (owner = email của owner).
func (ft *feat) push() {
	t := ft.t
	t.Helper()
	ft.f.seq++
	var gates []map[string]any
	for _, g := range ft.gates {
		gates = append(gates, map[string]any{"gate": g, "state": "NOT_SUBMITTED", "currentHash": ghash(g, ft.text[g])})
	}
	s := map[string]any{"schemaVersion": 1, "repo": map[string]any{"id": ft.repo, "branch": "b"}, "clientId": "A",
		"capturedAt": time.Now().UTC().Add(time.Duration(ft.f.seq) * time.Second).Format(time.RFC3339Nano), "sender": "x",
		"feature": map[string]any{"key": ft.key, "title": "t", "flow": ft.flow, "owner": ft.f.ownerEmail},
		"gates":   gates, "steps": json.RawMessage(ft.stepsJSON), "cases": []any{}, "openQuestions": []any{},
		"artifacts": map[string]any{}, "eval": json.RawMessage(ft.evalJSON), "review": nil, "truncated": false}
	if ft.tree != "" {
		s["tree"] = ft.tree
	}
	if ft.cardsJSON != "" {
		s["reviewCards"] = json.RawMessage(ft.cardsJSON)
	}
	b, _ := json.Marshal(s)
	r := ft.f.w.PostBearer("/api/ingest/snapshot", string(b), ft.f.ingest)
	if r.Status != 200 || r.JSON(t)["accepted"] != true {
		t.Fatal("snapshot", r.Status, r.Body)
	}
}

func (ft *feat) submissionBody(gate, findingsJSON string) string {
	return fmt.Sprintf(`{"repo":"%s","feature":"%s","gate":"%s","hash":"%s","findings":%s,
	 "artifacts":[{"path":"%s","content":"%s"}]}`, ft.repo, ft.key, gate, ft.hash(gate), findingsJSON, gpath(gate),
		base64.StdEncoding.EncodeToString([]byte(ft.text[gate])))
}

func (ft *feat) submit(gate, findingsJSON string) webtest.Resp {
	return ft.f.w.PostBearer("/api/cli/submissions", ft.submissionBody(gate, findingsJSON), ft.f.ingest)
}

// submitWithRisk nộp sạch kèm risk.level (mức trong risk.yaml).
func (ft *feat) submitWithRisk(gate, level string) webtest.Resp {
	b := ft.submissionBody(gate, "[]")
	b = b[:strings.LastIndex(b, "}")] + `,"risk":{"level":"` + level + `"}}`
	return ft.f.w.PostBearer("/api/cli/submissions", b, ft.f.ingest)
}

// importPolicy: chính sách đầu tiên của repo (phiên bản 1), như POST /api/cli/policy/import (Task 9) lưu.
func (ft *feat) importPolicy(yaml string) {
	ft.t.Helper()
	ft.f.exec(ft.t, "INSERT INTO risk_policy_versions(repo_id, version, body, hash, author, reason, source) "+
		"VALUES ($1, 1, $2, 'sha256:x', $3, 'import', 'import')", ft.repo, yaml, ft.f.owner)
}

// riskEvent gửi một sự kiện rủi ro của tính năng (Bearer ingest).
func (ft *feat) riskEvent(typ, detailJSON string) {
	ft.t.Helper()
	b, _ := json.Marshal(map[string]any{"repoId": ft.repo, "events": []any{map[string]any{"type": typ,
		"at": time.Now().UTC().Format(time.RFC3339Nano), "feature": ft.key, "detail": detailJSON}}})
	if r := ft.f.w.PostBearer("/api/ingest/events", string(b), ft.f.ingest); r.Status != 200 {
		ft.t.Fatal("events", r.Status, r.Body)
	}
}

func (ft *feat) submitOk(gate string) {
	ft.t.Helper()
	if r := ft.submit(gate, "[]"); r.Status != 200 {
		ft.t.Fatal("submit", gate, r.Status, r.Body)
	}
}

func (ft *feat) approveBody(gate, hash string) string {
	return fmt.Sprintf(`{"repo":"%s","feature":"%s","gate":"%s","hash":"%s","flags":[],"assumptionsSeen":[]}`, ft.repo, ft.key, gate, hash)
}

func (ft *feat) approve(s webtest.Session, gate string) webtest.Resp {
	return ft.f.w.PostSession("/api/gates/approve", ft.approveBody(gate, ft.hash(gate)), s, s.CSRF)
}

func (ft *feat) approveOk(gate string) {
	ft.t.Helper()
	if r := ft.approve(ft.f.ownerS, gate); r.Status != 200 {
		ft.t.Fatal("approve", gate, r.Status, r.Body)
	}
}

// openGates nộp rồi duyệt lần lượt các cổng.
func (ft *feat) openGates(gates ...string) {
	ft.t.Helper()
	for _, g := range gates {
		ft.submitOk(g)
		ft.approveOk(g)
	}
}

// action: POST /api/gates/<action>; comment nil = không gửi.
func (ft *feat) action(action string, s webtest.Session, gate string, comment *string) webtest.Resp {
	c := ""
	if comment != nil {
		b, _ := json.Marshal(*comment)
		c = `,"comment":` + string(b)
	}
	return ft.f.w.PostSession("/api/gates/"+action, fmt.Sprintf(`{"repo":"%s","feature":"%s","gate":"%s"%s}`, ft.repo, ft.key, gate, c), s, s.CSRF)
}

func str(s string) *string { return &s }

func (f *gfx) errOf(t *testing.T, r webtest.Resp) string {
	t.Helper()
	s, _ := r.JSON(t)["error"].(string)
	return s
}

// verified xác minh phong bì bằng khóa công khai lấy từ /api/cli/pubkey và đọc trạng thái như CLI.
func (f *gfx) verified(t *testing.T, body string) snapshot.FeatureGateState {
	t.Helper()
	pk := f.w.GetBearer("/api/cli/pubkey", f.ingest)
	pub, err := snapshot.PublicKeyFromBase64(pk.JSON(t)["publicKey"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var env snapshot.SignedEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err, body)
	}
	payload, ok := env.Verify(pub)
	if !ok {
		t.Fatal("chữ ký sai")
	}
	st, err := snapshot.FeatureGateStateFromJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func (f *gfx) envelopeRaw(t *testing.T, ft *feat) string {
	t.Helper()
	r := f.w.GetBearer("/api/cli/gates?repo="+ft.repo+"&feature="+ft.key, f.ingest)
	if r.Status != 200 {
		t.Fatal("gates", r.Status, r.Body)
	}
	return r.Body
}

func (f *gfx) envelope(t *testing.T, ft *feat) snapshot.FeatureGateState {
	t.Helper()
	return f.verified(t, f.envelopeRaw(t, ft))
}

func (f *gfx) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.e.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *gfx) str(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := f.e.Pool.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		t.Fatal(sql, err)
	}
	return s
}

func (f *gfx) strs(t *testing.T, sql string, args ...any) []string {
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

func (f *gfx) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.e.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func apprKeys(as []snapshot.Appr, fn func(snapshot.Appr) string) string {
	var out []string
	for _, a := range as {
		out = append(out, fn(a))
	}
	return strings.Join(out, ",")
}

// httpResult là phản hồi đọc được trong goroutine (không gọi t.Fatal ngoài goroutine của test).
type httpResult struct {
	status int
	body   string
	err    error
}

func sessionHdr(id, csrf string) map[string]string {
	return map[string]string{"Cookie": "AISDLC_SESSION=" + id, "X-CSRF-Token": csrf, "Content-Type": "application/json"}
}

func bearerHdr(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}
}

// postTo gửi POST tới base (an toàn trong goroutine: lỗi trả về, không gọi t.Fatal).
func postTo(ctx context.Context, base, path, body string, hdr map[string]string) httpResult {
	req, err := http.NewRequestWithContext(ctx, "POST", base+path, strings.NewReader(body))
	if err != nil {
		return httpResult{err: err}
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return httpResult{err: err}
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return httpResult{status: res.StatusCode, body: string(b), err: err}
}

func (f *gfx) post(ctx context.Context, path, body string, hdr map[string]string) httpResult {
	return postTo(ctx, f.e.Server.URL, path, body, hdr)
}
