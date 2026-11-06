package board_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/web"
	"aisdlc/internal/web/webtest"
)

// Port BoardIT (9) qua handler đầy đủ (xác thực + ingest + board).

type itFix struct {
	e              *webtest.Env
	w              *webtest.Client
	tokenA, tokenB string
}

func newIT(t *testing.T) *itFix {
	t.Helper()
	f := &itFix{}
	f.e = webtest.New(t, func(d web.Deps) http.Handler { return web.NewHandler(d) })
	f.w = f.e.Client(t)
	f.tokenA = webtest.CliToken(t, f.e.Pool, "add", "board.a", "board.a", "--email", "a@corp")
	f.tokenB = webtest.CliToken(t, f.e.Pool, "add", "board.b", "board.b", "--email", "b@corp")
	return f
}

func (f *itFix) post(t *testing.T, path, body, bearer string) webtest.Resp {
	t.Helper()
	return f.w.Do("POST", path, body, map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + bearer})
}

func (f *itFix) get(path, bearer string) webtest.Resp { return f.w.GetBearer(path, bearer) }

func (f *itFix) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.e.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *itFix) now() time.Time { return *f.e.Now }

func decode(t *testing.T, r webtest.Resp) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(r.Body), &v); err != nil {
		t.Fatalf("%d %v: %s", r.Status, err, r.Body)
	}
	return v
}

