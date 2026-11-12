package webclient

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aisdlc/internal/gitx"
	"aisdlc/internal/i18n"
	"aisdlc/internal/snapshot"
)

// Giới hạn của WebPublisher.java.
const (
	FlushMax           = 20               // số file tối đa mỗi lượt gửi
	InlineDeadline     = 2 * time.Second  // lệnh CLI chờ gửi tối đa
	BackgroundDeadline = 5 * time.Second  // tiến trình gửi nền (`aisdlc web flush`)
	PushDeadline       = 30 * time.Second // `aisdlc web push`
	PushLockWait       = 10 * time.Second // chờ khóa khi tiến trình khác đang gửi
)

// FlushResult là kết quả một lượt gửi: số bản tin đã gửi và số còn chờ trong outbox.
type FlushResult struct{ Sent, Pending int }

// outcome của một lượt; stopped: dừng vì lỗi, quá giờ hoặc không xử lý được file nào.
type outcome struct {
	sent, pending int
	stopped       bool
}

// Publisher ghi bản tin vào outbox rồi gửi không chặn (WebPublisher). Mọi lỗi chỉ ra Err một dòng
// "aisdlc web: ..."; không bao giờ ra stdout và không đổi exit code của lệnh.
//
// Hook chỉ ghi outbox rồi mở tiến trình gửi nền (Spawn → `aisdlc web flush`); lệnh CLI gửi ngay nhưng tối đa
// InlineDeadline. Sau lỗi kết nối có backoff; FlushLock bảo đảm một tiến trình gửi mỗi lúc.
type Publisher struct {
	Root string
	// Client trả client đã cấu hình; false khi web tắt (không cấu hình).
	Client func() (*Client, bool)
	Outbox Outbox
	// Now là đồng hồ của mốc thời gian ghi file (backoff-until, last-ok); nil → time.Now. Hạn gửi dùng đồng hồ thật.
	Now func() time.Time
	// Spawn mở tiến trình gửi nền (mặc định SpawnFlush(Root)).
	Spawn func() error
	// Err nhận dòng lỗi (stderr); nil → bỏ.
	Err io.Writer
	// RepoID là định danh repo của lô event; nil → snapshot.RepoID của Root (tính một lần).
	RepoID func() string

	mu       sync.Mutex
	repoID   *string
	clientID string
}

// NewPublisher tạo Publisher cho repo root.
func NewPublisher(root string, client func() (*Client, bool), errw io.Writer) *Publisher {
	p := &Publisher{Root: root, Client: client, Outbox: NewOutbox(root), Err: errw}
	p.Spawn = func() error { return SpawnFlush(root, "") }
	return p
}

func (p *Publisher) client() (*Client, bool) {
	if p.Client == nil {
		return nil, false
	}
	c, ok := p.Client()
	return c, ok && c != nil
}

// Enabled: web đã cấu hình.
func (p *Publisher) Enabled() bool {
	_, ok := p.client()
	return ok
}

func (p *Publisher) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

// run chạy fn, panic thành lỗi (Java bắt mọi Throwable trừ OutOfMemoryError).
func safely(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return fn()
}

// guard chạy fn; lỗi (kể cả panic) chỉ thành một dòng "aisdlc web: ..." ở Err.
func (p *Publisher) guard(fn func() error) {
	if err := safely(fn); err != nil {
		p.warn("aisdlc web: " + err.Error())
	}
}

func (p *Publisher) warn(line string) {
	if p.Err != nil {
		fmt.Fprintln(p.Err, line)
	}
}

// AfterCommand (lệnh CLI): ghi snapshot mới của tính năng (build) và event e, rồi gửi ngay tối đa InlineDeadline
// (trừ khi đang backoff hoặc tiến trình khác đang gửi). Lỗi dựng snapshot cũng chỉ ra Err.
func (p *Publisher) AfterCommand(featureKey string, build func() ([]byte, error), e Event) {
	p.AfterCommandFunc(featureKey, build, func() Event { return e })
}

