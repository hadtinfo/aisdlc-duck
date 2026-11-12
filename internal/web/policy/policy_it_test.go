package policy_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"aisdlc/internal/risk"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/web"
	"aisdlc/internal/web/signing"
	"aisdlc/internal/web/webtest"
)

// Port PolicyControllerIT (14 test) + TestPolicyValidateUsesGoRisk + TestPolicyEnvelopeVerifiesInCLI.

type fixture struct {
	e       *webtest.Env
	w       *webtest.Client
	owner   string
	ownerS  webtest.Session
	ingest  string
	approve string
	seq     int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{owner: "pl.owner"}
	f.e = webtest.New(t, func(d web.Deps) http.Handler {
		k, err := signing.Load(filepath.Join(t.TempDir(), "signing-key"), "")
		if err != nil {
			t.Fatal(err)
		}
		d.Keys = k
		return web.NewHandler(d)
	})
	f.w = f.e.Client(t)
	tok := webtest.CliToken(t, f.e.Pool, "add", f.owner, "Owner", "--email", "pl-owner@corp")
	f.ownerS = f.w.Session(tok)
	_, f.ingest = f.w.CreateToken(f.ownerS, "ingest", "cli")
	_, f.approve = f.w.CreateToken(f.ownerS, "approve", "term")
	return f
}

func (f *fixture) repo() string { f.seq++; return fmt.Sprintf("org/pl-%d", f.seq) }

func enc(s string) string { return url.QueryEscape(s) }

func genericYAML(t *testing.T) string {
	t.Helper()
	y, err := risk.PresetYAML("generic", risk.PresetLatestVersion("generic"))
	if err != nil {
		t.Fatal(err)
	}
	return y
}

func (f *fixture) importYaml(repo, yaml string) webtest.Resp {
	b, _ := json.Marshal(map[string]string{"repo": repo, "yaml": yaml})
	return f.w.PostBearer("/api/cli/policy/import", string(b), f.ingest)
}

func (f *fixture) publish(repo string, base int, policy any, reason string, fromPreset bool) webtest.Resp {
	b, _ := json.Marshal(map[string]any{"repo": repo, "baseVersion": base, "reason": reason, "fromPreset": fromPreset, "policy": policy})
	return f.w.PostSession("/api/policy/publish", string(b), f.ownerS, f.ownerS.CSRF)
}

func (f *fixture) live(t *testing.T, repo string) map[string]any {
	t.Helper()
	r := f.w.GetSession("/api/policy?repo="+enc(repo), f.ownerS)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	return r.JSON(t)
}

// seeded nhập mẫu generic mới nhất và trả policy (map) như UI nhận.
func (f *fixture) seeded(t *testing.T, repo string) map[string]any {
	t.Helper()
	if r := f.importYaml(repo, genericYAML(t)); r.Status != 201 {
		t.Fatal(r.Status, r.Body)
	}
	return f.live(t, repo)["policy"].(map[string]any)
}

