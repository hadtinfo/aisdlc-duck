package gates_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/web"
	"aisdlc/internal/web/signing"
	"aisdlc/internal/web/webtest"
)

type fixture struct {
	e      *webtest.Env
	w      *webtest.Client
	keys   *signing.Keys
	ingest string
	login  string
	leadS  webtest.Session
	devS   webtest.Session
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	f.e = webtest.New(t, func(d web.Deps) http.Handler {
		k, err := signing.Load(filepath.Join(t.TempDir(), "signing-key"), "")
		if err != nil {
			t.Fatal(err)
		}
		f.keys = k
		d.Keys = k
		return web.NewHandler(d)
	})
	f.w = f.e.Client(t)
	f.login = webtest.CliToken(t, f.e.Pool, "add", "cli.gate", "Cổng CLI")
	leadTok := webtest.CliToken(t, f.e.Pool, "add", "im.lead", "Lead", "--role", "lead")
	devTok := webtest.CliToken(t, f.e.Pool, "add", "im.dev", "Dev")
	_, f.ingest = f.w.CreateToken(f.w.Session(f.login), "ingest", "cli")
	f.leadS = f.w.Session(leadTok)
	f.devS = f.w.Session(devTok)
	return f
}

func (f *fixture) pubkey(t *testing.T) map[string]any {
	t.Helper()
	r := f.w.GetBearer("/api/cli/pubkey", f.ingest)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	return r.JSON(t)
}