// at lấy giá trị theo đường dẫn khóa/chỉ số.
func at(v any, keys ...any) any {
	for _, k := range keys {
		switch k := k.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			a, _ := v.([]any)
			if k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

func size(v any) int { a, _ := v.([]any); return len(a) }

func snap(repoID, key, owner string) string {
	o := "null"
	if owner != "" {
		o = `"` + owner + `"`
	}
	return fmt.Sprintf(`{"schemaVersion":1,"repo":{"id":"%s","branch":"feature/%s"},"clientId":"A",
 "capturedAt":"2026-11-05T09:00:00Z","sender":"x","feature":{"key":"%s","title":"Tiêu đề","flow":"FULL","owner":%s},
 "gates":[{"gate":"BRD","state":"AWAITING_APPROVAL"}],"steps":[],"cases":[],"openQuestions":[],
 "artifacts":{},"eval":{"passed":true,"hard":[{"id":"CASES_ALL_PASS","passed":true,"detail":"6/6 case pass"}]},
 "review":null,"truncated":false}`, repoID, key, key, o)
}

func TestBoardShowsCardAndInboxForOwner(t *testing.T) {
	f := newIT(t)
	if r := f.post(t, "/api/ingest/snapshot", snap("gitlab.corp/pay/svc", "PAY-1", "a@corp"), f.tokenA); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	a := decode(t, f.get("/api/board", f.tokenA))
	card := at(a, "cards", 0)
	if at(a, "me") != "board.a" || size(at(a, "cards")) != 1 || at(card, "owner") != "board.a" || at(card, "column") != "BRD" ||
		at(card, "session") != "NONE" || at(card, "eval", "casesTotal") != 6.0 {
		t.Fatal(a)
	}
	if size(at(a, "inbox")) != 1 || at(a, "inbox", 0, "command") != "aisdlc approve brd" {
		t.Fatal(a)
	}
	b := decode(t, f.get("/api/board", f.tokenB))
	if size(at(b, "cards")) != 1 || size(at(b, "inbox")) != 0 || at(b, "inbox") == nil {
		t.Fatal(b)
	}
	if n := size(at(decode(t, f.get("/api/board?owner=board.b", f.tokenA)), "cards")); n != 0 {
		t.Fatal(n)
	}
	if n := size(at(decode(t, f.get("/api/board?repo=khac", f.tokenA)), "cards")); n != 0 {
		t.Fatal(n)
	}
	if r := f.get("/api/board", "nope"); r.Status != 401 {
		t.Fatal(r.Status)
	}
}

func TestWebModeRepoUsesServerGateStateForInboxColumnAndDetail(t *testing.T) {
	f := newIT(t)
	repo := "r/board-web"
	s := strings.Replace(snap(repo, "WEB-1", "a@corp"), `[{"gate":"BRD","state":"AWAITING_APPROVAL"}]`,
		`[{"gate":"BRD","state":"NOT_SUBMITTED","currentHash":"hB"},`+
			`{"gate":"DESIGN","state":"LOCKED_BY_PREVIOUS","currentHash":"hD"},`+
			`{"gate":"CASES","state":"LOCKED_BY_PREVIOUS","currentHash":"hC"},`+
			`{"gate":"REVIEW","state":"LOCKED_BY_PREVIOUS","currentHash":"hR"}]`, 1)
	if r := f.post(t, "/api/ingest/snapshot", s, f.tokenA); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	f.exec(t, "INSERT INTO repo_settings(repo_id, approvals, set_by) VALUES ($1, 'web', 'board.a') ON CONFLICT DO NOTHING", repo)
	var brd int64
	if err := f.e.Pool.QueryRow(context.Background(), "INSERT INTO submissions(repo_id, feature_key, gate, hash, findings, artifacts, "+
		"submitted_by) VALUES ($1, 'WEB-1', 'BRD', 'hB', '[]', '[]', 'board.a') RETURNING id", repo).Scan(&brd); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "INSERT INTO approvals(submission_id, repo_id, feature_key, gate, hash, approver, channel) "+
		"VALUES ($1, $2, 'WEB-1', 'BRD', 'hB', 'board.a', 'web')", brd, repo)
	f.exec(t, "INSERT INTO submissions(repo_id, feature_key, gate, hash, findings, artifacts, submitted_by) "+
		"VALUES ($1, 'WEB-1', 'DESIGN', 'hD', '[]', '[]', 'board.a')", repo)

	a := decode(t, f.get("/api/board?repo="+repo, f.tokenA))
	card := at(a, "cards", 0)
	if at(card, "column") != "DESIGN" || !strings.Contains(fmt.Sprint(at(card, "labels")), "AWAITING_APPROVAL:DESIGN") {
		t.Fatal(card)
	}
	if size(at(a, "inbox")) != 1 || at(a, "inbox", 0, "kind") != "APPROVE" || at(a, "inbox", 0, "command") != "aisdlc approve design" {
		t.Fatal(at(a, "inbox"))
	}
	d := decode(t, f.get("/api/features/"+repo+"/WEB-1", f.tokenA))
	if at(d, "approvalsMode") != "web" || at(d, "column") != "DESIGN" ||
		at(d, "snapshot", "gates", 0, "state") != "APPROVED" || at(d, "snapshot", "gates", 1, "state") != "AWAITING_APPROVAL" {
		t.Fatal(d)
	}
	if at(d, "snapshot", "gates", 0, "approval", "approver") != "board.a" {
		t.Fatal(at(d, "snapshot", "gates", 0))
	}

	// repo local: giữ nguyên trạng thái của snapshot
	f.post(t, "/api/ingest/snapshot", snap("r/board-local", "LOC-1", "a@corp"), f.tokenA)
	l := decode(t, f.get("/api/features/r/board-local/LOC-1", f.tokenA))
	if at(l, "approvalsMode") != "local" || at(l, "snapshot", "gates", 0, "state") != "AWAITING_APPROVAL" {
		t.Fatal(l)
	}
	if fmt.Sprint(at(l, "submittedPaths")) != "map[]" || at(l, "pendingImports") == nil || size(at(l, "pendingImports")) != 0 {
		t.Fatal(l)
	}
}

func TestOwnerFallsBackToFirstSenderAndSessionStatus(t *testing.T) {
	f := newIT(t)
	f.post(t, "/api/ingest/snapshot", snap("r/x", "K-1", ""), f.tokenB)
	now := f.now().Format(time.RFC3339)
	if r := f.post(t, "/api/ingest/events", fmt.Sprintf(`{"repoId":"r/x","clientId":"A","events":[{"type":"SESSION_START","at":"%s","feature":"K-1","sender":"b"}]}`, now), f.tokenB); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	card := at(decode(t, f.get("/api/board", f.tokenB)), "cards", 0)
	if at(card, "owner") != "board.b" || at(card, "session") != "ACTIVE" {
		t.Fatal(card)
	}
	later := f.now().Add(time.Second).Format(time.RFC3339)
	f.post(t, "/api/ingest/events", fmt.Sprintf(`{"repoId":"r/x","clientId":"A","events":[{"type":"SESSION_STOP","at":"%s","feature":"K-1","sender":"b"}]}`, later), f.tokenB)
	card = at(decode(t, f.get("/api/board", f.tokenB)), "cards", 0)
	if at(card, "session") != "STOPPED" {
		t.Fatal(card)
	}
}

func doneSnap(repo, key, reviewExtra string) string {
	return strings.Replace(strings.Replace(snap(repo, key, "a@corp"), `"state":"AWAITING_APPROVAL"`, `"state":"APPROVED"`, 1),
		`"gates":[`, `"gates":[{"gate":"DESIGN","state":"APPROVED"},{"gate":"CASES","state":"APPROVED"},{"gate":"REVIEW","state":"APPROVED"`+reviewExtra+`},`, 1)
}

func TestOldDoneCardsAreHidden(t *testing.T) {
	f := newIT(t)
	f.post(t, "/api/ingest/snapshot", doneSnap("r/x", "OLD-1", ""), f.tokenA)
	if n := size(at(decode(t, f.get("/api/board", f.tokenA)), "cards")); n != 1 {
		t.Fatal(n)
	}
	f.exec(t, "UPDATE features SET synced_at = $1", f.now().Add(-10*24*time.Hour))
	if n := size(at(decode(t, f.get("/api/board", f.tokenA)), "cards")); n != 0 {
		t.Fatal(n)
	}
	if n := size(at(decode(t, f.get("/api/board?hideDoneOlderThanDays=30", f.tokenA)), "cards")); n != 1 {
		t.Fatal(n)
	}
	if n := size(at(decode(t, f.get("/api/board?hideDoneOlderThanDays=0", f.tokenA)), "cards")); n != 1 {
		t.Fatal(n)
	}
	if r := f.get("/api/board?hideDoneOlderThanDays=abc", f.tokenA); r.Status != 400 {
		t.Fatal(r.Status, r.Body)
	}
}

func TestDoneAgeComesFromReviewApprovalNotSyncTime(t *testing.T) {
	f := newIT(t)
	approvedAt := f.now().Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	f.post(t, "/api/ingest/snapshot", doneSnap("r/x", "OLD-2", `,"approval":{"hash":"h","approver":"a","at":"`+approvedAt+`"}`), f.tokenA)
	if n := size(at(decode(t, f.get("/api/board", f.tokenA)), "cards")); n != 0 {
		t.Fatal(n)
	}
	if n := size(at(decode(t, f.get("/api/board?hideDoneOlderThanDays=30", f.tokenA)), "cards")); n != 1 {
		t.Fatal(n)
	}
}

func TestFeatureDetailWithSlashRepoId(t *testing.T) {
	f := newIT(t)
	f.post(t, "/api/ingest/snapshot", snap("gitlab.corp/pay/payment-svc", "PAY-1", "a@corp"), f.tokenA)
	if r := f.post(t, "/api/ingest/events", `{"repoId":"gitlab.corp/pay/payment-svc","clientId":"A","events":[
	 {"type":"COMMAND","at":"2026-11-03T10:00:00Z","feature":"PAY-1","sender":"a","detail":"step finish S1"},
	 {"type":"FILE_EDIT","at":"2026-11-03T11:00:00Z","feature":"PAY-1","step":"S1","path":"a.txt","sender":"a"}]}`, f.tokenA); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	r := f.get("/api/features/gitlab.corp/pay/payment-svc/PAY-1", f.tokenA)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	d := decode(t, r)
	if at(d, "snapshot", "feature", "key") != "PAY-1" || at(d, "column") != "BRD" || at(d, "owner") != "board.a" ||
		at(d, "lastSender") != "board.a" || at(d, "labels", 0) != "AWAITING_APPROVAL:BRD" {
		t.Fatal(d)
	}
	ev := decode(t, f.get("/api/features/gitlab.corp/pay/payment-svc/PAY-1/events?limit=1", f.tokenA))
	if size(ev) != 1 || at(ev, 0, "type") != "FILE_EDIT" {
		t.Fatal(ev)
	}
	if n := size(decode(t, f.get("/api/features/gitlab.corp/pay/payment-svc/PAY-1/events", f.tokenA))); n != 2 {
		t.Fatal(n)
	}
	if r := f.get("/api/features/gitlab.corp/pay/payment-svc/NOPE-9", f.tokenA); r.Status != 404 {
		t.Fatal(r.Status)
	}
	if r := f.get("/api/features/PAY-1", f.tokenA); r.Status != 404 {
		t.Fatal(r.Status)
	}
	if r := f.get("/api/features/gitlab.corp/pay/payment-svc/PAY-1/", f.tokenA); r.Status != 404 {
		t.Fatal(r.Status)
	}
}

// sse mở stream (Bearer) và trả kênh các dòng; đóng body khi test xong.
func sse(t *testing.T, base, path, bearer string) (<-chan string, func()) {
	t.Helper()
	req, _ := http.NewRequest("GET", base+path, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "text/event-stream")
	ctx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(ctx)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("stream %s: %d", path, res.StatusCode)
	}
	lines := make(chan string, 1000)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	stop := func() { cancel(); res.Body.Close() }
	t.Cleanup(stop)
	return lines, stop
}