func (f *fixture) str(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := f.e.Pool.QueryRow(t.Context(), sql, args...).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *fixture) count(t *testing.T, repo string) int {
	t.Helper()
	var n int
	if err := f.e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM risk_policy_versions WHERE repo_id = $1", repo).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func clone(p map[string]any) map[string]any {
	b, _ := json.Marshal(p)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func arr(v any) []any { a, _ := v.([]any); return a }

func TestImportCreatesV1OnceOnly(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	if r := f.importYaml(repo, genericYAML(t)); r.Status != 201 {
		t.Fatal(r.Status, r.Body)
	}
	if got := f.str(t, "SELECT source || '|' || author || '|' || reason FROM risk_policy_versions WHERE repo_id = $1 AND version = 1", repo); got != "import|"+f.owner+"|Nhập từ aisdlc/risk-policy.yaml" {
		t.Fatal(got)
	}
	banking, _ := risk.PresetYAML("banking", risk.PresetLatestVersion("banking"))
	if r := f.importYaml(repo, banking); r.Status != 200 || r.JSON(t)["version"] != 1.0 {
		t.Fatal(r.Status, r.Body)
	}
	if f.count(t, repo) != 1 {
		t.Fatal("trùng phiên bản")
	}
	bad := f.importYaml(f.repo(), "levels: [a")
	if bad.Status != 400 || len(arr(bad.JSON(t)["errors"])) == 0 {
		t.Fatal(bad.Status, bad.Body)
	}
}

func TestCliGetSignedVerifiesWithPinnedKey(t *testing.T) { // TestPolicyEnvelopeVerifiesInCLI
	f := newFixture(t)
	repo := f.repo()
	f.seeded(t, repo)
	r := f.w.GetBearer("/api/cli/policy?repo="+enc(repo), f.ingest)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	k := f.w.GetBearer("/api/cli/pubkey", f.ingest).JSON(t)
	key, err := snapshot.PublicKeyFromBase64(k["publicKey"].(string))
	if err != nil {
		t.Fatal(err)
	}
	c, ok := snapshot.VerifyPolicy([]byte(r.Body), key, repo)
	if !ok || c.Version != 1 {
		t.Fatal(ok, c)
	}
	p, err := risk.ParsePolicy(c.YAML)
	if err != nil || strings.Join(p.Levels, ",") != "low,medium,high" {
		t.Fatal(p.Levels, err)
	}
	if _, ok := snapshot.VerifyPolicy([]byte(r.Body), key, "other/repo"); ok {
		t.Fatal("repo khác phải bị từ chối")
	}
}

func TestCliGet404WhenNone(t *testing.T) {
	f := newFixture(t)
	if r := f.w.GetBearer("/api/cli/policy?repo="+enc(f.repo()), f.ingest); r.Status != 404 {
		t.Fatal(r.Status, r.Body)
	}
	if r := f.w.GetBearer("/api/cli/policy", f.ingest); r.Status != 400 || r.JSON(t)["error"] != "Thiếu repo" {
		t.Fatal(r.Status, r.Body)
	}
}

func TestPublishCreatesNextVersion(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	p := clone(f.seeded(t, repo))
	p["rules"] = arr(p["rules"])[1:]
	r := f.publish(repo, 1, p, "bỏ G1", false)
	if r.Status != 201 {
		t.Fatal(r.Status, r.Body)
	}
	if j := r.JSON(t); j["version"] != 2.0 || len(arr(j["loosening"])) == 0 { // tắt rule = nới
		t.Fatal(r.Body)
	}
	if got := f.str(t, "SELECT source FROM risk_policy_versions WHERE repo_id = $1 AND version = 2", repo); got != "web" {
		t.Fatal(got)
	}
	if f.live(t, repo)["version"] != 2.0 {
		t.Fatal("version")
	}
	var vs []map[string]any
	if err := json.Unmarshal([]byte(f.w.GetSession("/api/policy/versions?repo="+enc(repo), f.ownerS).Body), &vs); err != nil {
		t.Fatal(err)
	}
	if vs[0]["version"] != 2.0 || len(arr(vs[0]["changes"])) == 0 || len(arr(vs[1]["changes"])) != 0 || vs[1]["changes"] == nil {
		t.Fatal(vs)
	}
	if !strings.HasSuffix(vs[0]["createdAt"].(string), "Z") {
		t.Fatal(vs[0]["createdAt"])
	}
	if pre := f.publish(repo, 2, p, "từ mẫu", true); pre.Status != 201 {
		t.Fatal(pre.Status, pre.Body)
	}
	if got := f.str(t, "SELECT source FROM risk_policy_versions WHERE repo_id = $1 AND version = 3", repo); got != "preset-update" {
		t.Fatal(got)
	}
	if n := f.str(t, "SELECT count(*)::text FROM audit WHERE action = 'policy.publish' AND repo_id = $1", repo); n != "2" {
		t.Fatal(n)
	}
}

func TestPublishStaleBase409(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	p := f.seeded(t, repo)
	if r := f.publish(repo, 1, p, "một", false); r.Status != 201 {
		t.Fatal(r.Status, r.Body)
	}
	r := f.publish(repo, 1, p, "hai", false)
	j := r.JSON(t)
	if r.Status != 409 || j["message"] != "Đã có phiên bản mới hơn (v2); tải lại để xem." || j["error"] != j["message"] {
		t.Fatal(r.Status, r.Body)
	}
}

func TestPublishInvalidPolicy400WithErrors(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	p := clone(f.seeded(t, repo))
	p["default"] = "khong-co"
	r := f.publish(repo, 1, p, "x", false)
	if r.Status != 400 || len(arr(r.JSON(t)["errors"])) == 0 {
		t.Fatal(r.Status, r.Body)
	}
	if f.count(t, repo) != 1 {
		t.Fatal("không được ghi")
	}
}

func TestPublishRequiresReason(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	p := f.seeded(t, repo)
	for _, reason := range []string{"", "   "} {
		if r := f.publish(repo, 1, p, reason, false); r.Status != 400 || r.JSON(t)["error"] != "Cần nhập lý do xuất bản" {
			t.Fatal(r.Status, r.Body)
		}
	}
}

func TestPublishRequiresSessionAndCsrf(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	p := f.seeded(t, repo)
	pj, _ := json.Marshal(p)
	body := `{"repo":"` + repo + `","baseVersion":1,"reason":"x","policy":` + string(pj) + `}`
	if r := f.w.PostBearer("/api/policy/publish", body, f.ingest); r.Status != 401 && r.Status != 403 {
		t.Fatal(r.Status)
	}
	if r := f.w.PostSession("/api/policy/publish", body, f.ownerS, ""); r.Status != 401 && r.Status != 403 {
		t.Fatal(r.Status)
	}
	if f.count(t, repo) != 1 {
		t.Fatal("không được ghi")
	}
}

func (f *fixture) validate(repo string, p any) map[string]any {
	b, _ := json.Marshal(map[string]any{"repo": repo, "policy": p})
	r := f.w.PostSession("/api/policy/validate", string(b), f.ownerS, f.ownerS.CSRF)
	var m map[string]any
	_ = json.Unmarshal([]byte(r.Body), &m)
	return m
}

func TestValidateReturnsErrorsAndChanges(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	p := clone(f.seeded(t, repo))
	p["rules"] = arr(p["rules"])[1:]
	r := f.validate(repo, p)
	if len(arr(r["errors"])) != 0 || r["errors"] == nil || len(arr(r["changes"])) == 0 {
		t.Fatal(r)
	}
	p["default"] = "khong-co"
	if bad := f.validate(repo, p); len(arr(bad["errors"])) == 0 || len(arr(bad["changes"])) != 0 || bad["changes"] == nil {
		t.Fatal(bad)
	}
}

func TestPolicyValidateUsesGoRisk(t *testing.T) {
	f := newFixture(t)
	p := clone(f.seeded(t, f.repo()))
	p["default"] = "khong-co"
	p["levels"] = []any{"a", "a"}
	pj, _ := json.Marshal(p)
	_, err := risk.PolicyFromJSON(string(pj))
	pe, ok := err.(*risk.PolicyError)
	if !ok || len(pe.Errors) == 0 {
		t.Fatal(err)
	}
	got := arr(f.validate("", p)["errors"])
	if len(got) != len(pe.Errors) {
		t.Fatal(got, pe.Errors)
	}
	for i := range got {
		if got[i] != pe.Errors[i] {
			t.Fatalf("%q != %q", got[i], pe.Errors[i])
		}
	}
	// publish trả đúng lỗi đầu tiên của risk
	r := f.publish(f.repo(), 0, p, "x", false)
	if r.Status != 400 || r.JSON(t)["error"] != pe.Errors[0] {
		t.Fatal(r.Status, r.Body)
	}
}

// Thử phân loại đã bỏ hẳn: không còn route.
func TestClassifyRouteRemoved(t *testing.T) {
	f := newFixture(t)
	r := f.w.PostSession("/api/policy/classify", `{}`, f.ownerS, f.ownerS.CSRF)
	if r.Status != 404 && r.Status != 405 {
		t.Fatal(r.Status, r.Body)
	}
}

func schema1YAML(t *testing.T) string {
	t.Helper()
	y, err := risk.PresetYAML("generic", "2")
	if err != nil {
		t.Fatal(err)
	}
	return y
}

// seedSchema1: repo có phiên bản 1 schema 1 (nhập trước khi web từ chối schema 1), ghi thẳng vào DB.
func (f *fixture) seedSchema1(t *testing.T, repo string) {
	t.Helper()
	p, err := risk.ParsePolicy(schema1YAML(t))
	if err != nil {
		t.Fatal(err)
	}
	p.Version = 1
	y, err := p.ToYAML()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.Pool.Exec(t.Context(), "INSERT INTO risk_policy_versions(repo_id, version, body, hash, author, reason, source) "+
		"VALUES ($1, 1, $2, $3, $4, 'import', 'import')", repo, y, p.Hash(), f.owner); err != nil {
		t.Fatal(err)
	}
}

// Nhập lần đầu chính sách schema 1: 400 với đúng thông điệp Usable (nhắc `aisdlc policy migrate`), không tạo phiên bản.
func TestImportRejectsSchema1(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	r := f.importYaml(repo, schema1YAML(t))
	p, _ := risk.ParsePolicy(schema1YAML(t))
	var pe *risk.PolicyError
	if !errors.As(p.Usable(), &pe) {
		t.Fatal("Usable phải trả *risk.PolicyError")
	}
	want := pe.Errors[0]
	if r.Status != 400 || r.JSON(t)["error"] != want || !strings.Contains(want, "aisdlc policy migrate") {
		t.Fatal(r.Status, r.Body)
	}
	if f.count(t, repo) != 0 {
		t.Fatal("không được tạo phiên bản")
	}
}

func (f *fixture) migrateDraft(repo string) webtest.Resp {
	b, _ := json.Marshal(map[string]any{"repo": repo})
	return f.w.PostSession("/api/policy/migrate-draft", string(b), f.ownerS, f.ownerS.CSRF)
}

func TestMigrateDraftFromSchema1(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	f.seedSchema1(t, repo)
	if f.live(t, repo)["policy"].(map[string]any)["schema"] != 1.0 {
		t.Fatal("chính sách sống phải là schema 1")
	}
	r := f.migrateDraft(repo)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	m := r.JSON(t)
	pol := m["policy"].(map[string]any)
	if pol["schema"] != 2.0 || pol["preset"] != nil {
		t.Fatal(pol)
	}
	for _, x := range arr(pol["rules"]) {
		if x.(map[string]any)["describe"] == "" {
			t.Fatal(x)
		}
	}
	ch := arr(m["changes"])
	if len(ch) == 0 || ch[0].(map[string]any)["kind"] != "SCHEMA_MIGRATED" {
		t.Fatal(ch)
	}
	if u, has := f.live(t, repo)["presetUpdate"]; !has || u != nil { // schema 1 chỉ đọc: không đề nghị cập nhật mẫu
		t.Fatal(u)
	}
	// không ghi gì vào DB
	if f.count(t, repo) != 1 {
		t.Fatal("migrate-draft không được tạo phiên bản")
	}
	// schema 2 thì không có gì để chuyển
	repo2 := f.repo()
	f.seeded(t, repo2)
	if r := f.migrateDraft(repo2); r.Status != 400 {
		t.Fatal(r.Status, r.Body)
	}
	if r := f.migrateDraft(f.repo()); r.Status != 404 {
		t.Fatal(r.Status, r.Body)
	}
}

func TestPublishRejectsSchema1(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	f.seedSchema1(t, repo)
	p1 := f.live(t, repo)["policy"]
	r := f.publish(repo, 1, p1, "x", false)
	if r.Status != 400 || !strings.Contains(r.JSON(t)["error"].(string), "schema 1") {
		t.Fatal(r.Status, r.Body)
	}
	if f.count(t, repo) != 1 {
		t.Fatal("không được tạo phiên bản")
	}
	if errs := arr(f.validate(repo, p1)["errors"]); len(errs) == 0 {
		t.Fatal("validate phải báo lỗi schema 1")
	}
}

func TestVersionsShowSchemaMigrated(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	f.seedSchema1(t, repo)
	pol := f.migrateDraft(repo).JSON(t)["policy"]
	if r := f.publish(repo, 1, pol, "chuyển schema 2", false); r.Status != 201 {
		t.Fatal(r.Status, r.Body)
	}
	r := f.w.GetSession("/api/policy/versions?repo="+enc(repo), f.ownerS)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(r.Body), &list); err != nil {
		t.Fatal(err, r.Body)
	}
	ch := arr(list[0]["changes"])
	if len(ch) == 0 || ch[0].(map[string]any)["kind"] != "SCHEMA_MIGRATED" {
		t.Fatal(list[0])
	}
}

