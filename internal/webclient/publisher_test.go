package webclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"aisdlc/internal/gitx"
	"aisdlc/internal/webclient/fakeweb"
)

// NOW cùng đồng hồ với WebPublisherTest.
var pubNow = time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)

// hermeticGit: git không đọc cấu hình của máy.
func hermeticGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("HOME", t.TempDir())
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

type pubFix struct {
	t       *testing.T
	root    string
	url     atomic.Value // string; "" = web tắt
	err     *syncBuf
	spawned atomic.Int32
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newPubFix(t *testing.T, url string) *pubFix {
	t.Helper()
	hermeticGit(t)
	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "config", "user.name", "Dev Một")
	gitIn(t, root, "config", "user.email", "d@x")
	f := &pubFix{t: t, root: root, err: &syncBuf{}}
	f.url.Store(url)
	return f
}

func (f *pubFix) pub() *Publisher {
	p := NewPublisher(f.root, func() (*Client, bool) {
		u := f.url.Load().(string)
		if u == "" {
			return nil, false
		}
		return &Client{BaseURL: u, Token: "t0k"}, true
	}, f.err)
	p.Now = func() time.Time { return pubNow }
	p.Spawn = func() error { f.spawned.Add(1); return nil }
	return p
}

func ev() Event {
	return Event{Type: "FILE_EDIT", At: time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC), Feature: "PAY-1", Step: "S1",
		Path: "a/b.java", Sender: "Dev"}
}

func closedURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr
}

func (f *pubFix) queueEvents(n int) {
	box := NewOutbox(f.root)
	for i := 0; i < n; i++ {
		mustNil(f.t, box.PutEvent([]byte(`{"type":"FILE_EDIT"}`)))
	}
}

func (f *pubFix) size() int { return NewOutbox(f.root).Size() }

// afterCommand như WebPublisher.afterCommand của một lệnh (snapshot giản lược + event COMMAND).
func (f *pubFix) afterCommand(p *Publisher, cmd string) {
	snap := func() ([]byte, error) {
		cid, err := ClientID(f.root)
		if err != nil {
			return nil, err
		}
		return []byte(`{"schemaVersion":1,"feature":{"key":"PAY-1"},"clientId":"` + cid + `"}`), nil
	}
	p.AfterCommand("PAY-1", snap, Event{Type: "COMMAND", At: pubNow, Feature: "PAY-1",
		Sender: Sender(gitx.New(f.root)), Detail: cmd})
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("%v: %s", err, s)
	}
	return m
}

func firstEvent(t *testing.T, batch string) map[string]any {
	t.Helper()
	return decode(t, batch)["events"].([]any)[0].(map[string]any)
}

// batchIsVersion2AndSessionIdOnlyWhenPresent
func TestBatchIsVersion2AndSessionIDOnlyWhenPresent(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, web.URL())
	p := f.pub()
	e1 := Event{Type: "FILE_EDIT", At: pubNow, Feature: "PAY-1", Step: "S1", Path: "a/b.java", Sender: "Dev", SessionID: "s-9"}
	p.Emit(e1)
	p.Emit(ev())
	p.Flush(t.Context())
	batches := web.RequestsFor("POST", "/api/ingest/events")
	if len(batches) != 2 {
		t.Fatal(len(batches))
	}
	for _, b := range batches {
		m := decode(t, b.Body)
		if m["version"] != float64(2) {
			t.Fatal(b.Body)
		}
	}
	if firstEvent(t, batches[0].Body)["sessionId"] != "s-9" {
		t.Fatal(batches[0].Body)
	}
	if _, has := firstEvent(t, batches[1].Body)["sessionId"]; has {
		t.Fatal(batches[1].Body)
	}
	// hợp đồng event như Jackson: đủ trường, null khi thiếu
	want := `{"type":"FILE_EDIT","at":"2026-11-03T01:00:00Z","feature":"PAY-1","step":"S1","path":"a/b.java",` +
		`"outOfScope":false,"sender":"Dev","detail":null}`
	if got := string(EventJSON(ev())); got != want {
		t.Fatalf("\n%s\n%s", got, want)
	}
}

