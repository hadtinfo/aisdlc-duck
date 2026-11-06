package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/step"
	"aisdlc/internal/webclient"
	"aisdlc/internal/webclient/fakeweb"
)

// ControlLedgerTest (4) và WebControlTest (5).

const ledgerKey = model.FeatureKey("PAY-1")

func ids(is []snapshot.Instruction) []int64 {
	out := make([]int64, 0, len(is))
	for _, i := range is {
		out = append(out, i.ID)
	}
	return out
}

func TestClaimOnce(t *testing.T) {
	var items []snapshot.Instruction
	for i := int64(1); i <= 8; i++ {
		items = append(items, snapshot.Instruction{ID: i, By: "an", At: fakeweb.T, Text: "c"})
	}
	for round := 0; round < 5; round++ {
		root := t.TempDir()
		var (
			mu  sync.Mutex
			got []int64
			wg  sync.WaitGroup
		)
		start := make(chan struct{})
		for k := 0; k < 2; k++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				c, err := NewControlLedger(root, ledgerKey, 0).Claim(items)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				got = append(got, ids(c)...)
				mu.Unlock()
			}()
		}
		close(start)
		wg.Wait()
		sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
		eq(t, len(got), len(items))
		for i, id := range got {
			eq(t, id, int64(i+1)) // không trùng
		}
		eq(t, len(NewControlLedger(root, ledgerKey, 0).Delivered()), len(items))
		c, err := NewControlLedger(root, ledgerKey, 0).Claim(items)
		must(t, err)
		eq(t, len(c), 0)
	}
}

// ---- tiến trình con giữ control.lock ----

const lockHolderEnv = "AISDLC_CLI_LOCK_HOLDER"

// TestHelperHoldLock không phải test thật: là thân tiến trình con giữ khóa flock trên file trong biến môi trường
// (LockHolder.java), in "locked" rồi ngủ tới khi bị giết.
func TestHelperHoldLock(t *testing.T) {
	lock := os.Getenv(lockHolderEnv)
	if lock == "" {
		t.Skip("chỉ chạy như tiến trình con")
	}
	_ = os.MkdirAll(filepath.Dir(lock), 0o755)
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		os.Stdout.WriteString("lỗi: " + err.Error() + "\n")
		os.Exit(2)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		os.Stdout.WriteString("lỗi: " + err.Error() + "\n")
		os.Exit(2)
	}
	os.Stdout.WriteString("locked\n")
	time.Sleep(120 * time.Second)
	os.Exit(0)
}

