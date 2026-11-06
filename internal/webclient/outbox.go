package webclient

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aisdlc/internal/fsx"
)

// MaxOutboxFiles là số file tối đa trong outbox (Outbox.MAX_FILES).
const MaxOutboxFiles = 2000

// WebDir là .aisdlc/web của repo.
func WebDir(root string) string { return filepath.Join(root, ".aisdlc", "web") }

// Outbox là hàng đợi bản tin ở .aisdlc/web/outbox/: mỗi tính năng một snapshot mới nhất (snapshot-<KEY>.json),
// event là event-<millis>-<id>.json; tối đa 2000 file. File web từ chối vĩnh viễn chuyển sang .aisdlc/web/rejected/.
type Outbox struct{ Dir string }

// NewOutbox là outbox của repo root.
func NewOutbox(root string) Outbox { return Outbox{Dir: filepath.Join(WebDir(root), "outbox")} }

func (o Outbox) rejectedDir() string { return filepath.Join(filepath.Dir(o.Dir), "rejected") }

var (
	millisMu   sync.Mutex
	lastMillis int64
)

// nextMillis tăng đơn điệu trong tiến trình để thứ tự tên file = thứ tự ghi.
func nextMillis() int64 {
	millisMu.Lock()
	defer millisMu.Unlock()
	ms := time.Now().UnixMilli()
	if ms <= lastMillis {
		ms = lastMillis + 1
	}
	lastMillis = ms
	return ms
}

func randID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// PutSnapshot thay file snapshot của tính năng bằng bản mới.
func (o Outbox) PutSnapshot(featureKey string, b []byte) error {
	if err := fsx.WriteAtomic(filepath.Join(o.Dir, "snapshot-"+featureKey+".json"), b, 0o644); err != nil {
		return err
	}
	o.trim()
	return nil
}

// PutEvent ghi một event (JSON đã dựng) vào outbox.
func (o Outbox) PutEvent(b []byte) error {
	name := "event-" + strconv.FormatInt(nextMillis(), 10) + "-" + randID(4) + ".json"
	if err := fsx.WriteAtomic(filepath.Join(o.Dir, name), b, 0o644); err != nil {
		return err
	}
	o.trim()
	return nil
}

// AddEvent ghi event e (EventJSON) vào outbox.
func (o Outbox) AddEvent(e Event) error { return o.PutEvent(EventJSON(e)) }

// IsSnapshotFile: file snapshot (tên bắt đầu "snapshot-").
func IsSnapshotFile(p string) bool { return strings.HasPrefix(filepath.Base(p), "snapshot-") }

func isMessageName(name string) bool {
	return strings.HasSuffix(name, ".json") && !strings.HasPrefix(name, ".") &&
		(strings.HasPrefix(name, "snapshot-") || strings.HasPrefix(name, "event-"))
}

func listJSON(dir string) []string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range es {
		if isMessageName(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func (o Outbox) all() []string { return listJSON(o.Dir) }

func mtime(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.ModTime().UnixMilli()
}

// List trả tối đa max file, cũ nhất trước; snapshot đứng trước event.
func (o Outbox) List(max int) []string {
	var snaps, events []string
	for _, p := range o.all() {
		if IsSnapshotFile(p) {
			snaps = append(snaps, p)
		} else {
			events = append(events, p)
		}
	}
	mt := map[string]int64{}
	for _, p := range snaps {
		mt[p] = mtime(p)
	}
	sort.SliceStable(snaps, func(i, j int) bool {
		if mt[snaps[i]] != mt[snaps[j]] {
			return mt[snaps[i]] < mt[snaps[j]]
		}
		return filepath.Base(snaps[i]) < filepath.Base(snaps[j])
	})
	sort.Slice(events, func(i, j int) bool { return filepath.Base(events[i]) < filepath.Base(events[j]) })
	out := append(snaps, events...)
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// Remove xóa file (không có cũng được).
func (o Outbox) Remove(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Reject chuyển file bị web từ chối vĩnh viễn (hoặc hỏng) sang rejected/.
func (o Outbox) Reject(p string) error {
	if err := os.MkdirAll(o.rejectedDir(), 0o755); err != nil {
		return err
	}
	return os.Rename(p, filepath.Join(o.rejectedDir(), filepath.Base(p)))
}

// Size là số bản tin đang chờ.
func (o Outbox) Size() int { return len(o.all()) }

// RejectedCount là số file web đã từ chối vĩnh viễn (400/413) hoặc hỏng, nằm ở rejected/.
func (o Outbox) RejectedCount() int {
	es, err := os.ReadDir(o.rejectedDir())
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range es {
		if strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

// trim: vượt 2000 file thì bỏ event cũ nhất (snapshot được giữ). Lỗi xóa bỏ qua (tiến trình khác đã xóa).
func (o Outbox) trim() {
	all := o.all()
	excess := len(all) - MaxOutboxFiles
	if excess <= 0 {
		return
	}
	var events []string
	for _, p := range all {
		if !IsSnapshotFile(p) {
			events = append(events, p)
		}
	}
	sort.Slice(events, func(i, j int) bool { return filepath.Base(events[i]) < filepath.Base(events[j]) })
	for i := 0; i < excess && i < len(events); i++ {
		_ = o.Remove(events[i])
	}
}

// writeAtomic ghi file trạng thái web (fsx.WriteAtomic, 0644).
func writeAtomic(p string, b []byte) error { return fsx.WriteAtomic(p, b, 0o644) }