// AfterCommandFunc như AfterCommand, nhưng event dựng trong guard (sau snapshot): lỗi hay panic khi dựng event
// (vd. đọc git) cũng chỉ thành một dòng ở Err.
func (p *Publisher) AfterCommandFunc(featureKey string, build func() ([]byte, error), event func() Event) {
	if !p.Enabled() {
		return
	}
	p.guard(func() error {
		snap, err := build()
		if err != nil {
			return err
		}
		if err := p.Outbox.PutSnapshot(featureKey, snap); err != nil {
			return err
		}
		if err := p.Outbox.AddEvent(event()); err != nil {
			return err
		}
		return p.sendInline(context.Background(), InlineDeadline)
	})
}

// Emit (hook): chỉ ghi event vào outbox, rồi mở tiến trình gửi nền nếu không đang backoff và chưa có tiến trình
// nào đang gửi. Không bao giờ gửi mạng trong tiến trình hook.
func (p *Publisher) Emit(e Event) {
	if !p.Enabled() {
		return
	}
	p.guard(func() error {
		if err := p.Outbox.AddEvent(e); err != nil {
			return err
		}
		if BackoffActive(p.Root, p.now()) {
			return nil
		}
		busy, err := FlushBusy(p.Root)
		if err != nil || busy {
			return err
		}
		if p.Spawn == nil {
			return nil
		}
		return p.Spawn()
	})
}

// Queue (lệnh CLI): chỉ đặt event vào outbox; AfterCommand của lệnh gửi đi.
func (p *Publisher) Queue(e Event) {
	if !p.Enabled() {
		return
	}
	p.guard(func() error { return p.Outbox.AddEvent(e) })
}

// Stage dựng snapshot và đặt vào outbox (không gửi); dùng cho `web push`. quiet: lỗi không in gì (hook).
func (p *Publisher) Stage(featureKey string, build func() ([]byte, error), quiet bool) {
	fn := func() error {
		snap, err := build()
		if err != nil {
			return err
		}
		return p.Outbox.PutSnapshot(featureKey, snap)
	}
	if quiet {
		_ = safely(fn)
		return
	}
	p.guard(fn)
}

// SendSnapshot gửi snapshot của tính năng ngay (snapshot đứng đầu hàng đợi), chờ khóa nếu đang có tiến trình gửi.
// Dùng trước khi nộp cổng ở chế độ web. Không bao giờ lỗi.
func (p *Publisher) SendSnapshot(ctx context.Context, featureKey string, build func() ([]byte, error)) {
	if !p.Enabled() {
		return
	}
	p.Stage(featureKey, build, false)
	p.Flush(ctx)
}

// FlushInline (lệnh CLI): gửi một lượt (tối đa 20 file) trong tổng thời gian deadline; đang backoff hoặc có
// tiến trình khác đang gửi thì để lại outbox.
func (p *Publisher) FlushInline(ctx context.Context, deadline time.Duration) {
	if !p.Enabled() {
		return
	}
	p.guard(func() error { return p.sendInline(ctx, deadline) })
}

func (p *Publisher) sendInline(ctx context.Context, deadline time.Duration) error {
	c, ok := p.client()
	if !ok || BackoffActive(p.Root, p.now()) {
		return nil
	}
	l, ok, err := TryFlushLock(p.Root)
	if err != nil || !ok {
		return err // tiến trình gửi nền xem lại outbox sau khi nhả khóa
	}
	defer l.Close()
	_, err = p.doFlush(ctx, c, time.Now().Add(deadline))
	return err
}

// Flush gửi một lượt (tối đa 20 file, tối đa InlineDeadline), bỏ qua backoff; chờ khóa tối đa PushLockWait.
func (p *Publisher) Flush(ctx context.Context) FlushResult { return p.locked(ctx, false) }

// Push (`aisdlc web push`) gửi cả hàng đợi (tối đa PushDeadline), bỏ qua backoff.
func (p *Publisher) Push(ctx context.Context) FlushResult { return p.locked(ctx, true) }

func (p *Publisher) locked(ctx context.Context, all bool) FlushResult {
	c, ok := p.client()
	if !ok {
		return FlushResult{}
	}
	var res *FlushResult
	p.guard(func() error {
		l, ok, err := WaitFlushLock(ctx, p.Root, PushLockWait)
		if err != nil {
			return err
		}
		if !ok {
			p.warn(i18n.T("aisdlc web: tiến trình khác đang gửi; thử lại sau"))
			return nil
		}
		defer l.Close()
		d := InlineDeadline
		if all {
			d = PushDeadline
		}
		deadline := time.Now().Add(d)
		sent := 0
		for {
			o, err := p.doFlush(ctx, c, deadline)
			if err != nil {
				return err
			}
			sent += o.sent
			if !all || o.stopped || o.pending == 0 {
				res = &FlushResult{Sent: sent, Pending: o.pending}
				return nil
			}
		}
	})
	if res != nil {
		return *res
	}
	return FlushResult{Pending: p.Outbox.Size()}
}