// holdLock mở tiến trình con giữ khóa lock tới khi test gọi hàm trả về (hoặc test xong).
func holdLock(t *testing.T, lock string) (release func()) {
	t.Helper()
	exe, err := os.Executable()
	must(t, err)
	cmd := exec.Command(exe, "-test.run=^TestHelperHoldLock$")
	cmd.Env = append(os.Environ(), lockHolderEnv+"="+lock)
	out, err := cmd.StdoutPipe()
	must(t, err)
	must(t, cmd.Start())
	var once sync.Once
	release = func() {
		once.Do(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
	}
	t.Cleanup(release)
	line, _ := bufio.NewReader(out).ReadString('\n')
	if strings.TrimSpace(line) != "locked" {
		release()
		t.Fatalf("tiến trình giữ khóa không chạy được: %q", line)
	}
	return release
}

func controlLockFile(root string) string {
	return filepath.Join(root, ".aisdlc/web/control/control.lock")
}

func TestBudgetedLockGivesUpWhenAnotherProcessHoldsIt(t *testing.T) {
	root := t.TempDir()
	items := []snapshot.Instruction{{ID: 1, By: "an", At: fakeweb.T, Text: "c1"}}
	release := holdLock(t, controlLockFile(root))
	t0 := time.Now()
	_, err := NewControlLedger(root, ledgerKey, time.Second).Claim(items)
	ms := time.Since(t0)
	var busy *ControlBusy
	if !errors.As(err, &busy) {
		t.Fatalf("muốn ControlBusy, được %v", err)
	}
	if ms < 900*time.Millisecond || ms > 5*time.Second {
		t.Fatalf("chờ khoảng 1 giây rồi bỏ: %v", ms)
	}
	release()
	eq(t, len(NewControlLedger(root, ledgerKey, 0).Delivered()), 0)
	got, err := NewControlLedger(root, ledgerKey, time.Second).Claim(items)
	must(t, err)
	eq(t, len(got), 1)
}

func TestBudgetedLockGivesUpWhenThisProcessHoldsIt(t *testing.T) {
	root := t.TempDir()
	holding, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _ = NewControlLedger(root, ledgerKey, 0).Apply(func(map[int64]bool) []step.PendingResult {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	err := NewControlLedger(root, ledgerKey, 300*time.Millisecond).
		RecordResults([]step.PendingResult{{ID: 5, Status: "APPLIED"}})
	close(release)
	<-done
	var busy *ControlBusy
	if !errors.As(err, &busy) {
		t.Fatalf("muốn ControlBusy, được %v", err)
	}
	eq(t, len(NewControlLedger(root, ledgerKey, 0).Done()), 0)
}

func TestResultsBecomeDone(t *testing.T) {
	root := t.TempDir()
	must(t, NewControlLedger(root, ledgerKey, 0).RecordResults([]step.PendingResult{
		{ID: 5, Status: "APPLIED"}, {ID: 6, Status: "STALE", Reason: "r"}}))
	done := NewControlLedger(root, ledgerKey, 0).Done()
	eq(t, len(done), 2)
	eq(t, done[5] && done[6], true)
	_, err := os.Stat(filepath.Join(root, ".aisdlc/web/control/PAY-1.local.json"))
	must(t, err)
	// cùng định dạng sổ với Java
	eq(t, readFile(t, filepath.Join(root, ".aisdlc/web/control/PAY-1.local.json")),
		`{"delivered":[],"results":{"5":"APPLIED","6":"STALE"}}`)
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	return string(b)
}

// ---- WebControlTest ----

type controlWebFx struct {
	*testRepo
	web    *fakeweb.Server
	repoID string
}

func newControlWebFx(t *testing.T) *controlWebFx {
	r := newTestRepo(t)
	r.setupInit()
	repoID := snapshot.RepoID(context.Background(), gitx.New(r.root), r.root)
	web := fakeweb.New(t)
	web.SetFeature(repoID, "PAY-1")
	home := t.TempDir()
	r.env["HOME"] = home
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "ingest-tok"
	must(t, webclient.Pin(home, web.Pub))
	return &controlWebFx{r, web, repoID}
}

// control là WebControl của một lệnh mới (cờ webDown riêng). Stderr bỏ đi.
func (c *controlWebFx) control() *WebControl {
	return NewWebControl(NewWebCache(c.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), c.root))
}

func (c *controlWebFx) serve(issuedAt time.Time, pause *snapshot.Pause, ins ...snapshot.Instruction) {
	c.web.Update(func(s *fakeweb.Server) {
		s.Control = snapshot.FeatureControl{Repo: c.repoID, Feature: "PAY-1", IssuedAt: issuedAt, Pause: pause,
			Instructions: ins}
	})
}

func pauseAn() *snapshot.Pause { return &snapshot.Pause{By: "an", At: fakeweb.T} }

func (c *controlWebFx) cacheFile() string { return snapshot.ControlCacheFile(c.root, ledgerKey) }

func (c *controlWebFx) inboxExists() bool { return c.exists(".aisdlc/web/control/inbox") }

func TestValidEnvelopeWritesCache(t *testing.T) {
	c := newControlWebFx(t)
	c.serve(fakeweb.T, pauseAn())
	wc := c.control()
	eq(t, wc.Refresh(ledgerKey), true)
	_, err := os.Stat(c.cacheFile())
	must(t, err)
	got := wc.Cached(ledgerKey)
	if got == nil || got.Pause == nil {
		t.Fatalf("cache: %+v", got)
	}
	eq(t, got.Pause.By, "an")
	req := c.web.RequestsFor("GET", "/api/cli/control")[0]
	contains(t, req.Query, "feature=PAY-1")
	eq(t, req.Auth, "Bearer ingest-tok")
}

func TestControlBadSignatureKeepsOldCache(t *testing.T) {
	c := newControlWebFx(t)
	c.serve(fakeweb.T, pauseAn())
	eq(t, c.control().Refresh(ledgerKey), true)
	before := readFile(t, c.cacheFile())
	_, other, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	c.web.Update(func(s *fakeweb.Server) { s.Signer = other })
	c.serve(fakeweb.T.Add(time.Minute), nil)
	eq(t, c.control().Refresh(ledgerKey), false)
	eq(t, readFile(t, c.cacheFile()), before)
	if got := c.control().Cached(ledgerKey); got == nil || got.Pause == nil {
		t.Fatalf("cache cũ phải còn tạm dừng: %+v", got)
	}
}

func TestOlderIssuedAtDoesNotOverwrite(t *testing.T) {
	c := newControlWebFx(t)
	c.serve(fakeweb.T.Add(600*time.Second), pauseAn())
	eq(t, c.control().Refresh(ledgerKey), true)
	before := readFile(t, c.cacheFile())
	c.serve(fakeweb.T, nil)
	c.control().Refresh(ledgerKey)
	eq(t, readFile(t, c.cacheFile()), before)
	c.serve(fakeweb.T.Add(601*time.Second), nil)
	eq(t, c.control().Refresh(ledgerKey), true)
	if got := c.control().Cached(ledgerKey); got == nil || got.Pause != nil {
		t.Fatalf("phải là bản mới không tạm dừng: %+v", got)
	}
}

func TestInboxFlagFollowsUndeliveredInstructions(t *testing.T) {
	c := newControlWebFx(t)
	i1 := snapshot.Instruction{ID: 11, By: "an", At: fakeweb.T, Text: "dùng BigDecimal"}
	c.serve(fakeweb.T, pauseAn(), i1)
	wc := c.control()
	eq(t, wc.Refresh(ledgerKey), true)
	eq(t, c.inboxExists(), true)
	eq(t, fmt.Sprint(ids(wc.Undelivered(ledgerKey))), "[11]")
	// đã giao trên máy này: không còn là chưa giao, làm mới không tạo lại cờ
	_, err := NewControlLedger(c.root, ledgerKey, 0).Claim(wc.Undelivered(ledgerKey))
	must(t, err)
	eq(t, len(wc.Undelivered(ledgerKey)), 0)
	c.serve(fakeweb.T.Add(time.Second), pauseAn(), i1)
	eq(t, c.control().Refresh(ledgerKey), true)
	eq(t, c.inboxExists(), false)
	// chỉ dẫn mới → có cờ; web không còn chỉ dẫn nào → xóa cờ
	c.serve(fakeweb.T.Add(2*time.Second), pauseAn(), i1,
		snapshot.Instruction{ID: 12, By: "an", At: fakeweb.T, Text: "thêm test"})
	eq(t, c.control().Refresh(ledgerKey), true)
	eq(t, c.inboxExists(), true)
	c.serve(fakeweb.T.Add(3*time.Second), pauseAn())
	eq(t, c.control().Refresh(ledgerKey), true)
	eq(t, c.inboxExists(), false)
}

func TestControlNetworkErrorReturnsFalseAndStartsBackoff(t *testing.T) {
	c := newControlWebFx(t)
	c.web.Update(func(s *fakeweb.Server) { s.Drop = true })
	eq(t, c.control().Refresh(ledgerKey), false)
	eq(t, c.exists(".aisdlc/web/backoff-until"), true)
	before := len(c.web.Requests())
	eq(t, c.control().Refresh(ledgerKey), false)
	eq(t, len(c.web.Requests()), before)
}