// disabledWritesNothing
func TestDisabledWritesNothing(t *testing.T) {
	f := newPubFix(t, "")
	p := f.pub()
	if p.Enabled() {
		t.Fatal("enabled")
	}
	p.Emit(ev())
	p.Queue(ev())
	f.afterCommand(p, "init")
	p.FlushInline(t.Context(), InlineDeadline)
	if err := p.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.root, ".aisdlc", "web")); !os.IsNotExist(err) {
		t.Fatal(".aisdlc/web đã được tạo")
	}
	if f.spawned.Load() != 0 || f.err.String() != "" {
		t.Fatal(f.spawned.Load(), f.err.String())
	}
}

// serverDownKeepsQueueAndExitCodeUnchanged (phần publisher; mã thoát của lệnh ở Task 3)
func TestServerDownKeepsQueue(t *testing.T) {
	f := newPubFix(t, closedURL(t))
	p := f.pub()
	start := time.Now()
	f.afterCommand(p, "submit brd")
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("chậm: %v", d)
	}
	files := NewOutbox(f.root).List(100)
	snaps := 0
	for _, x := range files {
		if IsSnapshotFile(x) {
			snaps++
		}
	}
	if snaps != 1 || len(files)-snaps != 1 {
		t.Fatal(files)
	}
}

// flushSendsBacklogMax20
func TestFlushSendsBacklogMax20(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, web.URL())
	f.queueEvents(25)
	r := f.pub().Flush(t.Context())
	if r.Sent != 20 || r.Pending != 5 || len(web.Requests()) != 20 {
		t.Fatalf("%+v %d", r, len(web.Requests()))
	}
}

// sendsBearerAndContract (phần publisher; snapshot thật kiểm ở snapshot_test)
func TestSendsBearerAndContract(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, web.URL())
	f.afterCommand(f.pub(), "init")
	f.afterCommand(f.pub(), "submit brd")
	reqs := web.Requests()
	if len(reqs) == 0 {
		t.Fatal("không gửi gì")
	}
	for _, r := range reqs {
		if r.Auth != "Bearer t0k" {
			t.Fatal(r.Auth)
		}
	}
	snaps := web.RequestsFor("POST", "/api/ingest/snapshot")
	events := web.RequestsFor("POST", "/api/ingest/events")
	if len(snaps) != 2 || len(events) != 2 {
		t.Fatal(len(snaps), len(events))
	}
	cid := decode(t, snaps[0].Body)["clientId"].(string)
	if cid == "" || cid != decode(t, snaps[1].Body)["clientId"] {
		t.Fatal(snaps[0].Body, snaps[1].Body)
	}
	e := decode(t, events[1].Body)
	if e["clientId"] != cid || !strings.HasPrefix(e["repoId"].(string), "local/") {
		t.Fatal(events[1].Body)
	}
	ev0 := firstEvent(t, events[1].Body)
	if ev0["type"] != "COMMAND" || ev0["detail"] != "submit brd" || ev0["sender"] != "Dev Một" {
		t.Fatal(events[1].Body)
	}
	if f.size() != 0 {
		t.Fatal(f.size())
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(f.root, ".aisdlc", "web", "last-ok"))); got != "2026-11-02T03:00:00Z" {
		t.Fatal(got)
	}
	if got, ok := LastOK(f.root); !ok || got != "2026-11-02T03:00:00Z" {
		t.Fatal(got, ok)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(f.root, ".aisdlc", "web", "client-id"))); got != cid {
		t.Fatal(got)
	}
}

