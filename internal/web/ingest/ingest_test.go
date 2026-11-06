package ingest_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"aisdlc/internal/web"
	"aisdlc/internal/web/webtest"
)

type fixture struct {
	e     *webtest.Env
	w     *webtest.Client
	token string
	mu    sync.Mutex
	seen  [][2]string
}

func (f *fixture) changes() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]string(nil), f.seen...)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	f.e = webtest.New(t, func(d web.Deps) http.Handler {
		d.Changed = func(repo, feature string) {
			f.mu.Lock()
			f.seen = append(f.seen, [2]string{repo, feature})
			f.mu.Unlock()
		}
		return web.NewHandler(d)
	})
	f.w = f.e.Client(t)
	f.token = webtest.CliToken(t, f.e.Pool, "add", "ingest.bob", "Bob", "--email", "bob@corp")
	return f
}

func (f *fixture) post(path, body, bearer string) webtest.Resp {
	h := map[string]string{"Content-Type": "application/json"}
	if bearer != "" {
		h["Authorization"] = "Bearer " + bearer
	}
	return f.w.Do("POST", path, body, h)
}

func (f *fixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.e.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func snap(capturedAt, clientID string) string {
	return fmt.Sprintf(`{"schemaVersion":1,"repo":{"id":"gitlab.corp/pay/svc","branch":"b"},"clientId":"%s",
 "capturedAt":"%s","sender":"x","feature":{"key":"PAY-1","title":"t","flow":"FULL"},
 "gates":[],"steps":[],"cases":[],"openQuestions":[],"artifacts":{},"eval":null,"review":null,
 "truncated":false}`, clientID, capturedAt)
}

const (
	t1 = "2026-11-03T10:00:00Z"
	t2 = "2026-11-03T11:00:00Z"
)

func TestRejectsMissingOrBadToken401(t *testing.T) {
	f := newFixture(t)
	for _, c := range [][3]string{{"/api/ingest/snapshot", snap(t1, "A"), ""}, {"/api/ingest/snapshot", snap(t1, "A"), "nope"}, {"/api/ingest/events", "{}", "nope"}} {
		if r := f.post(c[0], c[1], c[2]); r.Status != 401 {
			t.Fatalf("%v: %d %s", c, r.Status, r.Body)
		}
	}
	if n := f.count(t, "SELECT count(*) FROM features"); n != 0 {
		t.Fatal(n)
	}
}

func TestAcceptsSnapshotAndStoresLatest(t *testing.T) {
	f := newFixture(t)
	r := f.post("/api/ingest/snapshot", snap(t1, "A"), f.token)
	if r.Status != 200 || !strings.Contains(r.Body, `"accepted":true`) || !strings.Contains(r.Body, `"clockSkew":false`) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var client, first, last, snapText string
	var skew bool
	err := f.e.Pool.QueryRow(context.Background(), "SELECT client_id, first_sender_login, last_sender_login, clock_skew, snapshot::text FROM features "+
		"WHERE repo_id = 'gitlab.corp/pay/svc' AND feature_key = 'PAY-1'").Scan(&client, &first, &last, &skew, &snapText)
	if err != nil {
		t.Fatal(err)
	}
	if client != "A" || first != "ingest.bob" || last != "ingest.bob" || skew || !strings.Contains(snapText, `"schemaVersion": 1`) {
		t.Fatalf("%s %s %s %v %s", client, first, last, skew, snapText)
	}
	if n := f.count(t, "SELECT count(*) FROM repos WHERE id = 'gitlab.corp/pay/svc'"); n != 1 {
		t.Fatal(n)
	}
	if got := f.changes(); len(got) != 1 || got[0] != [2]string{"gitlab.corp/pay/svc", "PAY-1"} {
		t.Fatalf("%v", got)
	}
	// bản mới hơn thay thế
	if r := f.post("/api/ingest/snapshot", snap(t2, "A"), f.token); !strings.Contains(r.Body, `"accepted":true`) {
		t.Fatal(r.Body)
	}
	if n := f.count(t, "SELECT count(*) FROM features WHERE captured_at = $1::timestamptz", t2); n != 1 {
		t.Fatal(n)
	}
}

func TestOlderSameClientIgnored(t *testing.T) {
	f := newFixture(t)
	f.post("/api/ingest/snapshot", snap(t2, "A"), f.token)
	n0 := len(f.changes())
	r := f.post("/api/ingest/snapshot", snap(t1, "A"), f.token)
	if r.Status != 200 || !strings.Contains(r.Body, `"accepted":false`) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if n := f.count(t, "SELECT count(*) FROM features WHERE captured_at = $1::timestamptz", t2); n != 1 {
		t.Fatal(n)
	}
	if len(f.changes()) != n0 {
		t.Fatal("không được phát FeatureChanged")
	}
}

func TestOlderOtherClientAcceptedWithSkew(t *testing.T) {
	f := newFixture(t)
	f.post("/api/ingest/snapshot", snap(t2, "A"), f.token)
	r := f.post("/api/ingest/snapshot", snap(t1, "B"), f.token)
	if !strings.Contains(r.Body, `"accepted":true`) || !strings.Contains(r.Body, `"clockSkew":true`) {
		t.Fatal(r.Body)
	}
	var client string
	var skew bool
	if err := f.e.Pool.QueryRow(context.Background(), "SELECT client_id, clock_skew FROM features").Scan(&client, &skew); err != nil {
		t.Fatal(err)
	}
	if client != "B" || !skew {
		t.Fatal(client, skew)
	}
}

func TestRejectsWrongSchemaVersion400(t *testing.T) {
	f := newFixture(t)
	cases := []struct{ body, msg string }{
		{strings.Replace(snap(t1, "A"), `"schemaVersion":1`, `"schemaVersion":2`, 1), "schemaVersion phải bằng 1"},
		{strings.Replace(snap(t1, "A"), `"schemaVersion":1`, `"schemaVersion":1.0`, 1), "schemaVersion phải bằng 1"},
		{strings.Replace(snap(t1, "A"), `"key":"PAY-1",`, ``, 1), "Thiếu feature.key"},
		{snap("not-a-date", "A"), "Thời điểm không hợp lệ: not-a-date"},
		{"{bad", "JSON không hợp lệ"},
		{"", "Snapshot phải là JSON object"},
		{"[]", "Snapshot phải là JSON object"},
		{strings.Replace(snap(t1, "A"), `"clientId":"A"`, `"clientId":"  "`, 1), "Thiếu clientId"},
	}
	for _, c := range cases {
		r := f.post("/api/ingest/snapshot", c.body, f.token)
		if r.Status != 400 || r.JSON(t)["error"] != c.msg {
			t.Errorf("%.40q: %d %s", c.body, r.Status, r.Body)
		}
	}
	if n := f.count(t, "SELECT count(*) FROM features"); n != 0 {
		t.Fatal(n)
	}
}

func TestInstantFormatsLikeJava(t *testing.T) {
	f := newFixture(t)
	for _, ok := range []string{"2026-11-03T10:00:00Z", "2026-11-03T10:00Z", "2026-11-03T10:00:00.123456789Z", "2026-11-03T10:00:00+07:00", "2026-11-03t10:00:00z"} {
		if r := f.post("/api/ingest/snapshot", snap(ok, "A"), f.token); r.Status != 200 {
			t.Errorf("%s: %d %s", ok, r.Status, r.Body)
		}
	}
	for _, bad := range []string{"2026-11-03", "2026-11-03T10:00:00", "2026-02-30T10:00:00Z", "2026-11-03T25:00:00Z", "2026-11-03 10:00:00Z"} {
		if r := f.post("/api/ingest/snapshot", snap(bad, "A"), f.token); r.Status != 400 {
			t.Errorf("%s: %d %s", bad, r.Status, r.Body)
		}
	}
}

func TestRejectsOver8MB413(t *testing.T) {
	f := newFixture(t)
	big := strings.Replace(snap(t1, "A"), `"truncated":false`, `"pad":"`+strings.Repeat("x", 9<<20)+`"`, 1)
	r := f.post("/api/ingest/snapshot", big, f.token)
	if r.Status != 413 || r.JSON(t)["error"] != "Payload vượt 8 MB" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if n := f.count(t, "SELECT count(*) FROM features"); n != 0 {
		t.Fatal(n)
	}
}

func TestOver8MbReturns413ForAnySize(t *testing.T) {
	f := newFixture(t)
	for _, mb := range []int{9, 20, 40} {
		big := `{"repoId":"r","pad":"` + strings.Repeat("x", mb<<20) + `"}`
		if r := f.post("/api/ingest/events", big, f.token); r.Status != 413 {
			t.Fatalf("%d MB: %d %.100s", mb, r.Status, r.Body)
		}
	}
	// thiếu token thì 401 dù thân lớn
	if r := f.post("/api/ingest/events", strings.Repeat("x", 20<<20), ""); r.Status != 401 {
		t.Fatalf("%d", r.Status)
	}
}

// Review Focus 2: qua httptest.Server thật, client phải nhận phản hồi chứ không bị reset kết nối.
func TestIngestTooLargeReturns413(t *testing.T) {
	f := newFixture(t)
	for _, mb := range []int{9, 64} {
		for _, chunked := range []bool{false, true} {
			body := io.Reader(strings.NewReader(strings.Repeat("x", mb<<20)))
			req, _ := http.NewRequest("POST", f.e.Server.URL+"/api/ingest/events", body)
			if chunked {
				req.ContentLength = -1
			} else {
				req.ContentLength = int64(mb << 20)
			}
			req.Header.Set("Authorization", "Bearer "+f.token)
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%d MB chunked=%v: client không nhận được phản hồi: %v", mb, chunked, err)
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != 413 || !strings.Contains(string(b), "Payload vượt 8 MB") {
				t.Fatalf("%d MB chunked=%v: %d %s", mb, chunked, res.StatusCode, b)
			}
		}
	}
}

// Review Focus 2: không token mà body lớn → 401 sạch, không reset.
func TestUnauthorizedLargeBodyGets401(t *testing.T) {
	f := newFixture(t)
	for _, chunked := range []bool{false, true} {
		for _, path := range []string{"/api/ingest/snapshot", "/api/ingest/events"} {
			req, _ := http.NewRequest("POST", f.e.Server.URL+path, strings.NewReader(strings.Repeat("x", 32<<20)))
			if chunked {
				req.ContentLength = -1
			}
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s chunked=%v: client không nhận được phản hồi: %v", path, chunked, err)
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != 401 || strings.TrimSpace(string(b)) != `{"error":"unauthorized"}` {
				t.Fatalf("%s chunked=%v: %d %s", path, chunked, res.StatusCode, b)
			}
		}
	}
}

func TestAuthBypassAttemptsFailClosed(t *testing.T) {
	f := newFixture(t)
	for _, p := range []string{"/api;x/ingest/events", "/api/ingest;x/events", "/api/nope", "/api/login;x", "/api/%69ngest/events",
		"/./api/ingest/events", "/api/./ingest/events", "/x/../api/ingest/events", "//api/ingest/events", "/api"} {
		r := f.w.Raw("POST", p, `{"repoId":"r","events":[]}`, map[string]string{"Content-Type": "application/json"})
		if r.Status != 401 {
			t.Errorf("%s: %d %s", p, r.Status, r.Body)
		}
	}
	if r := f.w.Do("GET", "/index.html", "", nil); r.Status == 401 {
		t.Fatal("đường tĩnh không cần auth")
	}
	if n := f.count(t, "SELECT count(*) FROM events"); n != 0 {
		t.Fatal(n)
	}
}

func TestNulInPayloadRejected400(t *testing.T) {
	f := newFixture(t)
	r := f.post("/api/ingest/snapshot", strings.Replace(snap(t1, "A"), `"title":"t"`, `"title":"a\u0000b"`, 1), f.token)
	if r.Status != 400 || r.JSON(t)["error"] != "Chuỗi chứa ký tự NUL (U+0000)" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	r = f.post("/api/ingest/snapshot", strings.Replace(snap(t1, "A"), `"title"`, `"ti\u0000tle"`, 1), f.token)
	if r.Status != 400 || r.JSON(t)["error"] != "Tên trường chứa ký tự NUL (U+0000)" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	ev := `{"repoId":"r","events":[{"type":"COMMAND","at":"2026-11-03T10:00:00Z","detail":"x\u0000"}]}`
	if r := f.post("/api/ingest/events", ev, f.token); r.Status != 400 {
		t.Fatalf("%d", r.Status)
	}
	if f.count(t, "SELECT count(*) FROM features") != 0 || f.count(t, "SELECT count(*) FROM events") != 0 {
		t.Fatal("không được ghi gì")
	}
}

const batch = `{"repoId":"gitlab.corp/pay/svc","clientId":"A","events":[
 {"type":"SESSION_START","at":"2026-11-03T10:00:00Z","feature":null,"step":null,"path":null,"outOfScope":false,"sender":"x","detail":null},
 {"type":"FILE_EDIT","at":"2026-11-03T10:01:00Z","feature":"PAY-1","step":"S1","path":"src/A.java","outOfScope":true,"sender":"x","detail":null},
 {"type":"COMMAND","at":"2026-11-03T10:02:00Z","feature":"PAY-1","step":"S1","path":null,"outOfScope":false,"sender":"x","detail":"step finish S1"}]}`

func TestEventsBatchAppended(t *testing.T) {
	f := newFixture(t)
	r := f.post("/api/ingest/events", batch, f.token)
	if r.Status != 200 || r.JSON(t)["appended"] != float64(3) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if n := f.count(t, "SELECT count(*) FROM events WHERE sender_login = 'ingest.bob'"); n != 3 {
		t.Fatal(n)
	}
	var oos bool
	var detail string
	f.e.Pool.QueryRow(context.Background(), "SELECT out_of_scope FROM events WHERE type = 'FILE_EDIT'").Scan(&oos)
	f.e.Pool.QueryRow(context.Background(), "SELECT detail FROM events WHERE type = 'COMMAND'").Scan(&detail)
	if !oos || detail != "step finish S1" {
		t.Fatal(oos, detail)
	}
	if got := f.changes(); len(got) != 1 || got[0] != [2]string{"gitlab.corp/pay/svc", "PAY-1"} {
		t.Fatalf("%v", got)
	}
	// lô sai thì không ghi gì
	bad := f.post("/api/ingest/events", strings.Replace(batch, "FILE_EDIT", "NOPE", 1), f.token)
	if bad.Status != 400 || bad.JSON(t)["error"] != "type không hợp lệ: NOPE" {
		t.Fatalf("%d %s", bad.Status, bad.Body)
	}
	if n := f.count(t, "SELECT count(*) FROM events"); n != 3 {
		t.Fatal(n)
	}
}

func TestEventsValidation(t *testing.T) {
	f := newFixture(t)
	ev := `{"type":"COMMAND","at":"2026-11-03T10:00:00Z"}`
	cases := []struct{ body, msg string }{
		{`{}`, "Thiếu repoId"},
		{``, "Thiếu repoId"},
		{`{"repoId":"r"}`, "Thiếu events"},
		{`{"repoId":"r","events":{}}`, "Thiếu events"},
		{`{"repoId":"r","events":[],"version":3}`, "version phải là 1 hoặc 2"},
		{`{"repoId":"r","events":[],"version":"1"}`, "version phải là 1 hoặc 2"},
		{`{"repoId":"r","events":[{"type":"COMMAND"}]}`, "Thiếu events[].at"},
		{`{"repoId":"r","events":[{"type":"COMMAND","at":"zz"}]}`, "Thời điểm không hợp lệ: zz"},
		{`{"repoId":"r","events":[{"type":"COMMAND","at":"2026-11-03T10:00:00Z","sessionId":5}]}`, "sessionId phải là chuỗi tối đa 100 ký tự"},
		{`{"repoId":"r","events":[{"type":"COMMAND","at":"2026-11-03T10:00:00Z","sessionId":"` + strings.Repeat("s", 101) + `"}]}`, "sessionId phải là chuỗi tối đa 100 ký tự"},
		{`{"repoId":"r","events":[{}]}`, "type không hợp lệ: "},
	}
	for _, c := range cases {
		r := f.post("/api/ingest/events", c.body, f.token)
		if r.Status != 400 || r.JSON(t)["error"] != c.msg {
			t.Errorf("%.60q: %d %s", c.body, r.Status, r.Body)
		}
	}
	for _, ok := range []string{`{"repoId":"r","events":[],"version":1}`, `{"repoId":"r","events":[],"version":2}`, `{"repoId":"r","events":[],"version":null}`,
		`{"repoId":"r","events":[` + ev + `],"version":2}`, `{"repoId":"r","events":[{"type":"COMMAND","at":"2026-11-03T10:00:00Z","sessionId":"` + strings.Repeat("s", 100) + `"}]}`} {
		if r := f.post("/api/ingest/events", ok, f.token); r.Status != 200 {
			t.Errorf("%.60q: %d %s", ok, r.Status, r.Body)
		}
	}
	if n := f.count(t, "SELECT count(*) FROM events WHERE session_id IS NOT NULL"); n != 1 {
		t.Fatal(n)
	}
}

func (f *fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.e.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) str(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var s *string
	if err := f.e.Pool.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		t.Fatal(err)
	}
	if s == nil {
		return "<null>"
	}
	return *s
}