func TestPresetUpdateWhenBundledNewer(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	y := genericYAML(t)
	old := regexpReplace(t, strings.Replace(y, `version: "`+risk.PresetLatestVersion("generic")+`"`, `version: "0"`, 1), `(?m)^  - \{ id: G4.*\n`)
	if r := f.importYaml(repo, old); r.Status != 201 {
		t.Fatal(r.Status, r.Body)
	}
	u, _ := f.live(t, repo)["presetUpdate"].(map[string]any)
	if u == nil || u["name"] != "generic" || u["from"] != "0" || u["to"] != risk.PresetLatestVersion("generic") ||
		u["baseMissing"] != true || len(arr(u["changes"])) == 0 {
		t.Fatal(u)
	}
	// bản đang dùng mẫu mới nhất → không có thông báo
	cur := f.repo()
	f.seeded(t, cur)
	lv := f.live(t, cur)
	if v, has := lv["presetUpdate"]; !has || v != nil {
		t.Fatal(lv["presetUpdate"])
	}
	if ids, _ := json.Marshal(lv["presetRuleIds"]); string(ids) != `["G1","G2","G3","G4"]` {
		t.Fatal(string(ids))
	}
	if len(arr(f.live(t, repo)["presetRuleIds"])) != 0 { // mẫu v0 không còn đi kèm
		t.Fatal("presetRuleIds")
	}
	p, err := risk.ParsePolicy(lv["yaml"].(string))
	if err != nil || len(p.Rules) != 4 {
		t.Fatal(err, p.Rules)
	}
}