// rejected400MovesToRejected
func TestRejected400MovesToRejected(t *testing.T) {
	web := fakeweb.New(t)
	web.Update(func(s *fakeweb.Server) {
		s.Override["POST /api/ingest/events"] = fakeweb.JSONResp(400, `{"error":"x"}`)
	})
	f := newPubFix(t, web.URL())
	f.queueEvents(1)
	r := f.pub().Flush(t.Context())
	if r.Sent != 0 || r.Pending != 0 {
		t.Fatalf("%+v", r)
	}
	es, _ := os.ReadDir(filepath.Join(f.root, ".aisdlc", "web", "rejected"))
	if len(es) != 1 {
		t.Fatal(es)
	}
}

// serverErrorKeepsFiles
func TestServerErrorKeepsFiles(t *testing.T) {
	web := fakeweb.New(t)
	web.Update(func(s *fakeweb.Server) { s.Override["POST /api/ingest/events"] = fakeweb.JSONResp(503, `{}`) })
	f := newPubFix(t, web.URL())
	f.queueEvents(1)
	if r := f.pub().Flush(t.Context()); r.Pending != 1 {
		t.Fatalf("%+v", r)
	}
	// 401/408/429 cũng giữ lại
	for _, st := range []int{401, 408, 429} {
		web.Update(func(s *fakeweb.Server) { s.Override["POST /api/ingest/events"] = fakeweb.JSONResp(st, `{}`) })
		if r := f.pub().Flush(t.Context()); r.Pending != 1 {
			t.Fatalf("%d: %+v", st, r)
		}
	}
}

// eventGoesToOutboxAndStdoutStaysClean
func TestEventGoesToOutboxAndOutputStaysClean(t *testing.T) {
	f := newPubFix(t, "http://127.0.0.1:1")
	f.pub().Emit(ev())
	if f.size() != 1 || f.err.String() != "" {
		t.Fatal(f.size(), f.err.String())
	}
}

// publisherErrorsGoToStderrOnly
func TestPublisherErrorsGoToStderrOnly(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, web.URL())
	p := f.pub()
	p.AfterCommand("NOPE-9", func() ([]byte, error) { return nil, errors.New("Không đọc được tính năng NOPE-9") },
		Event{Type: "COMMAND", At: pubNow, Feature: "NOPE-9", Detail: "init"})
	if got := f.err.String(); !strings.HasPrefix(got, "aisdlc web: ") || !strings.Contains(got, "NOPE-9") {
		t.Fatalf("%q", got)
	}
	// panic cũng thành một dòng lỗi
	f.err = &syncBuf{}
	p = f.pub()
	p.AfterCommand("X-1", func() ([]byte, error) { panic("hỏng") }, Event{Type: "COMMAND"})
	if got := f.err.String(); !strings.HasPrefix(got, "aisdlc web: hỏng") {
		t.Fatalf("%q", got)
	}
	// dựng event (sau snapshot) cũng nằm trong guard
	f.err = &syncBuf{}
	p = f.pub()
	p.AfterCommandFunc("X-2", func() ([]byte, error) { return []byte(`{}`), nil },
		func() Event { panic("git hỏng") })
	if got := f.err.String(); !strings.HasPrefix(got, "aisdlc web: git hỏng") {
		t.Fatalf("%q", got)
	}
}

// eventOnlyQueuesThenSpawnsBackgroundFlush
func TestEventOnlyQueuesThenSpawnsBackgroundFlush(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, web.URL())
	f.pub().Emit(ev())
	if len(web.Requests()) != 0 || f.size() != 1 || f.spawned.Load() != 1 {
		t.Fatal(len(web.Requests()), f.size(), f.spawned.Load())
	}
}

// eventDoesNotSpawnWhileFlushRunning
func TestEventDoesNotSpawnWhileFlushRunning(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, web.URL())
	held, ok, err := TryFlushLock(f.root)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	f.pub().Emit(ev())
	if f.spawned.Load() != 0 {
		t.Fatal("spawn khi đang gửi")
	}
	// lệnh CLI cũng không gửi song song (tránh gửi trùng): để lại outbox cho tiến trình đang gửi
	f.afterCommand(f.pub(), "submit brd")
	if len(web.Requests()) != 0 {
		t.Fatal(web.Requests())
	}
	mustNil(t, f.pub().Drain(t.Context()))
	if len(web.Requests()) != 0 {
		t.Fatal(web.Requests())
	}
	held.Close()
	mustNil(t, f.pub().Drain(t.Context()))
	if f.size() != 0 || len(web.Requests()) == 0 {
		t.Fatal(f.size(), len(web.Requests()))
	}
}