func evJSON(typ, detail string) string {
	return fmt.Sprintf(`{"repoId":"gitlab.corp/pay/svc","events":[{"type":%q,"at":"2026-11-03T12:00:00Z","feature":"PAY-1","detail":%q}]}`, typ, detail)
}

func TestControlEffects(t *testing.T) {
	f := newFixture(t)
	repo := "gitlab.corp/pay/svc"
	f.exec(t, "INSERT INTO repos(id) VALUES ($1)", repo)
	f.exec(t, "INSERT INTO instructions(repo_id, feature_key, by_login, text, source) VALUES ($1,'PAY-1','x','hi','web')", repo)
	f.exec(t, "INSERT INTO pending_actions(repo_id, feature_key, kind, step, created_by, expires_at) VALUES ($1,'PAY-1','ACCEPT_STEP','S1','x', now() + interval '1 day')", repo)

	if r := f.post("/api/ingest/events", evJSON("INSTRUCTION_DELIVERED", " 1 "), f.token); r.Status != 200 {
		t.Fatal(r.Body)
	}
	if got := f.str(t, "SELECT delivered_at::text FROM instructions"); got == "<null>" {
		t.Fatal("chưa đánh dấu đã giao")
	}
	f.post("/api/ingest/events", evJSON("INSTRUCTION_DELIVERED", "abc"), f.token) // hỏng: bỏ qua, vẫn 200
	// STALE rồi APPLIED ghi đè; STALE không ghi đè APPLIED
	f.post("/api/ingest/events", evJSON("ACTION_RESULT", `{"id":1,"status":"STALE","reason":"r1"}`), f.token)
	if got := f.str(t, "SELECT status FROM pending_actions"); got != "STALE" {
		t.Fatal(got)
	}
	f.post("/api/ingest/events", evJSON("ACTION_RESULT", `{"id":1,"status":"APPLIED"}`), f.token)
	f.post("/api/ingest/events", evJSON("ACTION_RESULT", `{"id":1,"status":"STALE"}`), f.token)
	if got := f.str(t, "SELECT status FROM pending_actions"); got != "APPLIED" {
		t.Fatal(got)
	}
	f.post("/api/ingest/events", evJSON("ACTION_RESULT", `not json`), f.token)
	if n := f.count(t, "SELECT count(*) FROM events"); n != 6 {
		t.Fatal(n)
	}
}

