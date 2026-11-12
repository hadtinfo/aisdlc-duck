package board

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"aisdlc/internal/web/httpx"
)

// DefaultHeartbeat là nhịp comment ":hb" (SseHub.java @Scheduled fixedRate = 20 000 ms).
const DefaultHeartbeat = 20 * time.Second

// DefaultWriteTimeout bao mỗi lần ghi khung SSE: client không đọc (TCP đầy) thì lần ghi lỗi thay vì treo goroutine.
const DefaultWriteTimeout = 30 * time.Second

// Hub phát SSE khi một tính năng đổi (SseHub.java): `card` {repoId,key} cho bảng; `event` (dòng event mới) và
// `snapshot` (synced_at đổi) cho trang tính năng; comment ":ok" khi mở và ":hb" theo nhịp Heartbeat.
//
// Mỗi stream chạy trong goroutine của request; Publish chỉ đánh thức (không chặn, không đụng socket), nên client treo
// hay lỗi không làm hỏng ingest đã commit. Stream kết thúc khi client ngắt (ctx request), khi hai lần ghi liên tiếp
// lỗi, hoặc khi Close (Shutdown).
type Hub struct {
	// Heartbeat và WriteTimeout đặt trước khi phục vụ; 0 = mặc định.
	Heartbeat    time.Duration
	WriteTimeout time.Duration

	reader  *Reader
	mu      sync.Mutex
	clients map[*client]struct{}
	done    chan struct{}
	closed  bool
}

type cardKey struct {
	RepoID string `json:"repoId"`
	Key    string `json:"key"`
}

// client là một stream; feature rỗng (repo == "") là stream bảng.
type client struct {
	board     bool
	repo, key string
	wake      chan struct{} // cap 1: gộp các lần đánh thức
	mu        sync.Mutex
	pending   []cardKey // bảng: card chờ gửi, không trùng
}

// NewHub tạo hub; reader dùng cho stream tính năng.
func NewHub(reader *Reader) *Hub {
	return &Hub{reader: reader, clients: map[*client]struct{}{}, done: make(chan struct{})}
}

// Clients là số stream đang mở.
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Publish báo tính năng (repoID, feature) vừa đổi; không chặn.
func (h *Hub) Publish(repoID, feature string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for c := range h.clients {
		if c.board {
			c.add(cardKey{repoID, feature})
		} else if c.repo == repoID && c.key == feature {
			c.signal()
		}
	}
}

// Close kết thúc mọi stream đang mở; stream mới bị từ chối (503). Gọi khi Shutdown.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.done)
	}
}

func (h *Hub) add(c *client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	c.wake = make(chan struct{}, 1)
	h.clients[c] = struct{}{}
	return true
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

func (c *client) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *client) add(k cardKey) {
	c.mu.Lock()
	dup := false
	for _, p := range c.pending {
		if p == k {
			dup = true
			break
		}
	}
	if !dup {
		c.pending = append(c.pending, k)
	}
	c.mu.Unlock()
	c.signal()
}

func (c *client) take() []cardKey {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.pending
	c.pending = nil
	return p
}

// stream ghi khung SSE: mỗi lần ghi có hạn giờ và Flush; hai lần liên tiếp lỗi thì báo gỡ client.
type stream struct {
	w       http.ResponseWriter
	rc      *http.ResponseController
	timeout time.Duration
	fails   int
}

// send ghi một khung; false = gỡ client.
func (s *stream) send(frame string) bool {
	_ = s.rc.SetWriteDeadline(time.Now().Add(s.timeout)) // ResponseRecorder không hỗ trợ: bỏ qua
	_, err := io.WriteString(s.w, frame)
	if err == nil {
		err = s.rc.Flush()
	}
	if err != nil {
		s.fails++
		return s.fails < 2
	}
	s.fails = 0
	return true
}

func (s *stream) end() { _ = s.rc.SetWriteDeadline(time.Time{}) }

func comment(c string) string { return ":" + c + "\n\n" }

// event như SseEmitter.event().name(name).data(data): mỗi dòng của data một dòng "data:".
func event(name, data string) string {
	return "event:" + name + "\ndata:" + strings.ReplaceAll(data, "\n", "\ndata:") + "\n\n"
}

func (h *Hub) open(w http.ResponseWriter) *stream {
	timeout := h.WriteTimeout
	if timeout <= 0 {
		timeout = DefaultWriteTimeout
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	return &stream{w: w, rc: http.NewResponseController(w), timeout: timeout}
}

func (h *Hub) ticker() *time.Ticker {
	d := h.Heartbeat
	if d <= 0 {
		d = DefaultHeartbeat
	}
	return time.NewTicker(d)
}

func unavailable(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": httpx.LocaleFrom(r.Context()).T("Server đang tắt")})
}

// ServeBoard là GET /api/stream: sự kiện `card` {repoId,key} mỗi khi một tính năng đổi.
func (h *Hub) ServeBoard(w http.ResponseWriter, r *http.Request) {
	c := &client{board: true}
	if !h.add(c) {
		unavailable(w, r)
		return
	}
	defer h.remove(c)
	s := h.open(w)
	defer s.end()
	if !s.send(comment("ok")) {
		return
	}
	tick := h.ticker()
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.done:
			return
		case <-tick.C:
			if !s.send(comment("hb")) {
				return
			}
		case <-c.wake:
			for _, k := range c.take() {
				b, _ := json.Marshal(k)
				if !s.send(event("card", string(b))) {
					return
				}
			}
		}
	}
}

// ServeFeature là GET /api/features/{repo}/{key}/stream: `event` cho mỗi event mới (theo id), `snapshot` (data rỗng)
// khi synced_at đổi. Người gọi đã kiểm tính năng tồn tại.
func (h *Hub) ServeFeature(w http.ResponseWriter, r *http.Request, repoID, key string) {
	c := &client{repo: repoID, key: key}
	if !h.add(c) {
		unavailable(w, r)
		return
	}
	defer h.remove(c)
	ctx := r.Context()
	// Đăng ký trước rồi mới đọc mốc: Publish đến giữa chừng chỉ làm lần đánh thức đầu không có gì mới, không lỡ event.
	lastID, err := h.reader.MaxEventID(ctx, repoID, key)
	var lastSynced time.Time
	var hasSynced bool
	if err == nil {
		lastSynced, hasSynced, err = h.reader.SyncedAt(ctx, repoID, key)
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s := h.open(w)
	defer s.end()
	if !s.send(comment("ok")) {
		return
	}
	tick := h.ticker()
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.done:
			return
		case <-tick.C:
			if !s.send(comment("hb")) {
				return
			}
		case <-c.wake:
			evs, err := h.reader.EventsAfter(ctx, repoID, key, lastID)
			if err != nil {
				log.Print(httpx.OpLocale().Tf("board: lỗi phát SSE cho %s/%s, bỏ subscriber: %v", repoID, key, err))
				return
			}
			for _, e := range evs {
				lastID = e.ID
				b, _ := json.Marshal(e)
				if !s.send(event("event", string(b))) {
					return
				}
			}
			synced, ok, err := h.reader.SyncedAt(ctx, repoID, key)
			if err != nil {
				log.Print(httpx.OpLocale().Tf("board: lỗi phát SSE cho %s/%s, bỏ subscriber: %v", repoID, key, err))
				return
			}
			if ok && (!hasSynced || !synced.Equal(lastSynced)) {
				lastSynced, hasSynced = synced, true
				if !s.send(event("snapshot", "")) {
					return
				}
			}
		}
	}
}