// connectionFailureStartsBackoffThatCommandsAndHooksHonour
func TestConnectionFailureStartsBackoffThatCommandsAndHooksHonour(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, closedURL(t))
	f.afterCommand(f.pub(), "submit brd")
	backoff := filepath.Join(f.root, ".aisdlc", "web", "backoff-until")
	if got := strings.TrimSpace(readFile(t, backoff)); got != "2026-11-02T03:01:00Z" {
		t.Fatal(got)
	}
	f.url.Store(web.URL()) // web sống lại nhưng còn trong thời gian chờ
	f.afterCommand(f.pub(), "submit brd")
	f.pub().Emit(ev())
	mustNil(t, f.pub().Drain(t.Context()))
	if len(web.Requests()) != 0 || f.spawned.Load() != 0 {
		t.Fatal(len(web.Requests()), f.spawned.Load())
	}
	if f.size() == 0 {
		t.Fatal("outbox rỗng")
	}
	// web push bỏ qua backoff, gửi hết và xóa mốc chờ
	r := f.pub().Push(t.Context())
	if r.Pending != 0 || f.size() != 0 || f.err.String() != "" {
		t.Fatalf("%+v %d %q", r, f.size(), f.err.String())
	}
	if _, err := os.Stat(backoff); !os.IsNotExist(err) {
		t.Fatal("còn backoff-until")
	}
}

// backoffExpires
func TestBackoffExpires(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, web.URL())
	mustNil(t, os.MkdirAll(filepath.Join(f.root, ".aisdlc", "web"), 0o755))
	mustNil(t, os.WriteFile(BackoffFile(f.root), []byte(javaInstant(pubNow.Add(-time.Second))), 0o644))
	f.pub().Emit(ev())
	if f.spawned.Load() != 1 {
		t.Fatal(f.spawned.Load())
	}
}

// drainSendsWholeOutboxInBatches
func TestDrainSendsWholeOutboxInBatches(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, web.URL())
	f.queueEvents(45)
	mustNil(t, f.pub().Drain(t.Context()))
	if len(web.Requests()) != 45 || f.size() != 0 || f.err.String() != "" {
		t.Fatal(len(web.Requests()), f.size(), f.err.String())
	}
}

// inlineFlushStopsAtOverallDeadlineOnSlowServer
func TestInlineFlushStopsAtOverallDeadlineOnSlowServer(t *testing.T) {
	web := fakeweb.New(t)
	web.Update(func(s *fakeweb.Server) {
		s.Override["POST /api/ingest/events"] = fakeweb.Resp{Status: 200, Delay: 700 * time.Millisecond}
	})
	f := newPubFix(t, web.URL())
	f.queueEvents(10)
	t0 := time.Now()
	f.afterCommand(f.pub(), "submit brd")
	// lệnh chờ gửi tối đa ~2 giây (biên rộng vì máy có thể đang tải)
	if d := time.Since(t0); d > 3500*time.Millisecond {
		t.Fatalf("lệnh chờ %v", d)
	}
	if n := len(web.Requests()); n < 1 || n > 5 {
		t.Fatal(n)
	}
	if f.size() == 0 {
		t.Fatal("outbox rỗng")
	}
	// web chậm nhưng có trả lời: hết hạn của lượt gửi không tính là web hỏng
	if _, err := os.Stat(BackoffFile(f.root)); !os.IsNotExist(err) {
		t.Fatal("có backoff-until")
	}
}