func TestRiskEventsApplyAndReplay(t *testing.T) {
	f := newFixture(t)
	// sự kiện rủi ro đến trước snapshot đầu: chưa có hàng features, rồi snapshot đầu phát lại theo thứ tự
	f.post("/api/ingest/events", evJSON("RISK_CLASSIFIED", `{"level":"HIGH","policyVersion":3}`), f.token)
	f.post("/api/ingest/events", `{"repoId":"gitlab.corp/pay/svc","events":[{"type":"RISK_OVERRIDDEN","at":"2026-11-03T12:30:00Z","feature":"PAY-1","detail":"{\"to\":\"LOW\"}"}]}`, f.token)
	if r := f.post("/api/ingest/snapshot", snap(t1, "A"), f.token); r.Status != 200 {
		t.Fatal(r.Body)
	}
	if got := f.str(t, "SELECT risk_level FROM features"); got != "LOW" {
		t.Fatal(got)
	}
	if got := f.str(t, "SELECT risk_policy_version::text FROM features"); got != "3" {
		t.Fatal(got)
	}
	// hàng đã có: áp trực tiếp; policyVersion không hợp lệ → NULL
	f.post("/api/ingest/events", evJSON("RISK_CLASSIFIED", `{"level":"MEDIUM","policyVersion":0}`), f.token)
	if got := f.str(t, "SELECT risk_level || '/' || coalesce(risk_policy_version::text,'null') FROM features"); got != "MEDIUM/null" {
		t.Fatal(got)
	}
	f.post("/api/ingest/events", evJSON("RISK_CLASSIFIED", `{"level":"  "}`), f.token) // bỏ qua
	if got := f.str(t, "SELECT risk_level FROM features"); got != "MEDIUM" {
		t.Fatal(got)
	}
}

func TestPurgeOld(t *testing.T) {
	f := newFixture(t)
	f.exec(t, "INSERT INTO events(repo_id, type, at, sender_login) VALUES ('r','COMMAND', $1, 'x'), ('r','COMMAND', $2, 'x')",
		f.e.Now.AddDate(0, 0, -181), f.e.Now.AddDate(0, 0, -179))
	// PurgeOld là phương thức của ingest.Service
	if n := f.purge(t); n != 1 {
		t.Fatal(n)
	}
	if n := f.count(t, "SELECT count(*) FROM events"); n != 1 {
		t.Fatal(n)
	}
}