// Drain là tiến trình gửi nền (`aisdlc web flush`, do hook mở): gửi hết outbox trong tối đa BackgroundDeadline.
// Đang backoff hoặc tiến trình khác giữ khóa thì thôi ngay. Không in gì; lỗi trả về cho nơi gọi.
func (p *Publisher) Drain(ctx context.Context) error {
	c, ok := p.client()
	if !ok {
		return nil
	}
	return safely(func() error {
		deadline := time.Now().Add(BackgroundDeadline)
		for time.Now().Before(deadline) && !BackoffActive(p.Root, p.now()) && ctx.Err() == nil {
			l, ok, err := TryFlushLock(p.Root)
			if err != nil || !ok {
				return err
			}
			var o outcome
			err = func() error {
				defer l.Close()
				p.Outbox.RecoverSending() // file .sending của tiến trình gửi đã chết: trả về hàng đợi
				for {
					var err error
					if o, err = p.doFlush(ctx, c, deadline); err != nil || o.stopped || o.pending == 0 {
						return err
					}
				}
			}()
			if err != nil {
				return err
			}
			// Xem lại sau khi nhả khóa: hook/lệnh ghi bản tin lúc khóa bị giữ thì không tự gửi.
			if o.stopped || p.Outbox.Size() == 0 {
				return nil
			}
		}
		return nil
	})
}

// doFlush gửi tối đa 20 file cũ nhất, dừng ở deadline. Phải giữ FlushLock. File hỏng (đọc không được, JSON sai)
// chuyển sang rejected/ thay vì chặn hàng đợi mãi.
func (p *Publisher) doFlush(ctx context.Context, c *Client, deadline time.Time) (outcome, error) {
	files := p.Outbox.List(FlushMax)
	if len(files) == 0 {
		return outcome{}, nil
	}
	clientID, err := p.ClientID()
	if err != nil {
		return outcome{}, err
	}
	repoID := p.repo()
	sent, handled, stopped := 0, 0, false
	for _, file := range files {
		left := time.Until(deadline)
		if left <= 0 || ctx.Err() != nil {
			stopped = true
			break
		}
		snap := IsSnapshotFile(file)
		body, st, err := readMessage(file)
		if errors.Is(err, os.ErrNotExist) {
			continue // file vừa bị lấy đi (trim)
		}
		if err == nil {
			if snap {
				if !json.Valid(body) {
					err = errors.New(i18n.T("snapshot không phải JSON hợp lệ"))
				}
			} else {
				body, err = batchJSON(repoID, clientID, body)
			}
		}
		if err != nil {
			if err := p.take(file, st, true); err != nil {
				return outcome{}, err
			}
			handled++
			continue
		}
		path := "/api/ingest/events"
		if snap {
			path = "/api/ingest/snapshot"
		}
		rctx, cancel := context.WithTimeout(ctx, left)
		resp, err := c.Do(rctx, http.MethodPost, path, body)
		expired := rctx.Err() != nil
		cancel()
		if err != nil {
			// web không với tới được: giữ hàng đợi, thôi các file còn lại; quá giờ do hạn của chính lượt gửi
			// (còn dưới 2 giây) hoặc người gọi hủy thì không tính là web hỏng
			own := (expired && left < 2*time.Second) || ctx.Err() != nil
			if !own {
				if err := BackoffStart(p.Root, p.now()); err != nil {
					return outcome{}, err
				}
			}
			stopped = true
			break
		}
		s := resp.Status
		switch {
		case s/100 == 2:
			if err := p.take(file, st, false); err != nil {
				return outcome{}, err
			}
			sent++
			handled++
		case s/100 == 4 && s != 401 && s != 408 && s != 429:
			if err := p.take(file, st, true); err != nil {
				return outcome{}, err
			}
			handled++
		default:
			stopped = true
		}
		if stopped {
			break
		}
	}
	if sent > 0 {
		if err := writeAtomic(filepath.Join(WebDir(p.Root), "last-ok"), []byte(javaInstant(p.now()))); err != nil {
			return outcome{}, err
		}
		BackoffClear(p.Root)
	}
	return outcome{sent: sent, pending: p.Outbox.Size(), stopped: stopped || handled == 0}, nil
}

