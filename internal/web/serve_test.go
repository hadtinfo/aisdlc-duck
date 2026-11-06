package web

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"aisdlc/internal/web/board"
)

func startServe(t *testing.T, h http.Handler, hub *board.Hub, timeout time.Duration) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- serve(ctx, srv, ln, hub, timeout) }()
	t.Cleanup(cancel)
	return "http://" + ln.Addr().String(), cancel, errc
}

// TestSseShutdownClosesStreams: hủy ctx thì Shutdown đóng ngay các stream SSE đang mở (hub.Close qua
// RegisterOnShutdown), không chờ hết hạn tắt; client nhận EOF sạch.
func TestSseShutdownClosesStreams(t *testing.T) {
	hub := board.NewHub(nil)
	r := chi.NewRouter()
	r.Use(Recoverer) // SSE đi qua Recoverer như server thật (Flush qua Unwrap)
	r.Get("/api/stream", hub.ServeBoard)
	base, cancel, errc := startServe(t, r, hub, 30*time.Second)

	var bodies []io.ReadCloser
	for i := 0; i < 3; i++ {
		res, err := http.Get(base + "/api/stream")
		if err != nil {
			t.Fatal(err)
		}
		l, err := bufio.NewReader(res.Body).ReadString('\n')
		if err != nil || l != ":ok\n" {
			t.Fatalf("%q %v", l, err)
		}
		bodies = append(bodies, res.Body)
	}
	if hub.Clients() != 3 {
		t.Fatal(hub.Clients())
	}
	start := time.Now()
	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown treo vì stream SSE")
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("Shutdown mất %v", el)
	}
	for _, b := range bodies {
		if _, err := io.ReadAll(b); err != nil {
			t.Fatal("stream không kết thúc sạch:", err)
		}
		b.Close()
	}
	if hub.Clients() != 0 {
		t.Fatal(hub.Clients())
	}
}

// TestShutdownTimeoutForcesClose: request không chịu kết thúc thì quá hạn Shutdown sẽ Close cưỡng bức, serve trả lỗi.
func TestShutdownTimeoutForcesClose(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	started := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_ = http.NewResponseController(w).Flush()
		close(started)
		select {
		case <-block:
		case <-r.Context().Done():
		}
	})
	base, cancel, errc := startServe(t, h, nil, 200*time.Millisecond)
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		res, err := http.Get(base)
		if err == nil {
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
	}()
	<-started
	cancel()
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "quá hạn") {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve không trả về sau khi quá hạn")
	}
	select {
	case <-clientDone: // Close cưỡng bức đã cắt kết nối còn treo
	case <-time.After(2 * time.Second):
		t.Fatal("kết nối còn mở sau khi quá hạn tắt")
	}
}

// TestRecoverAfterResponseStartedNo500: panic sau khi phản hồi đã bắt đầu thì không ghi thêm 500/JSON; kết nối bị hủy.
func TestRecoverAfterResponseStartedNo500(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, ":ok\n\n")
		_ = http.NewResponseController(w).Flush()
		panic("boom")
	}))
	base, _, _ := startServe(t, h, nil, time.Second)
	res, err := http.Get(base)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, rerr := io.ReadAll(res.Body)
	if res.StatusCode != 200 || strings.Contains(string(b), "internal") || !strings.HasPrefix(string(b), ":ok") {
		t.Fatalf("%d %q", res.StatusCode, b)
	}
	if rerr == nil {
		t.Fatal("kết nối phải bị hủy (body chunked không kết thúc đúng)")
	}
}