func TestInlineFlushOnHangingServerKeepsDeadline(t *testing.T) {
	web := fakeweb.New(t)
	web.Update(func(s *fakeweb.Server) { s.Hang = true })
	f := newPubFix(t, web.URL())
	f.queueEvents(3)
	t0 := time.Now()
	f.pub().FlushInline(t.Context(), InlineDeadline)
	if d := time.Since(t0); d > 3500*time.Millisecond {
		t.Fatalf("treo %v", d)
	}
	if f.size() != 3 {
		t.Fatal(f.size())
	}
}

// webFlushCommandDrainsSilently (phần publisher; lệnh `web flush` ở Task 3)
func TestDrainIsSilent(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, web.URL())
	f.queueEvents(3)
	mustNil(t, f.pub().Drain(t.Context()))
	if len(web.Requests()) != 3 || f.err.String() != "" {
		t.Fatal(len(web.Requests()), f.err.String())
	}
}

func TestCorruptOutboxFileIsQuarantined(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, web.URL())
	box := NewOutbox(f.root)
	mustNil(t, os.MkdirAll(box.Dir, 0o755))
	mustNil(t, os.WriteFile(filepath.Join(box.Dir, "event-1-bad00000.json"), []byte("{không phải json"), 0o644))
	mustNil(t, os.WriteFile(filepath.Join(box.Dir, "event-2-empty000.json"), nil, 0o644))
	mustNil(t, os.WriteFile(filepath.Join(box.Dir, "snapshot-PAY-1.json"), []byte("rác"), 0o644))
	// hook vẫn chạy bình thường khi outbox có file hỏng
	p := f.pub()
	p.Emit(ev())
	if f.err.String() != "" || f.spawned.Load() != 1 {
		t.Fatal(f.err.String(), f.spawned.Load())
	}
	mustNil(t, p.Drain(t.Context()))
	if f.size() != 0 {
		t.Fatal(box.List(10))
	}
	if n := box.RejectedCount(); n != 3 {
		t.Fatal(n)
	}
	// chỉ event hợp lệ được gửi; file hỏng không lên server
	reqs := web.Requests()
	if len(reqs) != 1 || reqs[0].Path != "/api/ingest/events" || firstEvent(t, reqs[0].Body)["type"] != "FILE_EDIT" {
		t.Fatal(reqs)
	}
	if f.err.String() != "" {
		t.Fatal(f.err.String())
	}
}

// --- tiến trình con ---

const helperEnv = "AISDLC_WEBCLIENT_HELPER"

// TestHelperProcess không phải test thật: là thân tiến trình con của các test bên dưới.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		t.Skip("chỉ chạy như tiến trình con")
	}
	root := os.Getenv("H_ROOT")
	switch mode {
	case "publish":
		helperPublish(root)
	case "hook":
		// "hook": mở tiến trình gửi nền rồi thoát ngay, in ra stdout một dòng
		exe, _ := os.Executable()
		cmd := []string{"-test.run=^TestHelperProcess$"}
		os.Setenv(helperEnv, "detached")
		if err := StartDetached(exe, cmd, root); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println("hook xong")
	case "detached":
		helperDetached(root)
	}
	os.Exit(0)
}

func helperPublish(root string) {
	n, _ := strconv.Atoi(os.Getenv("H_N"))
	prefix := os.Getenv("H_PREFIX")
	url := os.Getenv("H_URL")
	p := NewPublisher(root, func() (*Client, bool) { return &Client{BaseURL: url, Token: "t0k"}, true }, os.Stderr)
	p.Spawn = func() error { return nil }
	runWriter(p, prefix, n)
}

// runWriter ghi n event và snapshot tăng dần, xen kẽ các kiểu gửi như hook và lệnh CLI.
func runWriter(p *Publisher, prefix string, n int) {
	ctx := context.Background()
	for i := 0; i < n; i++ {
		e := Event{Type: "FILE_EDIT", At: time.Now(), Feature: "PAY-1", Detail: fmt.Sprintf("%s-%d", prefix, i)}
		switch i % 4 {
		case 0:
			p.Emit(e)
		case 1:
			p.Queue(e)
			_ = p.Drain(ctx)
		case 2:
			ver := i
			p.AfterCommand("K-"+prefix, func() ([]byte, error) {
				return []byte(fmt.Sprintf(`{"key":"K-%s","v":%d}`, prefix, ver)), nil
			}, e)
		default:
			p.Queue(e)
			p.Flush(ctx)
		}
	}
}

