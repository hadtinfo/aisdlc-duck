package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"aisdlc/internal/fsx"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/step"
	"aisdlc/internal/webclient"
)

// Điều khiển từ web (WebControl và ControlLedger của Java): tạm dừng, chỉ dẫn và lệnh chờ của một tính năng.
// Cache phong bì ký .aisdlc/web/control/<KEY>.json; sổ cục bộ .aisdlc/web/control/<KEY>.local.json; cả hai đọc-ghi
// dưới khóa .aisdlc/web/control/control.lock. Chỉ Refresh gọi mạng; hook chỉ đọc cache và sổ.

// hookLockBudget: hook chờ control.lock tối đa chừng này (không treo tới timeout của Claude Code); lệnh CLI chờ mãi.
const hookLockBudget = time.Second

// controlRetry: khoảng thử lại khóa khi có hạn chờ.
const controlRetry = 25 * time.Millisecond

// lockBudget là hạn chờ control.lock của lệnh: hook (Offline) ~1 giây, lệnh CLI 0 (chờ mãi).
func (c *Context) lockBudget() time.Duration {
	if c.network {
		return 0
	}
	return hookLockBudget
}

// ControlBusy: không lấy được control.lock trong thời hạn (tiến trình khác giữ: treo, bị dừng...).
type ControlBusy struct{}

func (*ControlBusy) Error() string {
	return "không khóa được .aisdlc/web/control/control.lock: tiến trình aisdlc khác đang giữ"
}

// isControlBusy: err là (hoặc bọc) ControlBusy.
func isControlBusy(err error) bool {
	var b *ControlBusy
	return errors.As(err, &b)
}

func controlDir(root string) string { return filepath.Join(webclient.WebDir(root), "control") }

// ControlInbox là cờ .aisdlc/web/control/inbox: có khi tính năng đang làm còn chỉ dẫn chưa giao.
func ControlInbox(root string) string { return filepath.Join(controlDir(root), "inbox") }

// controlLocked chạy body dưới khóa control.lock của repo (cache điều khiển và sổ cục bộ dùng chung). flock gắn với
// từng lần mở file nên hai goroutine trong một tiến trình cũng loại trừ nhau. budget > 0: thử khóa tới hết hạn rồi
// trả *ControlBusy; budget 0: chờ mãi.
func controlLocked(root string, budget time.Duration, body func() error) error {
	dir := controlDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "control.lock"), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	fd := int(f.Fd())
	if budget <= 0 {
		for {
			err = syscall.Flock(fd, syscall.LOCK_EX)
			if !errors.Is(err, syscall.EINTR) {
				break
			}
		}
		if err != nil {
			return err
		}
	} else {
		deadline := time.Now().Add(budget)
		for {
			err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				break
			}
			if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
				return err
			}
			if !time.Now().Before(deadline) {
				return &ControlBusy{}
			}
			time.Sleep(controlRetry)
		}
	}
	defer func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }()
	return body()
}

// ---- sổ cục bộ ----

// ControlLedger là sổ cục bộ {"delivered":[id...],"results":{"<id>":"APPLIED|STALE"}} của một tính năng: mỗi chỉ
// dẫn giao tối đa một lần trên máy này, mỗi lệnh chờ chỉ áp dụng và báo một lần.
type ControlLedger struct {
	root, file string
	budget     time.Duration
}

// NewControlLedger: budget là hạn chờ control.lock (hook ~1 giây) rồi *ControlBusy; 0 = chờ mãi.
func NewControlLedger(root string, key model.FeatureKey, budget time.Duration) *ControlLedger {
	return &ControlLedger{root: root, file: filepath.Join(controlDir(root), key.String()+".local.json"),
		budget: budget}
}

type ledgerState struct {
	delivered []int64
	results   []ledgerResult // theo thứ tự ghi (LinkedHashMap của Java)
}

type ledgerResult struct {
	id     int64
	status string
}

func (s *ledgerState) isDelivered(id int64) bool {
	for _, d := range s.delivered {
		if d == id {
			return true
		}
	}
	return false
}

func (s *ledgerState) done() map[int64]bool {
	m := make(map[int64]bool, len(s.results))
	for _, r := range s.results {
		m[r.id] = true
	}
	return m
}