func (f *fixture) verified(t *testing.T, body string) map[string]any {
	t.Helper()
	j := f.pubkey(t)
	pub, err := snapshot.PublicKeyFromBase64(j["publicKey"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if j["keyId"] != snapshot.KeyID(pub) {
		t.Fatal("keyId không khớp")
	}
	if _, has := j["endorsement"]; has {
		t.Fatal("không có khóa cũ thì không có endorsement")
	}
	var env snapshot.SignedEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err, body)
	}
	payload, ok := env.Verify(pub)
	if !ok {
		t.Fatal("chữ ký sai")
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (f *fixture) mode(t *testing.T, repo, configured string) string {
	t.Helper()
	r := f.w.PostBearer("/api/cli/mode", `{"repo":"`+repo+`","configured":"`+configured+`"}`, f.ingest)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	p := f.verified(t, r.Body)
	if p["repo"] != repo || p["at"] == "" || p["issuedAt"] == "" {
		t.Fatal(p)
	}
	return p["mode"].(string)
}

func (f *fixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.e.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) str(t *testing.T, sql string) string {
	t.Helper()
	var s string
	if err := f.e.Pool.QueryRow(context.Background(), sql).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestModePinsWebFirstTimeAndKeepsIt(t *testing.T) { // CliGateIT.modePinsWebFirstTimeAndKeepsIt
	f := newFixture(t)
	if f.mode(t, "r/cli-local", "local") != "local" {
		t.Fatal("local")
	}
	if f.count(t, "SELECT count(*) FROM repo_settings WHERE repo_id = 'r/cli-local'") != 0 {
		t.Fatal("local không ghim")
	}
	if f.mode(t, "r/cli-pin", "web") != "web" || f.mode(t, "r/cli-pin", "local") != "web" {
		t.Fatal("web phải được giữ")
	}
	if by := f.str(t, "SELECT set_by FROM repo_settings WHERE repo_id = 'r/cli-pin'"); by != "cli.gate" {
		t.Fatal(by)
	}
	if f.count(t, "SELECT count(*) FROM audit WHERE action = 'mode.pin' AND repo_id = 'r/cli-pin' AND channel = 'cli'") != 1 {
		t.Fatal("audit mode.pin đúng 1 dòng")
	}
	r := f.w.PostBearer("/api/cli/mode", `{"repo":"r/x","configured":"lan"}`, f.ingest)
	if r.Status != 400 || r.Body != `{"error":"configured phải là local hoặc web"}` {
		t.Fatal(r.Status, r.Body)
	}
	for _, b := range []string{`{"configured":"web"}`, `{"repo":"  ","configured":"web"}`, `{"repo":"r"}`, `[]`, `x`} {
		if r := f.w.PostBearer("/api/cli/mode", b, f.ingest); r.Status != 400 {
			t.Fatal(b, r.Status, r.Body)
		}
	}
}

func TestOnlyIngestTokensReachCliApi(t *testing.T) { // CliGateIT.onlyIngestTokensReachCliApi (phần pubkey/mode)
	f := newFixture(t)
	if r := f.w.GetBearer("/api/cli/pubkey", f.login); r.Status != 403 {
		t.Fatal(r.Status)
	}
	if r := f.w.PostBearer("/api/cli/mode", `{"repo":"r","configured":"web"}`, f.login); r.Status != 403 {
		t.Fatal(r.Status)
	}
	if r := f.w.GetBearer("/api/cli/pubkey", "khong-co"); r.Status != 401 {
		t.Fatal(r.Status)
	}
	if r := f.w.Do("GET", "/api/cli/pubkey", "", nil); r.Status != 401 || r.Body != `{"error":"unauthorized"}` {
		t.Fatal(r.Status, r.Body)
	}
	f.pubkey(t)
}

func TestEnvelopeVerifiesWithGoCLI(t *testing.T) {
	f := newFixture(t)
	j := f.pubkey(t)
	pub, err := snapshot.PublicKeyFromBase64(j["publicKey"].(string))
	if err != nil {
		t.Fatal(err)
	}
	env := f.keys.Sign([]byte(`{"a":1}`))
	if b, ok := env.Verify(pub); !ok || string(b) != `{"a":1}` {
		t.Fatal("phong bì ký ở server không qua Verify của CLI")
	}
	if env.KeyID != j["keyId"] {
		t.Fatal(env.KeyID, j["keyId"])
	}
	// /api/cli/mode: at/issuedAt là ISO-8601 UTC
	r := f.w.PostBearer("/api/cli/mode", `{"repo":"r/iso","configured":"web"}`, f.ingest)
	p := f.verified(t, r.Body)
	re := regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z$`)
	if !re.MatchString(p["at"].(string)) || !re.MatchString(p["issuedAt"].(string)) {
		t.Fatal(p)
	}
	if p["issuedAt"] != "2026-11-05T10:00:00Z" {
		t.Fatal("issuedAt phải theo đồng hồ tiêm", p["issuedAt"])
	}
}

func TestPubkeyIncludesEndorsementWithPreviousKey(t *testing.T) {
	dir := t.TempDir()
	if _, err := signing.Load(filepath.Join(dir, "old"), ""); err != nil {
		t.Fatal(err)
	}
	e := webtest.New(t, func(d web.Deps) http.Handler {
		k, err := signing.Load(filepath.Join(dir, "new"), filepath.Join(dir, "old"))
		if err != nil {
			t.Fatal(err)
		}
		d.Keys = k
		return web.NewHandler(d)
	})
	w := e.Client(t)
	_, ing := w.CreateToken(w.Session(webtest.CliToken(t, e.Pool, "add", "e.u", "E")), "ingest", "x")
	m := w.GetBearer("/api/cli/pubkey", ing).JSON(t)
	if s, _ := m["endorsement"].(string); s == "" {
		t.Fatal(m)
	}
}

func TestRepoModeChangeOnlyByLead(t *testing.T) { // ImportIT.repoModeChangeOnlyByLead
	f := newFixture(t)
	if f.mode(t, "r/im-mode", "web") != "web" {
		t.Fatal()
	}
	// GET /api/repos có cả repo chỉ có trong features (mặc định local)
	_, err := f.e.Pool.Exec(context.Background(), "INSERT INTO features(repo_id, feature_key, snapshot, captured_at, client_id, "+
		"first_sender_login, last_sender_login) VALUES ('r/only-feature','K-1','{}'::jsonb, now(), 'c', 'x', 'x')")
	if err != nil {
		t.Fatal(err)
	}
	r := f.w.GetSession("/api/repos", f.devS)
	var repos []map[string]any
	if r.Status != 200 || json.Unmarshal([]byte(r.Body), &repos) != nil {
		t.Fatal(r.Status, r.Body)
	}
	var mine, only map[string]any
	for _, x := range repos {
		switch x["id"] {
		case "r/im-mode":
			mine = x
		case "r/only-feature":
			only = x
		}
	}
	if mine == nil || mine["approvals"] != "web" || mine["setBy"] != "cli.gate" || mine["setAt"] == "" || mine["setAt"] == nil {
		t.Fatal(mine)
	}
	if only == nil || only["approvals"] != "local" || only["setBy"] != nil || only["setAt"] != nil {
		t.Fatal(only)
	}
	if r := f.w.GetBearer("/api/repos", f.login); r.Status != 200 {
		t.Fatal(r.Status)
	}
	if r := f.w.Do("GET", "/api/repos", "", nil); r.Status != 401 {
		t.Fatal(r.Status)
	}

	body := `{"repo":"r/im-mode","approvals":"local"}`
	if r := f.w.PostSession("/api/repos/mode", body, f.devS, f.devS.CSRF); r.Status != 403 ||
		r.Body != `{"error":"Chỉ lead được đổi chế độ duyệt"}` {
		t.Fatal(r.Status, r.Body)
	}
	if r := f.w.PostSession("/api/repos/mode", body, f.leadS, ""); r.Status != 403 || r.Body != `{"error":"forbidden"}` {
		t.Fatal(r.Status, r.Body)
	}
	if r := f.w.PostBearer("/api/repos/mode", body, f.login); r.Status != 403 {
		t.Fatal(r.Status)
	}
	r = f.w.PostSession("/api/repos/mode", `{"repo":"r/im-mode","approvals":"lan"}`, f.leadS, f.leadS.CSRF)
	if r.Status != 400 || r.Body != `{"error":"approvals phải là local hoặc web"}` {
		t.Fatal(r.Status, r.Body)
	}
	ok := f.w.PostSession("/api/repos/mode", body, f.leadS, f.leadS.CSRF)
	if ok.Status != 200 {
		t.Fatal(ok.Status, ok.Body)
	}
	m := ok.JSON(t)
	if m["id"] != "r/im-mode" || m["approvals"] != "local" || m["setBy"] != "im.lead" || m["setAt"] != "2026-11-05T10:00:00Z" {
		t.Fatal(m)
	}
	if s := f.str(t, "SELECT approvals || '|' || set_by FROM repo_settings WHERE repo_id = 'r/im-mode'"); s != "local|im.lead" {
		t.Fatal(s)
	}
	if s := f.str(t, "SELECT detail->>'from' || '>' || (detail->>'to') FROM audit "+
		"WHERE action = 'mode.set' AND repo_id = 'r/im-mode' AND login = 'im.lead' AND channel = 'web'"); s != "web>local" {
		t.Fatal(s)
	}
}

func TestRunFailsOnCorruptKey(t *testing.T) { // Run từ chối khởi động, không ghi đè
	file := filepath.Join(t.TempDir(), "signing-key")
	if err := os.WriteFile(file, []byte("hỏng"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := web.Run(context.Background(), web.Config{Listen: "127.0.0.1:0", DBURL: "postgres://127.0.0.1:1/x", KeyFile: file})
	if err == nil || !regexp.MustCompile(`Không nạp được khóa ký`).MatchString(err.Error()) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(file); string(b) != "hỏng" {
		t.Fatal("file bị ghi đè")
	}
}
