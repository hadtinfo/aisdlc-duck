package webclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// BackoffDelay là thời gian chờ sau lỗi kết nối (Backoff.DELAY).
const BackoffDelay = 60 * time.Second

// BackoffFile là .aisdlc/web/backoff-until: tới mốc đó hook không mở tiến trình gửi nền và lệnh CLI chỉ ghi outbox.
func BackoffFile(root string) string { return filepath.Join(WebDir(root), "backoff-until") }

// BackoffActive: còn trong thời gian chờ; file thiếu hoặc hỏng thì không chờ.
func BackoffActive(root string, now time.Time) bool {
	b, err := os.ReadFile(BackoffFile(root))
	if err != nil {
		return false
	}
	until, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
	if err != nil {
		return false
	}
	return now.Before(until)
}

// BackoffStart ghi mốc now + 60 giây.
func BackoffStart(root string, now time.Time) error {
	return writeAtomic(BackoffFile(root), []byte(javaInstant(now.Add(BackoffDelay))))
}

// BackoffClear xóa mốc chờ (lỗi bỏ qua: lần sau xóa lại).
func BackoffClear(root string) { _ = os.Remove(BackoffFile(root)) }

// FlushLockFile là .aisdlc/web/flush.lock.
func FlushLockFile(root string) string { return filepath.Join(WebDir(root), "flush.lock") }

// FlushLock là khóa flock trên .aisdlc/web/flush.lock: mỗi lúc chỉ một tiến trình (hoặc goroutine) gửi outbox,
// tránh gửi trùng một event. Khóa của hệ điều hành tự nhả khi tiến trình chết nên không có khóa treo. flock gắn với
// từng lần mở file nên hai lần lấy trong cùng tiến trình cũng loại trừ nhau.
type FlushLock struct{ f *os.File }

// TryFlushLock lấy khóa nếu đang rảnh; ok=false nếu tiến trình khác (hoặc chính tiến trình này) đang giữ.
func TryFlushLock(root string) (*FlushLock, bool, error) {
	p := FlushLockFile(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, false, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &FlushLock{f: f}, true, nil
}

// WaitFlushLock thử lấy khóa mỗi 50 ms trong ngân sách budget (hoặc tới khi ctx xong).
func WaitFlushLock(ctx context.Context, root string, budget time.Duration) (*FlushLock, bool, error) {
	until := time.Now().Add(budget)
	for {
		l, ok, err := TryFlushLock(root)
		if ok || err != nil || !time.Now().Before(until) {
			return l, ok, err
		}
		select {
		case <-ctx.Done():
			return nil, false, nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// FlushBusy: có tiến trình đang gửi (lấy thử rồi nhả ngay).
func FlushBusy(root string) (bool, error) {
	l, ok, err := TryFlushLock(root)
	if err != nil {
		return false, err
	}
	if ok {
		l.Close()
	}
	return !ok, nil
}

// Close nhả khóa (an toàn gọi nhiều lần; khóa cũng tự nhả khi tiến trình kết thúc).
func (l *FlushLock) Close() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}