func readMessage(file string) ([]byte, os.FileInfo, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, st, errors.New(i18n.T("không phải file thường"))
	}
	b, err := io.ReadAll(f)
	return b, st, err
}

// take bỏ file đã xử lý khỏi outbox (reject: chuyển sang rejected/). Snapshot có thể bị lệnh khác thay bằng bản mới
// trong lúc gửi (cùng tên): chỉ bỏ đúng bản đã đọc (st), bản mới hơn được để lại để gửi lượt sau.
func (p *Publisher) take(file string, st os.FileInfo, reject bool) error {
	if !IsSnapshotFile(file) || st == nil {
		if reject {
			if err := p.Outbox.Reject(file); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		}
		return p.Outbox.Remove(file)
	}
	tmp := filepath.Join(filepath.Dir(file), "."+filepath.Base(file)+"."+randID(4)+".sending")
	if err := os.Rename(file, tmp); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	cur, err := os.Stat(tmp)
	if err == nil && os.SameFile(cur, st) {
		if reject {
			if err := os.MkdirAll(p.Outbox.rejectedDir(), 0o755); err != nil {
				return err
			}
			return os.Rename(tmp, filepath.Join(p.Outbox.rejectedDir(), filepath.Base(file)))
		}
		return os.Remove(tmp)
	}
	// tmp là bản mới hơn ghi trong lúc gửi: trả về chỗ cũ, trừ khi đã có bản còn mới hơn nữa
	if err := os.Link(tmp, file); err != nil && !errors.Is(err, os.ErrExist) {
		return os.Rename(tmp, file)
	}
	return os.Remove(tmp)
}

func (p *Publisher) repo() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.repoID == nil {
		var id string
		if p.RepoID != nil {
			id = p.RepoID()
		} else {
			id = snapshot.RepoID(context.Background(), gitx.New(p.Root), p.Root)
		}
		p.repoID = &id
	}
	return *p.repoID
}

// ClientID là định danh clone (.aisdlc/web/client-id), đọc một lần mỗi Publisher.
func (p *Publisher) ClientID() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.clientID == "" {
		id, err := ClientID(p.Root)
		if err != nil {
			return "", err
		}
		p.clientID = id
	}
	return p.clientID, nil
}

// ClientID đọc hoặc tạo .aisdlc/web/client-id (UUID). Hai tiến trình cùng tạo thì cả hai dùng bản ghi trước.
func ClientID(root string) (string, error) {
	f := filepath.Join(WebDir(root), "client-id")
	if b, err := os.ReadFile(f); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id, nil
		}
	}
	id := newUUID()
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return "", fmt.Errorf(i18n.T("Không ghi được %s: %w"), f, err)
	}
	tmp := f + "." + randID(4) + ".tmp"
	if err := os.WriteFile(tmp, []byte(id), 0o644); err != nil {
		return "", fmt.Errorf(i18n.T("Không ghi được %s: %w"), f, err)
	}
	err := os.Link(tmp, f) // không ghi đè bản tiến trình khác vừa tạo
	_ = os.Remove(tmp)
	if err == nil {
		return id, nil
	}
	if b, rerr := os.ReadFile(f); rerr == nil {
		if cur := strings.TrimSpace(string(b)); cur != "" {
			return cur, nil
		}
	}
	return id, writeAtomic(f, []byte(id)) // file rỗng/hỏng: ghi đè như Java
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Sender là git config user.name; thiếu hoặc lỗi thì "unknown".
func Sender(git *gitx.Repo) string {
	n, err := git.UserName(context.Background())
	if err != nil || strings.TrimSpace(n) == "" {
		return "unknown"
	}
	return n
}

// LastOK là thời điểm gửi thành công cuối (.aisdlc/web/last-ok), nếu có.
func LastOK(root string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(WebDir(root), "last-ok"))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}