// readUntil đọc dòng tới khi có dòng bắt đầu bằng prefix (tối đa 2 giây, như Java).
func readUntil(t *testing.T, lines <-chan string, prefix string) []string {
	t.Helper()
	var got []string
	deadline := time.After(2 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("stream đóng trước %q: %q", prefix, got)
			}
			got = append(got, l)
			if strings.HasPrefix(l, prefix) {
				return got
			}
		case <-deadline:
			t.Fatalf("hết giờ chờ %q: %q", prefix, got)
		}
	}
}

func firstData(lines []string) string {
	for _, l := range lines {
		if strings.HasPrefix(l, "data:") {
			return l
		}
	}
	return ""
}

func contains(lines []string, s string) bool {
	for _, l := range lines {
		if l == s {
			return true
		}
	}
	return false
}

func TestFeatureStreamPushesOnIngest(t *testing.T) {
	f := newIT(t)
	f.post(t, "/api/ingest/snapshot", snap("gitlab.corp/pay/payment-svc", "PAY-1", "a@corp"), f.tokenA)
	lines, _ := sse(t, f.e.Server.URL, "/api/features/gitlab.corp/pay/payment-svc/PAY-1/stream", f.tokenA)
	readUntil(t, lines, ":ok")
	f.post(t, "/api/ingest/events", `{"repoId":"gitlab.corp/pay/payment-svc","clientId":"A","events":[
	 {"type":"FILE_EDIT","at":"2026-11-03T11:00:00Z","feature":"PAY-1","path":"a.txt","sender":"a"}]}`, f.tokenA)
	got := readUntil(t, lines, `data:{"id"`)
	if !contains(got, "event:event") || !strings.Contains(firstData(got), "a.txt") {
		t.Fatal(got)
	}
	if r := f.get("/api/features/gitlab.corp/pay/payment-svc/NOPE/stream", f.tokenA); r.Status != 404 {
		t.Fatal(r.Status)
	}
}

