// Package fakeweb là server web giả cho test của web client (port FakeWeb.java): httptest, ghi lại mọi request,
// cho kịch bản hóa phản hồi theo "METHOD /path", ký phong bì bằng khóa Ed25519 của test. Dùng chung cho các test
// của webclient, outbox, publisher, gates và lệnh CLI.
//
// Trường công khai (Repo, Feature, State, Mode, ...) đọc/ghi qua Update/Read để an toàn với handler chạy ở
// goroutine khác; Requests() trả bản sao.
package fakeweb

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"aisdlc/internal/i18n"
	"aisdlc/internal/snapshot"
)

// T là thời điểm cố định mặc định của các phong bì.
var T = time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)

// Req là một request đã nhận.
type Req struct {
	Method, Path, Query, Auth string
	Body                      string
	AcceptLanguage            string // header Accept-Language của request
}

// JSON giải mã body vào v.
func (r Req) JSON(v any) error { return json.Unmarshal([]byte(r.Body), v) }

// Field lấy trường chuỗi cấp một của body JSON ("" nếu thiếu).
func (r Req) Field(name string) string {
	var m map[string]any
	if json.Unmarshal([]byte(r.Body), &m) != nil {
		return ""
	}
	s, _ := m[name].(string)
	return s
}

// Resp là phản hồi kịch bản. Delay: chờ trước khi trả (mô phỏng web chậm).
type Resp struct {
	Status int
	Body   []byte
	Delay  time.Duration
}

// JSONResp tạo Resp từ chuỗi JSON.
func JSONResp(status int, body string) Resp { return Resp{Status: status, Body: []byte(body)} }

// Server là web giả.
type Server struct {
	t   *testing.T
	srv *httptest.Server
	mu  sync.Mutex

	// Pub/Priv: khóa ký của server giả. Signer: khóa dùng để ký thật (đặt khác Priv để mô phỏng chữ ký sai).
	Pub    ed25519.PublicKey
	Priv   ed25519.PrivateKey
	Signer ed25519.PrivateKey
	// Endorsement: chữ ký base64 của khóa cũ trên khóa này, trả ở /api/cli/pubkey khi có (đổi khóa).
	Endorsement string

	Repo, Feature string
	State         snapshot.FeatureGateState
	Mode          string
	// ModeIssuedAt: issuedAt của phong bì chế độ.
	ModeIssuedAt time.Time
	Control      snapshot.FeatureControl
	// Override thay phản hồi theo "METHOD /path".
	Override map[string]Resp
	// Drop: đóng kết nối không trả lời (client nhận lỗi mạng). Hang: giữ kết nối tới khi server đóng.
	Drop, Hang bool
	// PolicyYAML/PolicyVersion: chính sách rủi ro của repo (GET /api/cli/policy); rỗng + version 0 → 404 tới khi import.
	PolicyYAML    string
	PolicyVersion int
	// ServerRules mô phỏng luật server: nộp cổng cần mọi cổng trước có approval; event RISK_* gỡ bản tự duyệt.
	ServerRules bool
	// AutoLevel: mức server tự duyệt bản nộp sạch (channel auto, rule flow:<mức>); "" → không tự duyệt.
	AutoLevel string

	requests []Req
	closing  chan struct{}
}

// New dựng server giả cho repo "gitlab.corp/pay/svc" và tính năng "PAY-1" (đổi qua Update). Tự đóng khi test xong.
func New(t *testing.T) *Server {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{t: t, Pub: pub, Priv: priv, Signer: priv, Mode: "web", ModeIssuedAt: T,
		Override: map[string]Resp{}, closing: make(chan struct{})}
	s.SetFeature("gitlab.corp/pay/svc", "PAY-1")
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

// SetFeature đặt repo/tính năng và đưa State, Control về trạng thái rỗng.
func (s *Server) SetFeature(repo, feature string) {
	s.Update(func(s *Server) {
		s.Repo, s.Feature = repo, feature
		s.State = snapshot.FeatureGateState{Repo: repo, Feature: feature, Mode: "web", IssuedAt: T}
		s.Control = snapshot.FeatureControl{Repo: repo, Feature: feature, IssuedAt: T}
	})
}

// Update chạy f khi giữ khóa của server (đổi trường công khai an toàn).
func (s *Server) Update(f func(*Server)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

// URL là địa chỉ gốc, không có "/" cuối.
func (s *Server) URL() string { return s.srv.URL }

// PublicKey là khóa công khai (base64 X.509) mà /api/cli/pubkey trả.
func (s *Server) PublicKey() string { return snapshot.EncodePublicKey(s.Pub) }

// KeyID của khóa công khai.
func (s *Server) KeyID() string { return snapshot.KeyID(s.Pub) }

// Envelope ký payload bằng Signer, trả JSON phong bì.
func (s *Server) Envelope(payload []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.envelopeLocked(payload)
}

func (s *Server) envelopeLocked(payload []byte) []byte {
	b, err := json.Marshal(snapshot.Sign(payload, s.Signer, snapshot.KeyID(s.Signer.Public().(ed25519.PublicKey))))
	if err != nil {
		s.t.Fatal(err)
	}
	return b
}

// StateEnvelope là phong bì ký của State hiện tại.
func (s *Server) StateEnvelope() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateEnvelopeLocked()
}

func (s *Server) stateEnvelopeLocked() []byte {
	p, err := s.State.ToJSON()
	if err != nil {
		s.t.Fatal(err)
	}
	return s.envelopeLocked(p)
}

// Requests trả bản sao mọi request đã nhận theo thứ tự.
func (s *Server) Requests() []Req {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Req(nil), s.requests...)
}