func (s *ledgerState) put(id int64, status string) {
	for i := range s.results {
		if s.results[i].id == id {
			s.results[i].status = status
			return
		}
	}
	s.results = append(s.results, ledgerResult{id, status})
}

// Delivered là id các chỉ dẫn đã giao trên máy này.
func (l *ControlLedger) Delivered() map[int64]bool {
	s := l.read()
	m := make(map[int64]bool, len(s.delivered))
	for _, id := range s.delivered {
		m[id] = true
	}
	return m
}

// Done là id các lệnh chờ đã xử lý (APPLIED hoặc STALE) trên máy này.
func (l *ControlLedger) Done() map[int64]bool { s := l.read(); return s.done() }

// Claim (dưới khóa) trả các chỉ dẫn chưa giao trong candidates và đánh dấu đã giao (một lần duy nhất).
func (l *ControlLedger) Claim(candidates []snapshot.Instruction) ([]snapshot.Instruction, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	var out []snapshot.Instruction
	err := controlLocked(l.root, l.budget, func() error {
		s := l.read()
		for _, i := range candidates {
			if !s.isDelivered(i.ID) {
				s.delivered = append(s.delivered, i.ID)
				out = append(out, i)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return l.write(s)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RecordResults ghi kết quả lệnh chờ vào sổ (dưới khóa).
func (l *ControlLedger) RecordResults(results []step.PendingResult) error {
	if len(results) == 0 {
		return nil
	}
	return controlLocked(l.root, l.budget, func() error {
		s := l.read()
		for _, r := range results {
			s.put(r.ID, r.Status)
		}
		return l.write(s)
	})
}

// Apply (dưới khóa): apply nhận tập lệnh đã xử lý và trả kết quả các lệnh vừa xử lý; kết quả được ghi vào sổ trước
// khi nhả khóa (hai tiến trình không áp dụng cùng một lệnh hai lần).
func (l *ControlLedger) Apply(apply func(done map[int64]bool) []step.PendingResult) ([]step.PendingResult, error) {
	var res []step.PendingResult
	err := controlLocked(l.root, l.budget, func() error {
		s := l.read()
		res = apply(s.done())
		if len(res) == 0 {
			return nil
		}
		for _, r := range res {
			s.put(r.ID, r.Status)
		}
		return l.write(s)
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// read: file hỏng hoặc không có → sổ rỗng; phần tử hỏng bỏ qua.
func (l *ControlLedger) read() *ledgerState {
	s := &ledgerState{}
	b, err := os.ReadFile(l.file)
	if err != nil {
		return s
	}
	var raw struct {
		Delivered []json.RawMessage `json:"delivered"`
		Results   json.RawMessage   `json:"results"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return s
	}
	for _, v := range raw.Delivered {
		var n json.Number
		if json.Unmarshal(v, &n) != nil {
			continue
		}
		if id, err := n.Int64(); err == nil && !s.isDelivered(id) {
			s.delivered = append(s.delivered, id)
		}
	}
	// giữ thứ tự khóa của object results
	dec := json.NewDecoder(bytes.NewReader(raw.Results))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return s
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return s
		}
		k, _ := t.(string)
		var v any
		if dec.Decode(&v) != nil {
			return s
		}
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			continue // khóa hỏng: bỏ qua
		}
		s.put(id, jsonText(v))
	}
	return s
}

func (l *ControlLedger) write(s *ledgerState) error {
	b := []byte(`{"delivered":[`)
	for i, id := range s.delivered {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendInt(b, id, 10)
	}
	b = append(b, `],"results":{`...)
	for i, r := range s.results {
		if i > 0 {
			b = append(b, ',')
		}
		k, _ := json.Marshal(strconv.FormatInt(r.id, 10))
		v, err := jsonMarshal(r.status)
		if err != nil {
			return err
		}
		b = append(append(append(b, k...), ':'), v...)
	}
	b = append(b, "}}"...)
	return fsx.WriteAtomic(l.file, b, 0o644)
}

// ---- WebControl ----

// WebControl là điều khiển từ web của repo (dùng khóa ghim, định danh repo, cấu hình và backoff của WebCache).
type WebControl struct{ w *WebCache }

// NewWebControl tạo WebControl trên WebCache của repo.
func NewWebControl(w *WebCache) *WebControl { return &WebControl{w: w} }

// Cached: phong bì đã xác minh (khóa ghim, đúng repo và tính năng) hoặc nil. Không gọi mạng.
func (c *WebControl) Cached(key model.FeatureKey) *snapshot.FeatureControl { return c.w.Control(key) }

// Unreachable: web không với tới được trong tiến trình này hoặc đang backoff.
func (c *WebControl) Unreachable() bool { return c.w.unreachable() }

// Refresh: GET /api/cli/control (2 giây) → xác minh → ghi cache khi issuedAt mới ≥ cũ → cập nhật cờ inbox. False khi
// chưa đăng nhập, web không với tới (bật backoff) hoặc phản hồi không xác minh được (cache cũ giữ nguyên).
func (c *WebControl) Refresh(key model.FeatureKey) bool {
	cfg, ok := c.w.canCall()
	if !ok {
		return false
	}
	r, err := c.w.do(http.MethodGet, "/api/cli/control?repo="+enc(c.w.RepoID())+"&feature="+enc(key.String()),
		cfg.Token, nil, webReadTimeout)
	if err != nil {
		c.w.down()
		return false
	}
	if !r.OK() {
		return false
	}
	if c.accept(key, r.Body) {
		return true
	}
	// web đã đổi khóa: chỉ nhận khóa mới khi khóa đang ghim xác nhận (như trạng thái cổng)
	rotated, err := c.w.rotate(cfg, r.Body)
	if err != nil {
		c.w.down()
		return false
	}
	return rotated && c.accept(key, r.Body)
}

// Undelivered là chỉ dẫn trong cache chưa giao trên máy này.
func (c *WebControl) Undelivered(key model.FeatureKey) []snapshot.Instruction {
	fc := c.Cached(key)
	if fc == nil || len(fc.Instructions) == 0 {
		return nil
	}
	delivered := NewControlLedger(c.w.root, key, 0).Delivered()
	var out []snapshot.Instruction
	for _, i := range fc.Instructions {
		if !delivered[i.ID] {
			out = append(out, i)
		}
	}
	return out
}

// UpdateInbox cập nhật cờ inbox theo chỉ dẫn chưa giao của tính năng đang làm (dưới control.lock, cùng khóa với
// Claim và ghi cache); tính năng khác không đụng tới cờ. Lỗi im lặng (cờ chỉ để tăng tốc).
func (c *WebControl) UpdateInbox(key model.FeatureKey) {
	_ = controlLocked(c.w.root, c.w.ctx.lockBudget(), func() error {
		c.updateInboxLocked(key)
		return nil
	})
}

func (c *WebControl) updateInboxLocked(key model.FeatureKey) {
	active, ok, err := model.ReadActive(c.w.root)
	if err != nil || !ok || active != key {
		return
	}
	f := ControlInbox(c.w.root)
	if len(c.Undelivered(key)) == 0 {
		_ = os.Remove(f)
	} else if !exists(f) {
		_ = fsx.WriteAtomic(f, nil, 0o644)
	}
}

func (c *WebControl) accept(key model.FeatureKey, envelope []byte) bool {
	payload, ok := c.w.verified(envelope)
	if !ok {
		return false
	}
	next, err := snapshot.FeatureControlFromJSON(payload)
	if err != nil || next.Feature != key.String() || c.w.RepoID() == "" || next.Repo != c.w.RepoID() {
		return false
	}
	err = controlLocked(c.w.root, c.w.ctx.lockBudget(), func() error {
		cur := c.Cached(key)
		older := cur != nil && !cur.IssuedAt.IsZero() && (next.IssuedAt.IsZero() || next.IssuedAt.Before(cur.IssuedAt))
		if !older {
			if err := fsx.WriteAtomic(snapshot.ControlCacheFile(c.w.root, key), envelope, 0o644); err != nil {
				return err
			}
		}
		c.updateInboxLocked(key)
		return nil
	})
	return err == nil
}