func snapshotBody(repo, key string) string {
	return fmt.Sprintf(`{"schemaVersion":1,"repo":{"id":"%s","branch":"feature/%s"},"clientId":"A",
 "capturedAt":"2026-11-05T09:00:00Z","sender":"x","feature":{"key":"%s","title":"T","flow":"FULL","owner":null},
 "gates":[{"gate":"BRD","state":"AWAITING_APPROVAL"}],"steps":[],"cases":[],"openQuestions":[],
 "artifacts":{},"eval":null,"review":null,"truncated":false}`, repo, key, key)
}

func (f *fixture) event(repo, typ, detail string) int {
	d, _ := json.Marshal(detail)
	return f.w.PostBearer("/api/ingest/events", fmt.Sprintf(`{"version":2,"repoId":"%s","events":[{"type":"%s","at":"2026-11-04T10:00:00Z","feature":"K-1","detail":%s}]}`,
		repo, typ, d), f.ingest).Status
}

func TestEventsUpdateFeatureRisk(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	f.seeded(t, repo)
	if r := f.w.PostBearer("/api/ingest/snapshot", snapshotBody(repo, "K-1"), f.ingest); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if s := f.event(repo, "RISK_CLASSIFIED", `{"level":"high","policyVersion":3,"matched":["G1"]}`); s != 200 {
		t.Fatal(s)
	}
	q := "SELECT risk_level || '|' || risk_policy_version FROM features WHERE repo_id = $1 AND feature_key = 'K-1'"
	if got := f.str(t, q, repo); got != "high|3" && got != "HIGH|3" {
		t.Fatal(got)
	}
	if c := f.live(t, repo)["featureCounts"].(map[string]any)["3"]; c != 1.0 {
		t.Fatal(c)
	}
	if s := f.event(repo, "RISK_OVERRIDDEN", `{"from":"high","to":"medium","reason":"r"}`); s != 200 {
		t.Fatal(s)
	}
	if got := strings.ToLower(f.str(t, q, repo)); got != "medium|3" {
		t.Fatal(got)
	}
	board := f.w.GetSession("/api/board?repo="+enc(repo), f.ownerS).JSON(t)
	if !strings.EqualFold(arr(board["cards"])[0].(map[string]any)["riskLevel"].(string), "medium") {
		t.Fatal(board)
	}
}

