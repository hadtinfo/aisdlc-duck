package webclient

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/web"
	"aisdlc/internal/web/webtest"
)

// Snapshot do Build dựng và lô event v2 đi qua validator và ingest của server Go thật (giai đoạn 4a).
func TestSnapshotAndEventsAcceptedByGoServer(t *testing.T) {
	env := webtest.New(t, func(d web.Deps) http.Handler { return web.NewHandler(d) })
	token := webtest.CliToken(t, env.Pool, "add", "pub.a", "pub.a", "--email", "a@corp")

	f := newSnapFix(t)
	f.start("S1")
	f.write("src/a.txt", "a1\n")
	f.write("aisdlc/features/PAY-123/brd.md", "## Câu hỏi mở\n- [ ] Hỏi?\n<b>&</b>\n")
	f.finish("S1")
	f.do(f.gates.Reject(gate.REVIEW, "An", "Thiếu"))
	in := f.input(true)
	snap, err := BuildSnapshot(in)
	mustNil(t, err)

	p := NewPublisher(f.dir, func() (*Client, bool) { return &Client{BaseURL: env.Server.URL, Token: token}, true },
		&syncBuf{})
	errBuf := p.Err.(*syncBuf)
	p.Now = func() time.Time { return pubNow }
	p.Spawn = func() error { return nil }
	p.RepoID = func() string { return "local/x" }
	p.AfterCommand("PAY-123", func() ([]byte, error) { return snap, nil },
		Event{Type: "COMMAND", At: pubNow, Feature: "PAY-123", Sender: "An", Detail: "submit cases"})
	p.Queue(Event{Type: "FILE_EDIT", At: pubNow, Feature: "PAY-123", Step: "S1", Path: "src/a.txt", Sender: "An", SessionID: "s-1"})
	p.Queue(Event{Type: "SCOPE_DENIED", At: pubNow, Feature: "PAY-123", Step: "S1", Path: "x/y", OutOfScope: true, Sender: "An"})
	r := p.Push(context.Background())
	if r.Pending != 0 || errBuf.String() != "" {
		t.Fatalf("%+v %q", r, errBuf.String())
	}
	if n := NewOutbox(f.dir).RejectedCount(); n != 0 {
		es, _ := os.ReadDir(filepath.Join(f.dir, ".aisdlc", "web", "rejected"))
		t.Fatalf("server từ chối %d file: %v", n, es)
	}
	var key, flow string
	if err := env.Pool.QueryRow(context.Background(),
		"SELECT feature_key, snapshot->'feature'->>'flow' FROM features WHERE repo_id = 'local/x'").Scan(&key, &flow); err != nil {
		t.Fatal(err)
	}
	if key != "PAY-123" || flow != "LITE" {
		t.Fatal(key, flow)
	}
	var events int
	if err := env.Pool.QueryRow(context.Background(), "SELECT count(*) FROM events WHERE feature_key = 'PAY-123'").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 3 {
		t.Fatal(events)
	}
}
