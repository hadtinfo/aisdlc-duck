package board_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"aisdlc/internal/web"
	"aisdlc/internal/web/board"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/webtest"
)

// hubFix: board.Routes với xác thực giả (header X-Login) trên DB thật, để test truy cập Hub trực tiếp.
type hubFix struct {
	e      *webtest.Env
	hub    *board.Hub
	reader *board.Reader
}

func newHubFix(t *testing.T, gates board.GateView, heartbeat time.Duration) *hubFix {
	t.Helper()
	f := &hubFix{}
	f.e = webtest.New(t, func(d web.Deps) http.Handler {
		f.reader = &board.Reader{Pool: d.Pool, Now: d.Now, Gates: gates}
		f.hub = board.NewHub(f.reader)
		f.hub.Heartbeat = heartbeat
		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p := httpx.Principal{Login: r.Header.Get("X-Login"), Role: "dev", Via: "session"}
				next.ServeHTTP(w, r.WithContext(httpx.WithPrincipal(r.Context(), p)))
			})
		})
		board.Routes(r, f.reader, f.hub)
		return r
	})
	t.Cleanup(f.hub.Close) // trước khi server đóng (LIFO)
	return f
}

func (f *hubFix) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.e.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *hubFix) feature(t *testing.T, repo, key, snapshot string) {
	t.Helper()
	f.exec(t, `INSERT INTO features(repo_id, feature_key, snapshot, captured_at, client_id, first_sender_login, last_sender_login, synced_at)
	 VALUES ($1,$2,$3::jsonb,$4,'A','board.a','board.a',$4)
	 ON CONFLICT (repo_id, feature_key) DO UPDATE SET snapshot = excluded.snapshot, synced_at = excluded.synced_at`,
		repo, key, snapshot, *f.e.Now)
}

func (f *hubFix) event(t *testing.T, repo, key, typ, p string) {
	t.Helper()
	f.exec(t, "INSERT INTO events(repo_id, feature_key, type, at, path, sender_login) VALUES ($1,$2,$3,$4,$5,'board.a')",
		repo, key, typ, *f.e.Now, p)
}

// rawStream mở stream và trả reader thô (để kiểm từng byte).
func rawStream(t *testing.T, base, path string) (*bufio.Reader, *http.Response, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", base+path, nil)
	req.Header.Set("X-Login", "board.a")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	stop := func() { cancel(); res.Body.Close() }
	t.Cleanup(stop)
	return bufio.NewReader(res.Body), res, stop
}