func TestBoardStreamPushesCardOnSnapshot(t *testing.T) {
	f := newIT(t)
	lines, _ := sse(t, f.e.Server.URL, "/api/stream", f.tokenA)
	readUntil(t, lines, ":ok")
	f.post(t, "/api/ingest/snapshot", snap("r/x", "K-2", "a@corp"), f.tokenA)
	got := readUntil(t, lines, "data:")
	if !contains(got, "event:card") || !strings.Contains(firstData(got), `"repoId":"r/x"`) || !strings.Contains(firstData(got), `"key":"K-2"`) {
		t.Fatal(got)
	}
}

func TestIngestStillSucceedsWhenSubscriberSendFails(t *testing.T) {
	f := newIT(t)
	// subscriber đã đi: ingest không bị ảnh hưởng, hub vẫn phục vụ subscriber khác
	dead, stop := sse(t, f.e.Server.URL, "/api/stream", f.tokenA)
	readUntil(t, dead, ":ok")
	stop()
	r := f.post(t, "/api/ingest/snapshot", snap("r/x", "K-3", "a@corp"), f.tokenA)
	if r.Status != 200 || !strings.Contains(r.Body, `"accepted":true`) {
		t.Fatal(r.Status, r.Body)
	}
	lines, _ := sse(t, f.e.Server.URL, "/api/stream", f.tokenA)
	readUntil(t, lines, ":ok")
	f.post(t, "/api/ingest/snapshot", snap("r/x", "K-4", "a@corp"), f.tokenA)
	if got := readUntil(t, lines, "data:"); !contains(got, "event:card") {
		t.Fatal(got)
	}
}