func helperDetached(root string) {
	time.Sleep(3 * time.Second) // sống lâu hơn tiến trình "hook"
	sid, _ := getsid()
	var lines []string
	lines = append(lines, "sid="+strconv.Itoa(sid), "pid="+strconv.Itoa(os.Getpid()))
	for fd := 0; fd <= 2; fd++ {
		target, _ := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
		lines = append(lines, fmt.Sprintf("fd%d=%s", fd, target))
	}
	_ = os.WriteFile(filepath.Join(root, "detached.txt"), []byte(strings.Join(lines, "\n")), 0o644)
}

func getsid() (int, error) {
	r, _, e := syscall.RawSyscall(syscall.SYS_GETSID, 0, 0, 0)
	if e != 0 {
		return 0, e
	}
	return int(r), nil
}

func helperCmd(t *testing.T, env ...string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), env...)
	return cmd
}

func TestSpawnedFlushDetached(t *testing.T) {
	if _, err := os.Stat("/proc/self/fd"); err != nil {
		t.Skip("cần /proc")
	}
	root := t.TempDir()
	cmd := helperCmd(t, helperEnv+"=hook", "H_ROOT="+root)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr // ống: Wait chỉ xong khi mọi tiến trình giữ ống đã đóng nó
	t0 := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	waited := time.Since(t0)
	if strings.TrimSpace(stdout.String()) != "hook xong" {
		t.Fatalf("%q", stdout.String())
	}
	// tiến trình nền ngủ 3 giây rồi mới ghi detached.txt; nếu nó giữ stdout/stderr của hook thì Run đã phải chờ nó
	if _, err := os.Stat(filepath.Join(root, "detached.txt")); err == nil {
		t.Fatalf("hook chờ tiến trình nền (%v)", waited)
	}
	deadline := time.Now().Add(20 * time.Second)
	var info string
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(root, "detached.txt")); err == nil {
			info = string(b)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if info == "" {
		t.Fatal("tiến trình nền không chạy")
	}
	mySid, _ := getsid()
	for _, want := range []string{"fd0=/dev/null", "fd1=/dev/null", "fd2=/dev/null"} {
		if !strings.Contains(info, want) {
			t.Fatalf("thiếu %s:\n%s", want, info)
		}
	}
	if strings.Contains(info, "sid="+strconv.Itoa(mySid)+"\n") {
		t.Fatalf("cùng session với tiến trình test:\n%s", info)
	}
	// Setsid: tiến trình nền là trưởng session mới của chính nó
	var sid, pid string
	for _, l := range strings.Split(info, "\n") {
		if v, ok := strings.CutPrefix(l, "sid="); ok {
			sid = v
		}
		if v, ok := strings.CutPrefix(l, "pid="); ok {
			pid = v
		}
	}
	if sid == "" || sid != pid {
		t.Fatalf("không phải session mới:\n%s", info)
	}
}