func TestEventBeforeSnapshotIsAppliedWhenFeatureRowAppears(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	if s := f.event(repo, "RISK_CLASSIFIED", `{"level":"high","policyVersion":0,"matched":[]}`); s != 200 {
		t.Fatal(s)
	}
	if s := f.event(repo, "RISK_OVERRIDDEN", `{"from":"high","to":"low","reason":"r"}`); s != 200 {
		t.Fatal(s)
	}
	if r := f.w.PostBearer("/api/ingest/snapshot", snapshotBody(repo, "K-1"), f.ingest); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if got := strings.ToLower(f.str(t, "SELECT risk_level FROM features WHERE repo_id = $1", repo)); got != "low" {
		t.Fatal(got)
	}
	// policyVersion <= 0 (chế độ file) → NULL
	if got := f.str(t, "SELECT coalesce(risk_policy_version::text,'null') FROM features WHERE repo_id = $1", repo); got != "null" {
		t.Fatal(got)
	}
	board := f.w.GetSession("/api/board?repo="+enc(repo), f.ownerS).JSON(t)
	if !strings.EqualFold(arr(board["cards"])[0].(map[string]any)["riskLevel"].(string), "low") {
		t.Fatal(board)
	}
}

func TestPublishConcurrentSameBaseOne201One409(t *testing.T) {
	f := newFixture(t)
	for round := 0; round < 5; round++ {
		repo := f.repo()
		p := f.seeded(t, repo)
		start := make(chan struct{})
		codes := make(chan int, 2)
		msgs := make(chan string, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				r := f.publish(repo, 1, p, fmt.Sprintf("đua %d", i), false)
				codes <- r.Status
				if r.Status == 409 {
					var m map[string]any
					_ = json.Unmarshal([]byte(r.Body), &m)
					msgs <- fmt.Sprint(m["message"])
				}
			}(i)
		}
		close(start)
		wg.Wait()
		close(codes)
		close(msgs)
		var ok, conflict int
		for c := range codes {
			switch c {
			case 201:
				ok++
			case 409:
				conflict++
			default:
				t.Fatal(c)
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatal(ok, conflict)
		}
		for m := range msgs {
			if m != "Đã có phiên bản mới hơn (v2); tải lại để xem." {
				t.Fatal(m)
			}
		}
		if f.count(t, repo) != 2 {
			t.Fatal("số phiên bản")
		}
	}
}