// frame đọc một khung SSE (tới dòng trống), có hạn giờ.
func frame(t *testing.T, br *bufio.Reader) string {
	t.Helper()
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		var b strings.Builder
		for {
			l, err := br.ReadString('\n')
			b.WriteString(l)
			if err != nil || l == "\n" {
				ch <- res{b.String(), err}
				return
			}
		}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("đọc khung: %v (%q)", r.err, r.s)
		}
		return r.s
	case <-time.After(3 * time.Second):
		t.Fatal("hết giờ chờ khung SSE")
	}
	return ""
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("hết giờ chờ: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

const featSnap = `{"repo":{"id":"r/x","branch":"b"},"feature":{"key":"K-1","title":"T","flow":"FULL","owner":"a@corp"},
 "gates":[{"gate":"BRD","state":"AWAITING_APPROVAL"}],"steps":[],"eval":null}`

// TestSseEventFormat: khung như SseEmitter của Spring: ":ok" khi mở, "event:card"/"event:event"/"event:snapshot"
// với dòng "data:", comment ":hb" theo nhịp heartbeat (Java 20 giây, ở đây tiêm 100 ms).
func TestSseEventFormat(t *testing.T) {
	f := newHubFix(t, nil, 100*time.Millisecond)
	f.feature(t, "r/x", "K-1", featSnap)

	br, res, _ := rawStream(t, f.e.Server.URL, "/api/stream")
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatal(ct)
	}
	if fr := frame(t, br); fr != ":ok\n\n" {
		t.Fatalf("%q", fr)
	}
	start := time.Now()
	if fr := frame(t, br); fr != ":hb\n\n" {
		t.Fatalf("%q", fr)
	}
	if el := time.Since(start); el < 50*time.Millisecond || el > time.Second {
		t.Fatalf("nhịp heartbeat %v", el)
	}
	f.hub.Publish("r/x", "K-1")
	for {
		fr := frame(t, br)
		if fr == ":hb\n\n" {
			continue
		}
		if fr != "event:card\ndata:{\"repoId\":\"r/x\",\"key\":\"K-1\"}\n\n" {
			t.Fatalf("%q", fr)
		}
		break
	}

	fb, _, _ := rawStream(t, f.e.Server.URL, "/api/features/r/x/K-1/stream")
	if fr := frame(t, fb); fr != ":ok\n\n" {
		t.Fatalf("%q", fr)
	}
	f.event(t, "r/x", "K-1", "FILE_EDIT", "a.txt")
	f.exec(t, "UPDATE features SET synced_at = synced_at + interval '1 second'")
	f.hub.Publish("r/x", "K-1")
	var frames []string
	for len(frames) < 2 {
		if fr := frame(t, fb); fr != ":hb\n\n" {
			frames = append(frames, fr)
		}
	}
	var id int64
	if err := f.e.Pool.QueryRow(context.Background(), "SELECT max(id) FROM events").Scan(&id); err != nil {
		t.Fatal(err)
	}
	wantEvent := "event:event\ndata:{\"id\":" + itoa(id) + ",\"type\":\"FILE_EDIT\",\"at\":\"2026-11-05T10:00:00Z\",\"step\":null," +
		"\"path\":\"a.txt\",\"outOfScope\":false,\"sender\":\"board.a\",\"detail\":null}\n\n"
	if frames[0] != wantEvent || frames[1] != "event:snapshot\ndata:\n\n" {
		t.Fatalf("%q\nmuốn %q", frames, wantEvent)
	}
	// không có gì mới: Publish không gửi lại event cũ hay snapshot
	f.hub.Publish("r/x", "K-1")
	f.event(t, "r/x", "K-1", "COMMAND", "")
	f.hub.Publish("r/x", "K-1")
	for {
		fr := frame(t, fb)
		if fr == ":hb\n\n" {
			continue
		}
		if !strings.HasPrefix(fr, "event:event\ndata:{\"id\":"+itoa(id+1)+",\"type\":\"COMMAND\"") {
			t.Fatalf("%q", fr)
		}
		break
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// TestSseClientDisconnectReleases: nhiều client (bảng và tính năng) ngắt thì hub gỡ hết và goroutine trở về mức cũ.
func TestSseClientDisconnectReleases(t *testing.T) {
	f := newHubFix(t, nil, time.Hour)
	f.feature(t, "r/x", "K-1", featSnap)
	base := runtime.NumGoroutine()
	var stops []func()
	for i := 0; i < 5; i++ {
		br, _, stop := rawStream(t, f.e.Server.URL, "/api/stream")
		frame(t, br)
		stops = append(stops, stop)
		fb, _, stop2 := rawStream(t, f.e.Server.URL, "/api/features/r/x/K-1/stream")
		frame(t, fb)
		stops = append(stops, stop2)
	}
	if n := f.hub.Clients(); n != 10 {
		t.Fatal(n)
	}
	for _, s := range stops {
		s()
	}
	waitFor(t, "client về 0", func() bool { return f.hub.Clients() == 0 })
	http.DefaultClient.CloseIdleConnections()
	waitFor(t, "goroutine trở về mức cũ", func() bool { return runtime.NumGoroutine() <= base+3 })
	f.hub.Publish("r/x", "K-1") // không còn ai: không treo
}

// failWriter: ResponseWriter mà mọi lần ghi sau lần thứ ok đều lỗi.
type failWriter struct {
	mu     sync.Mutex
	h      http.Header
	ok     int
	writes int
	fails  int
}

func (w *failWriter) Header() http.Header { return w.h }
func (w *failWriter) WriteHeader(int)     {}
func (w *failWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	if w.writes > w.ok {
		w.fails++
		return 0, errors.New("broken pipe")
	}
	return len(p), nil
}
func (w *failWriter) Flush() {}

// TestSseTwoWriteFailuresDropClient: hai lần ghi liên tiếp lỗi thì gỡ client (handler trả về).
func TestSseTwoWriteFailuresDropClient(t *testing.T) {
	hub := board.NewHub(nil)
	hub.Heartbeat = 20 * time.Millisecond
	defer hub.Close()
	w := &failWriter{h: http.Header{}, ok: 1}
	done := make(chan struct{})
	go func() {
		hub.ServeBoard(w, httptest.NewRequest("GET", "/api/stream", nil))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client không bị gỡ")
	}
	if w.fails != 2 || hub.Clients() != 0 {
		t.Fatal(w.fails, hub.Clients())
	}
}

// TestSseCloseEndsStreams: Hub.Close kết thúc mọi stream đang mở và stream mới.
func TestSseCloseEndsStreams(t *testing.T) {
	f := newHubFix(t, nil, time.Hour)
	f.feature(t, "r/x", "K-1", featSnap)
	br, _, _ := rawStream(t, f.e.Server.URL, "/api/stream")
	frame(t, br)
	fb, _, _ := rawStream(t, f.e.Server.URL, "/api/features/r/x/K-1/stream")
	frame(t, fb)
	f.hub.Close()
	for _, r := range []*bufio.Reader{br, fb} {
		done := make(chan error, 1)
		go func() { _, err := io.ReadAll(r); done <- err }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("stream không đóng sau Close")
		}
	}
	if f.hub.Clients() != 0 {
		t.Fatal(f.hub.Clients())
	}
	f.hub.Publish("r/x", "K-1")
	r, err := http.Get(f.e.Server.URL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 503 {
		t.Fatal(r.StatusCode)
	}
}

// fakeGates phủ trạng thái cổng như Task 6 sẽ làm (BRD đã duyệt trên server, DESIGN chờ duyệt).
type fakeGates struct{}

func (fakeGates) Overlay(_ context.Context, _, _ string, snap json.RawMessage, _ time.Time) (json.RawMessage, error) {
	var m map[string]any
	if err := json.Unmarshal(snap, &m); err != nil {
		return nil, err
	}
	m["gates"] = []any{map[string]any{"gate": "BRD", "state": "APPROVED"}, map[string]any{"gate": "DESIGN", "state": "AWAITING_APPROVAL"}}
	return json.Marshal(m)
}
func (fakeGates) SubmittedPaths(context.Context, string, string) (map[string][]string, error) {
	return map[string][]string{"BRD": {"docs/brd.md"}}, nil
}
func (fakeGates) PendingImports(context.Context, string, string) ([]board.PendingImport, error) {
	return []board.PendingImport{{Gate: "BRD", Hash: "h"}}, nil
}
func (fakeGates) Resubmission(_ context.Context, _, _, gate string) (any, error) {
	return map[string]any{"n": 2, "gate": gate}, nil
}

// TestWebModeOverlayApplied: repo chế độ web thì cột, nhãn, hộp thư và chi tiết tính trên snapshot đã phủ (GateView).
func TestWebModeOverlayApplied(t *testing.T) {
	f := newHubFix(t, fakeGates{}, time.Hour)
	f.exec(t, "INSERT INTO users(login, display_name, email) VALUES ('board.a','a','a@corp')")
	f.feature(t, "r/board-web", "WEB-1", featSnap)
	f.feature(t, "r/board-local", "LOC-1", featSnap)
	f.exec(t, "INSERT INTO repo_settings(repo_id, approvals, set_by) VALUES ('r/board-web', 'web', 'board.a')")
	get := func(p string) any {
		req, _ := http.NewRequest("GET", f.e.Server.URL+p, nil)
		req.Header.Set("X-Login", "board.a")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v any
		if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
			t.Fatal(res.StatusCode, err)
		}
		return v
	}
	a := get("/api/board?repo=r/board-web")
	card := at(a, "cards", 0)
	if at(card, "column") != "DESIGN" || !strings.Contains(fmtJSON(at(card, "labels")), "AWAITING_APPROVAL:DESIGN") ||
		at(card, "approvalsMode") != "web" {
		t.Fatal(a)
	}
	if size(at(a, "inbox")) != 1 || at(a, "inbox", 0, "kind") != "APPROVE" || at(a, "inbox", 0, "command") != "aisdlc approve design" {
		t.Fatal(a)
	}
	d := get("/api/features/r/board-web/WEB-1")
	if at(d, "approvalsMode") != "web" || at(d, "column") != "DESIGN" || at(d, "snapshot", "gates", 0, "state") != "APPROVED" ||
		at(d, "snapshot", "gates", 1, "state") != "AWAITING_APPROVAL" || at(d, "submittedPaths", "BRD", 0) != "docs/brd.md" ||
		at(d, "pendingImports", 0, "gate") != "BRD" {
		t.Fatal(d)
	}
	if r := get("/api/features/r/board-web/WEB-1/resubmission?gate=DESIGN"); at(r, "n") != 2.0 {
		t.Fatal(r)
	}
	if r := get("/api/features/r/board-local/LOC-1/resubmission?gate=DESIGN"); fmtJSON(r) != `{"n":0}` {
		t.Fatal(r)
	}
	if r := get("/api/features/r/board-local/LOC-1/resubmission?gate=X"); fmtJSON(r) != `{"error":"Cổng không hợp lệ"}` {
		t.Fatal(r)
	}
	l := get("/api/features/r/board-local/LOC-1")
	if at(l, "approvalsMode") != "local" || at(l, "column") != "BRD" || fmtJSON(at(l, "submittedPaths")) != "{}" ||
		fmtJSON(at(l, "pendingImports")) != "[]" {
		t.Fatal(l)
	}
}

func fmtJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func keys(v any) []string {
	m, _ := v.(map[string]any)
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestBoardJSONMatchesUIFixture: snapshot PAY-1 mang reviewCards của fixture UI (pay1-cards.json, sinh từ CLI thật) đi qua
// API chi tiết nguyên vẹn; thẻ bảng và chi tiết có đúng các trường mà UI (api.ts CardData/BoardData, types.ts FeatureDetail) đọc.
func TestBoardJSONMatchesUIFixture(t *testing.T) {
	f := newIT(t)
	cards, err := os.ReadFile("testdata/pay1-cards.json")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Replace(snap("gitlab.corp/pay/payment-svc", "PAY-1", "a@corp"), `"truncated":false`, `"truncated":false,"reviewCards":`+string(cards), 1)
	if r := f.post(t, "/api/ingest/snapshot", s, f.tokenA); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	var want any
	if err := json.Unmarshal(cards, &want); err != nil {
		t.Fatal(err)
	}
	d := decode(t, f.get("/api/features/gitlab.corp/pay/payment-svc/PAY-1", f.tokenA))
	if !reflect.DeepEqual(at(d, "snapshot", "reviewCards"), want) {
		t.Fatalf("reviewCards khác fixture")
	}
	if got := keys(d); !reflect.DeepEqual(got, []string{"approvalsMode", "attempt", "clockSkew", "column", "labels", "lastSender", "owner",
		"pendingImports", "snapshot", "submittedPaths", "syncedAt"}) {
		t.Fatal(got)
	}
	b := decode(t, f.get("/api/board", f.tokenA))
	if got := keys(b); !reflect.DeepEqual(got, []string{"cards", "inbox", "me"}) {
		t.Fatal(got)
	}
	card := at(b, "cards", 0)
	if got := keys(card); !reflect.DeepEqual(got, []string{"approvalsMode", "attempt", "branch", "clockSkew", "column", "eval", "flow", "key",
		"labels", "owner", "repoId", "riskLevel", "session", "syncedAt", "title"}) {
		t.Fatal(got)
	}
	if got := keys(at(card, "eval")); !reflect.DeepEqual(got, []string{"casesPassed", "casesTotal", "passed"}) {
		t.Fatal(got)
	}
	if at(card, "syncedAt") != "2026-11-05T10:00:00Z" || at(card, "branch") != "feature/PAY-1" || at(card, "riskLevel") != nil {
		t.Fatal(card)
	}
	if got := keys(at(b, "inbox", 0)); !reflect.DeepEqual(got, []string{"command", "key", "kind", "repoId", "text"}) {
		t.Fatal(got)
	}
	ev := decode(t, f.get("/api/features/gitlab.corp/pay/payment-svc/PAY-1/events", f.tokenA))
	if size(ev) != 0 || ev == nil {
		t.Fatal(ev)
	}
}
