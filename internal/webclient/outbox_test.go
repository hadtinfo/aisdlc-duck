package webclient

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustNil(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// OutboxTest.capsAt2000DroppingOldestEvents
func TestOutboxCapsAt2000DroppingOldestEvents(t *testing.T) {
	box := NewOutbox(t.TempDir())
	mustNil(t, box.PutSnapshot("PAY-1", []byte("{}")))
	for i := 0; i < 2001; i++ {
		mustNil(t, box.PutEvent([]byte(fmt.Sprintf(`{"n":%d}`, i))))
	}
	if n := box.Size(); n != 2000 {
		t.Fatalf("size %d", n)
	}
	all := box.List(5000)
	if filepath.Base(all[0]) != "snapshot-PAY-1.json" {
		t.Fatal(all[0])
	}
	// event cũ nhất (n=0 và n=1) đã bị bỏ
	if got := readFile(t, all[1]); got != `{"n":2}` {
		t.Fatal(got)
	}
}

// OutboxTest.snapshotReplacesPrevious
func TestOutboxSnapshotReplacesPrevious(t *testing.T) {
	box := NewOutbox(t.TempDir())
	mustNil(t, box.PutSnapshot("PAY-1", []byte(`{"v":1}`)))
	mustNil(t, box.PutSnapshot("PAY-1", []byte(`{"v":2}`)))
	if box.Size() != 1 {
		t.Fatal(box.Size())
	}
	if got := readFile(t, box.List(10)[0]); got != `{"v":2}` {
		t.Fatal(got)
	}
}

// OutboxTest.listsSnapshotsBeforeEventsAndHonoursMax
func TestOutboxListsSnapshotsBeforeEventsAndHonoursMax(t *testing.T) {
	box := NewOutbox(t.TempDir())
	mustNil(t, box.PutEvent([]byte("{}")))
	mustNil(t, box.PutSnapshot("PAY-1", []byte("{}")))
	mustNil(t, box.PutEvent([]byte("{}")))
	two := box.List(2)
	if len(two) != 2 || !IsSnapshotFile(two[0]) || IsSnapshotFile(two[1]) {
		t.Fatal(two)
	}
}

// OutboxTest.sizeOfMissingOutboxIsZeroAndCreatesNothing
func TestOutboxSizeOfMissingOutboxIsZeroAndCreatesNothing(t *testing.T) {
	root := t.TempDir()
	if NewOutbox(root).Size() != 0 {
		t.Fatal("size")
	}
	if _, err := os.Stat(filepath.Join(root, ".aisdlc")); !os.IsNotExist(err) {
		t.Fatal("đã tạo .aisdlc")
	}
}

func TestOutboxLayoutAndTempFilesIgnored(t *testing.T) {
	root := t.TempDir()
	box := NewOutbox(root)
	if box.Dir != filepath.Join(root, ".aisdlc", "web", "outbox") {
		t.Fatal(box.Dir)
	}
	mustNil(t, box.AddEvent(Event{Type: "FILE_EDIT"}))
	// file tạm (ghi dở) và file lạ không phải bản tin
	mustNil(t, os.WriteFile(filepath.Join(box.Dir, ".event-1.json.tmp-9"), []byte("x"), 0o644))
	mustNil(t, os.WriteFile(filepath.Join(box.Dir, "notes.txt"), []byte("x"), 0o644))
	files := box.List(10)
	if len(files) != 1 || !strings.HasPrefix(filepath.Base(files[0]), "event-") || !strings.HasSuffix(files[0], ".json") {
		t.Fatal(files)
	}
	if box.Size() != 1 {
		t.Fatal(box.Size())
	}
	mustNil(t, box.Reject(files[0]))
	if box.RejectedCount() != 1 || box.Size() != 0 {
		t.Fatal(box.RejectedCount(), box.Size())
	}
	if _, err := os.Stat(filepath.Join(root, ".aisdlc", "web", "rejected", filepath.Base(files[0]))); err != nil {
		t.Fatal(err)
	}
}

func TestEventFileNamesOrderedByWrite(t *testing.T) {
	box := NewOutbox(t.TempDir())
	for i := 0; i < 50; i++ {
		mustNil(t, box.PutEvent([]byte(fmt.Sprintf(`{"n":%d}`, i))))
	}
	for i, f := range box.List(100) {
		if got := readFile(t, f); got != fmt.Sprintf(`{"n":%d}`, i) {
			t.Fatalf("thứ tự sai ở %d: %s", i, got)
		}
	}
}

func TestBackoffActiveStartClear(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	if BackoffActive(root, now) {
		t.Fatal("không có file mà vẫn active")
	}
	mustNil(t, BackoffStart(root, now))
	if got := strings.TrimSpace(readFile(t, BackoffFile(root))); got != "2026-11-02T03:01:00Z" {
		t.Fatal(got)
	}
	if !BackoffActive(root, now.Add(59*time.Second)) || BackoffActive(root, now.Add(60*time.Second)) {
		t.Fatal("hạn sai")
	}
	mustNil(t, os.WriteFile(BackoffFile(root), []byte("hỏng"), 0o644))
	if BackoffActive(root, now) {
		t.Fatal("file hỏng phải coi như không chờ")
	}
	BackoffClear(root)
	if _, err := os.Stat(BackoffFile(root)); !os.IsNotExist(err) {
		t.Fatal("chưa xóa")
	}
}

func TestFlushLockExclusiveAndBusy(t *testing.T) {
	root := t.TempDir()
	l, ok, err := TryFlushLock(root)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, ok2, _ := TryFlushLock(root); ok2 {
		t.Fatal("khóa thứ hai trong cùng tiến trình phải thất bại")
	}
	if busy, _ := FlushBusy(root); !busy {
		t.Fatal("phải bận")
	}
	start := time.Now()
	if _, ok3, _ := WaitFlushLock(t.Context(), root, 200*time.Millisecond); ok3 {
		t.Fatal("chờ trong ngân sách mà vẫn lấy được")
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("không chờ hết ngân sách")
	}
	l.Close()
	if busy, _ := FlushBusy(root); busy {
		t.Fatal("đã nhả mà vẫn bận")
	}
	l2, ok, err := WaitFlushLock(t.Context(), root, time.Second)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	l2.Close()
	if _, err := os.Stat(filepath.Join(root, ".aisdlc", "web", "flush.lock")); err != nil {
		t.Fatal(err)
	}
}

// FeedbackLogTest.lastEntryPerGate
func TestFeedbackLastEntryPerGate(t *testing.T) {
	f := filepath.Join(t.TempDir(), "feedback.md")
	mustNil(t, os.WriteFile(f, []byte("## DESIGN — trả lại bởi An lúc 2026-11-03T01:00:00Z\n\nThiếu bước lỗi\n\n"+
		"## DESIGN — mở lại bởi An lúc 2026-11-03T02:00:00Z\n\n"), 0o644))
	fb, ok, err := LastFeedback(f, "DESIGN")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if fb.Kind != "REOPENED" || fb.By != "An" || !fb.At.Time().Equal(time.Date(2026, 11, 3, 2, 0, 0, 0, time.UTC)) || fb.Comment != "" {
		t.Fatalf("%+v", fb)
	}
	if _, ok, _ := LastFeedback(f, "BRD"); ok {
		t.Fatal("BRD không có mục")
	}
}

// FeedbackLogTest.rejectedKeepsComment
func TestFeedbackRejectedKeepsComment(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "feedback.md")
	mustNil(t, os.WriteFile(f, []byte("## BRD — trả lại bởi Bình lúc 2026-11-03T01:00:00Z\n\nThiếu bước lỗi\nDòng 2\n\n"), 0o644))
	fb, ok, _ := LastFeedback(f, "BRD")
	if !ok || fb.Kind != "REJECTED" || fb.Comment != "Thiếu bước lỗi\nDòng 2" {
		t.Fatalf("%+v", fb)
	}
	if _, ok, err := LastFeedback(filepath.Join(dir, "none.md"), "BRD"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

// FeedbackLogTest.webIdMarkerIsNotPartOfComment
func TestFeedbackWebIDMarkerIsNotPartOfComment(t *testing.T) {
	f := filepath.Join(t.TempDir(), "feedback.md")
	mustNil(t, os.WriteFile(f, []byte("## BRD — trả lại bởi Bình lúc 2026-11-03T01:00:00Z\n<!-- aisdlc-fb:7 -->\n\nThiếu\n\n"), 0o644))
	fb, _, _ := LastFeedback(f, "BRD")
	if fb.Comment != "Thiếu" {
		t.Fatalf("%q", fb.Comment)
	}
}