func TestPolicyBodyLimitAndShape(t *testing.T) {
	f := newFixture(t)
	big := `{"repo":"x","yaml":"` + strings.Repeat("a", 300*1024) + `"}`
	if r := f.w.PostBearer("/api/cli/policy/import", big, f.ingest); r.Status != 413 || r.JSON(t)["error"] != "Payload quá lớn" {
		t.Fatal(r.Status, r.Body)
	}
	for body, want := range map[string]string{"": "Body phải là object JSON", "[1]": "Body phải là object JSON", "{x": "JSON không hợp lệ",
		`{"yaml":"a"}`: "Thiếu repo", `{"repo":"x"}`: "Thiếu yaml", "{\"repo\":\"a\\u0000\",\"yaml\":\"a\"}": "Chuỗi chứa ký tự NUL (U+0000)"} {
		if r := f.w.PostBearer("/api/cli/policy/import", body, f.ingest); r.Status != 400 || r.JSON(t)["error"] != want {
			t.Fatal(body, r.Status, r.Body)
		}
	}
	for _, bv := range []string{`"1"`, `1.0`, `null`, `3000000000`} {
		b := `{"repo":"x","reason":"r","baseVersion":` + bv + `,"policy":{}}`
		if r := f.w.PostSession("/api/policy/publish", b, f.ownerS, f.ownerS.CSRF); r.Status != 400 || r.JSON(t)["error"] != "Thiếu baseVersion" {
			t.Fatal(bv, r.Status, r.Body)
		}
	}
	if r := f.w.PostSession("/api/policy/publish", `{"repo":"x","reason":"r","baseVersion":0}`, f.ownerS, f.ownerS.CSRF); r.Status != 400 ||
		r.JSON(t)["error"] != "policy phải là object JSON" {
		t.Fatal(r.Status, r.Body)
	}
}

func regexpReplace(t *testing.T, s, re string) string {
	t.Helper()
	return regexp.MustCompile(re).ReplaceAllString(s, "")
}

// Token ci chỉ được đọc chính sách (GET /api/cli/policy); mọi đường /api/cli/** khác và POST vẫn bị chặn.
func TestCliPolicyReadableByCIToken(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	f.seeded(t, repo)
	ci := webtest.CliToken(t, f.e.Pool, "ci-token", "pl-ci")
	if r := f.w.GetBearer("/api/cli/policy?repo="+enc(repo), ci); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if r := f.w.GetBearer("/api/cli/pubkey", ci); r.Status != 403 {
		t.Fatal("ci không được đọc đường cli khác:", r.Status)
	}
	if r := f.w.PostBearer("/api/cli/policy/import", `{"repo":"`+repo+`","yaml":"x"}`, ci); r.Status != 403 {
		t.Fatal("ci không được ghi chính sách:", r.Status)
	}
}