// RequestsFor lọc theo method và path.
func (s *Server) RequestsFor(method, path string) []Req {
	var out []Req
	for _, r := range s.Requests() {
		if r.Method == method && r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// Reset xóa danh sách request đã ghi.
func (s *Server) Reset() { s.Update(func(s *Server) { s.requests = nil }) }

// Close đóng server (an toàn gọi nhiều lần); nhả các request đang Hang.
func (s *Server) Close() {
	s.mu.Lock()
	select {
	case <-s.closing:
	default:
		close(s.closing)
	}
	s.mu.Unlock()
	s.srv.CloseClientConnections()
	s.srv.Close()
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	req := Req{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"), Body: string(body),
		AcceptLanguage: r.Header.Get("Accept-Language")}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	drop, hang := s.Drop, s.Hang
	o, scripted := s.Override[req.Method+" "+req.Path]
	var resp Resp
	if !drop && !hang && !scripted {
		resp = s.route(req)
	}
	closing := s.closing
	s.mu.Unlock()
	if hang {
		select {
		case <-closing:
		case <-r.Context().Done():
		}
		return
	}
	if drop {
		if hj, ok := w.(http.Hijacker); ok {
			if c, _, err := hj.Hijack(); err == nil {
				_ = c.Close()
			}
		}
		return
	}
	if scripted {
		resp = o
	}
	if resp.Delay > 0 {
		select {
		case <-time.After(resp.Delay):
		case <-closing:
			return
		case <-r.Context().Done():
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.Status)
	// nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- máy chủ web giả chỉ dùng trong test, trả JSON đã soạn sẵn
	_, _ = w.Write(resp.Body)
}

func errJSON(status int, msg string) Resp {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return Resp{Status: status, Body: b}
}

func (s *Server) route(r Req) Resp {
	key := r.Method + " " + r.Path
	switch key {
	case "GET /api/cli/pubkey":
		m := map[string]string{"publicKey": s.PublicKey(), "keyId": s.KeyID()}
		if s.Endorsement != "" {
			m["endorsement"] = s.Endorsement
		}
		b, _ := json.Marshal(m)
		return Resp{Status: 200, Body: b}
	case "POST /api/cli/mode":
		b, _ := json.Marshal(map[string]string{"repo": r.Field("repo"), "mode": s.Mode,
			"at": T.Format(time.RFC3339), "issuedAt": s.ModeIssuedAt.Format(time.RFC3339)})
		return Resp{Status: 200, Body: s.envelopeLocked(b)}
	case "GET /api/cli/control":
		p, err := s.Control.ToJSON()
		if err != nil {
			return errJSON(500, err.Error())
		}
		return Resp{Status: 200, Body: s.envelopeLocked(p)}
	case "GET /api/cli/policy":
		if s.PolicyYAML == "" {
			return errJSON(404, i18n.T("chưa có chính sách"))
		}
		b, _ := json.Marshal(map[string]any{"repoId": s.State.Repo, "version": s.PolicyVersion, "yaml": s.PolicyYAML})
		return Resp{Status: 200, Body: s.envelopeLocked(b)}
	case "POST /api/cli/policy/import":
		if s.PolicyYAML != "" {
			return JSONResp(200, fmt.Sprintf(`{"version":%d}`, s.PolicyVersion))
		}
		s.PolicyYAML, s.PolicyVersion = r.Field("yaml"), 1
		return JSONResp(201, `{"version":1}`)
	case "POST /api/cli/import", "GET /api/cli/gates":
		return Resp{Status: 200, Body: s.stateEnvelopeLocked()}
	case "POST /api/cli/submissions":
		gate, hash := r.Field("gate"), r.Field("hash")
		if s.ServerRules {
			for _, g := range []string{"BRD", "DESIGN", "CASES", "REVIEW"} {
				if g == gate {
					break
				}
				found := false
				for _, a := range s.State.Approvals {
					found = found || a.Gate == g
				}
				if !found {
					return errJSON(409, i18n.T("Cổng trước chưa được duyệt"))
				}
			}
		}
		st := s.State
		st.IssuedAt = T
		st.Submissions = append(append([]snapshot.Sub(nil), st.Submissions...), snapshot.Sub{Gate: gate, Hash: hash, At: T})
		if s.AutoLevel != "" && gate != "REVIEW" {
			st.Approvals = append(append([]snapshot.Appr(nil), st.Approvals...),
				snapshot.Appr{Gate: gate, Hash: hash, Approver: "auto", At: T, Channel: "auto", Rule: "flow:" + s.AutoLevel})
		}
		s.State = st
		return Resp{Status: 200, Body: s.stateEnvelopeLocked()}
	case "POST /api/gates/approve":
		st := s.State
		st.IssuedAt = T
		st.Approvals = append(append([]snapshot.Appr(nil), st.Approvals...),
			snapshot.Appr{Gate: r.Field("gate"), Hash: r.Field("hash"), Approver: "dev", At: T, Channel: "terminal"})
		s.State = st
		return Resp{Status: 200, Body: s.stateEnvelopeLocked()}
	}
	if s.ServerRules && r.Path == "/api/ingest/events" && strings.Contains(r.Body, `"RISK_`) {
		st := s.State
		var keep []snapshot.Appr
		for _, a := range st.Approvals {
			if a.Channel != "auto" {
				keep = append(keep, a)
			}
		}
		st.Approvals = keep
		s.State = st
	}
	if strings.HasPrefix(r.Path, "/api/ingest/") {
		return Resp{Status: 200}
	}
	return errJSON(404, i18n.Tf("không có %s", key))
}