func TestConcurrentPublishAndFlushNoLossNoDup(t *testing.T) {
	web := fakeweb.New(t)
	f := newPubFix(t, web.URL())
	const goroutines, procs, perWriter = 4, 3, 40
	var wg sync.WaitGroup
	var procErr atomic.Value
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			p := f.pub()
			p.Spawn = func() error { return nil }
			runWriter(p, fmt.Sprintf("g%d", g), perWriter)
		}(g)
	}
	for c := 0; c < procs; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			cmd := helperCmd(t, helperEnv+"=publish", "H_ROOT="+f.root, "H_URL="+web.URL(),
				"H_PREFIX="+fmt.Sprintf("p%d", c), "H_N="+strconv.Itoa(perWriter))
			if out, err := cmd.CombinedOutput(); err != nil || len(bytes.TrimSpace(out)) > 0 {
				procErr.Store(fmt.Sprintf("tiến trình %d: %v %s", c, err, out))
			}
		}(c)
	}
	wg.Wait()
	if v := procErr.Load(); v != nil {
		t.Fatal(v)
	}
	if f.err.String() != "" {
		t.Fatal(f.err.String())
	}
	// gửi nốt phần còn lại
	if r := f.pub().Push(t.Context()); r.Pending != 0 {
		t.Fatalf("%+v %v", r, NewOutbox(f.root).List(10))
	}
	seen := map[string]int{}
	lastSnap := map[string]float64{}
	for _, r := range web.Requests() {
		switch r.Path {
		case "/api/ingest/events":
			seen[firstEvent(t, r.Body)["detail"].(string)]++
		case "/api/ingest/snapshot":
			m := decode(t, r.Body)
			lastSnap[m["key"].(string)] = m["v"].(float64)
		}
	}
	var prefixes []string
	for g := 0; g < goroutines; g++ {
		prefixes = append(prefixes, fmt.Sprintf("g%d", g))
	}
	for c := 0; c < procs; c++ {
		prefixes = append(prefixes, fmt.Sprintf("p%d", c))
	}
	for _, pf := range prefixes {
		for i := 0; i < perWriter; i++ {
			if n := seen[fmt.Sprintf("%s-%d", pf, i)]; n != 1 {
				t.Errorf("%s-%d gửi %d lần", pf, i, n)
			}
		}
		// snapshot mới nhất của mỗi người ghi (i lớn nhất có i%4==2) không bị mất
		last := (perWriter-1)/4*4 + 2
		if last >= perWriter {
			last -= 4
		}
		if lastSnap["K-"+pf] != float64(last) {
			t.Errorf("snapshot K-%s cuối là %v, muốn %d", pf, lastSnap["K-"+pf], last)
		}
	}
	if len(seen) != len(prefixes)*perWriter {
		t.Errorf("số event %d, muốn %d", len(seen), len(prefixes)*perWriter)
	}
	if NewOutbox(f.root).RejectedCount() != 0 {
		t.Fatal("có file bị từ chối")
	}
}

// Snapshot được thay bằng bản mới trong lúc đang gửi bản cũ: gửi xong không được xóa mất bản mới.
func TestSnapshotReplacedDuringSendIsKept(t *testing.T) {
	for _, status := range []int{200, 400} {
		web := fakeweb.New(t)
		web.Update(func(s *fakeweb.Server) {
			s.Override["POST /api/ingest/snapshot"] = fakeweb.Resp{Status: status, Delay: 400 * time.Millisecond}
		})
		f := newPubFix(t, web.URL())
		box := NewOutbox(f.root)
		mustNil(t, box.PutSnapshot("PAY-1", []byte(`{"v":1}`)))
		done := make(chan struct{})
		go func() {
			defer close(done)
			time.Sleep(100 * time.Millisecond) // lúc bản v1 đang trên đường
			mustNil(t, box.PutSnapshot("PAY-1", []byte(`{"v":2}`)))
		}()
		f.pub().Flush(t.Context())
		<-done
		files := box.List(10)
		if len(files) != 1 || readFile(t, files[0]) != `{"v":2}` {
			t.Fatalf("%d: bản mới bị mất: %v", status, files)
		}
		if reqs := web.RequestsFor("POST", "/api/ingest/snapshot"); len(reqs) != 1 || reqs[0].Body != `{"v":1}` {
			t.Fatalf("%d: %v", status, reqs)
		}
		// v1 đã bị v2 thay trên đĩa nên không còn gì để chuyển sang rejected; v2 chờ lượt sau
		if box.RejectedCount() != 0 {
			t.Fatal("bản mới bị chuyển nhầm sang rejected")
		}
	}
}
