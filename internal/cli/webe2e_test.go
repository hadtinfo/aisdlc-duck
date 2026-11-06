package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/web"
	"aisdlc/internal/web/signing"
	"aisdlc/internal/web/webtest"
	"aisdlc/internal/webclient"
)

// TestCliAgainstGoServer chạy CLI (trong tiến trình, HOME tạm, repo git thật) trên server web Go thật (Postgres qua
// Docker): web login, nộp BRD, lead duyệt trên web, CLI thấy approval, web push lên bảng, web tạm dừng chặn hook,
// eval --ci lấy approval bằng token CI. Thay bước web của e2e/run-e2e.sh của Java ở mức test Go.
func TestCliAgainstGoServer(t *testing.T) {
	r := newTestRepo(t)
	home := t.TempDir()
	r.env["HOME"] = home

	// 1. server thật; user lead (token login) → phiên; token ingest của lead; token ci
	keys, err := signing.Load(filepath.Join(t.TempDir(), "signing-key"), "")
	must(t, err)
	env := webtest.New(t, func(d web.Deps) http.Handler {
		d.Keys = keys
		return web.NewHandler(d)
	})
	r.now = *env.Now // CLI và server cùng đồng hồ
	loginTok := webtest.CliToken(t, env.Pool, "add", "lead", "Lead Một", "--email", "dev@example.com", "--role", "lead")
	ciTok := webtest.CliToken(t, env.Pool, "ci-token", "e2e")
	hc := env.Client(t)
	lead := hc.Session(loginTok)
	_, ingestTok := hc.CreateToken(lead, "ingest", "cli")

	// 2. web login: URL hiện chữ, token qua ô nhập ẩn (như pty của e2e Java)
	if code := r.runC(loginConsole{env.Server.URL, ingestTok}, "web", "login"); code != 0 {
		t.Fatalf("web login: exit %d\n%s%s", code, r.out, r.err)
	}
	notContains(t, r.out+r.err, ingestTok)
	eq(t, webclient.LoadPinnedKey(home).Equal(keys.Public()), true)

	// 3. setup chế độ web, init, submit brd; snapshot lên web (web flush như hook nền), lead duyệt bằng phiên + CSRF
	r.ok(noConsole(), "setup", "--approvals", "web")
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "setup")
	r.gitCmd("tag", "base")
	r.ok(noConsole(), "init", "PAY-1")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	r.ok(noConsole(), "web", "flush")
	repoID := NewWebCache(r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), r.root).RepoID()
	eq(t, repoID, "local/"+filepath.Base(r.root))
	hash := gateHashOnServer(t, hc, lead, repoID, "BRD")
	body, _ := json.Marshal(map[string]string{"repo": repoID, "feature": "PAY-1", "gate": "BRD", "hash": hash})
	if res := hc.PostSession("/api/gates/approve", string(body), lead, lead.CSRF); res.Status != 200 {
		t.Fatalf("approve %d %s", res.Status, res.Body)
	}

	// 4. CLI làm mới cache và thấy BRD đã duyệt (kênh web)
	r.ok(noConsole(), "status", "--json")
	var st statusJSON
	must(t, json.Unmarshal([]byte(r.out), &st))
	eq(t, st.Gates["BRD"], "APPROVED")
	wc := NewWebCache(r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}), r.root)
	a, ok, err := wc.Store("PAY-1").Approval(gate.BRD)
	must(t, err)
	eq(t, ok, true)
	eq(t, a.Channel, "web")
	eq(t, a.Approver, "lead")
	eq(t, a.Hash, hash)
	eq(t, r.exists("aisdlc/features/PAY-1/gates.json"), false)

	// 5. web push → thẻ PAY-1 của repo trên bảng
	r.ok(noConsole(), "web", "push")
	board := hc.GetSession("/api/board", lead)
	eq(t, board.Status, 200)
	if !hasCard(t, board.Body, repoID, "PAY-1") {
		t.Fatalf("bảng không có thẻ %s PAY-1: %s", repoID, board.Body)
	}

	// 6. lead tạm dừng trên web → web flush làm mới điều khiển → pre-tool-use bị chặn với thông báo tạm dừng
	pb, _ := json.Marshal(map[string]any{"repo": repoID, "feature": "PAY-1", "paused": true})
	if res := hc.PostSession("/api/control/pause", string(pb), lead, lead.CSRF); res.Status != 200 {
		t.Fatalf("pause %d %s", res.Status, res.Body)
	}
	r.ok(noConsole(), "web", "flush")
	r.hook("pre-tool-use", r.codeEdit())
	o := hookSpecific(t, r.out)
	eq(t, str(o, "permissionDecision"), "deny")
	eq(t, str(o, "permissionDecisionReason"),
		"Dev đã tạm dừng tại web: lead. Dừng lại và chờ. Khi dev báo tiếp tục thì thử lại.")

	// 7. eval --ci với token CI hỏi approval trên web: BRD khớp, DESIGN chưa duyệt
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "PAY-1")
	r.allCasesPass()
	r.env["AISDLC_WEB_URL"] = env.Server.URL
	r.env["AISDLC_CI_TOKEN"] = ciTok
	eq(t, r.run("eval", "--ci", "--base", "base"), 1)
	contains(t, r.out, "| GATE:BRD | ĐẠT | đã duyệt trên web, khớp nội dung |", "chưa duyệt DESIGN trên web")
	notContains(t, r.out, "GATE:WEB")
	notContains(t, r.out+r.err, ciTok, ingestTok)
}

// gateHashOnServer là hash bản nộp đang chờ của cổng g, đọc từ GET /api/features/<repo>/PAY-1 bằng phiên.
func gateHashOnServer(t *testing.T, hc *webtest.Client, s webtest.Session, repoID, g string) string {
	t.Helper()
	res := hc.GetSession("/api/features/"+repoID+"/PAY-1", s)
	if res.Status != 200 {
		t.Fatalf("feature %d %s", res.Status, res.Body)
	}
	var m struct {
		Snapshot struct {
			Gates []struct {
				Gate, State, CurrentHash string
			} `json:"gates"`
		} `json:"snapshot"`
	}
	must(t, json.Unmarshal([]byte(res.Body), &m))
	for _, x := range m.Snapshot.Gates {
		if x.Gate == g && x.CurrentHash != "" {
			return x.CurrentHash
		}
	}
	t.Fatalf("không thấy hash cổng %s: %s", g, res.Body)
	return ""
}

func hasCard(t *testing.T, body, repoID, key string) bool {
	t.Helper()
	var b struct {
		Cards []struct{ RepoID, Key string } `json:"cards"`
	}
	must(t, json.Unmarshal([]byte(body), &b))
	for _, c := range b.Cards {
		if c.RepoID == repoID && c.Key == key {
			return true
		}
	}
	return false
}
