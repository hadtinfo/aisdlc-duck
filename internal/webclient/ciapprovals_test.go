package webclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"aisdlc/internal/gate"
)

type ciSrv struct {
	*httptest.Server
	mu          sync.Mutex
	auth, query string
}

func (s *ciSrv) got() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auth, s.query
}

func startCI(t *testing.T, status int, body string) *ciSrv {
	t.Helper()
	s := &ciSrv{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.auth, s.query = r.Header.Get("Authorization"), r.URL.RawQuery
		s.mu.Unlock()
		if r.URL.Path != "/api/ci/approvals" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func ciSource(url, token, repo string) *HTTPCIApprovals {
	return &HTTPCIApprovals{BaseURL: url + "/", Token: token, Repo: func(context.Context) string { return repo }}
}

func TestCiParsesApprovalsAndSendsBearer(t *testing.T) {
	s := startCI(t, 200, `{"mode":"web","approvals":[{"gate":"REVIEW","hash":"h1","approver":"dev","at":"2026-11-03T01:00:00Z","evaluatedTree":"t1"},{"gate":"BOGUS","hash":"x"}]}`)
	r, err := ciSource(s.URL, "sekret", "host/g/r").Approvals(context.Background(), "PAY-1", "tree")
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != "web" || len(r.Approvals) != 1 || r.Approvals[gate.REVIEW].Hash != "h1" || r.EvaluatedTree[gate.REVIEW] != "t1" {
		t.Errorf("%+v", r)
	}
	auth, q := s.got()
	if auth != "Bearer sekret" || q != "repo=host%2Fg%2Fr&feature=PAY-1" {
		t.Errorf("auth %q query %q", auth, q)
	}
}

func TestCiParsesAutoChannelAndRule(t *testing.T) {
	s := startCI(t, 200, `{"mode":"web","pinned":true,"approvals":[
{"gate":"BRD","hash":"h1","approver":"auto","at":"2026-11-03T01:00:00Z","evaluatedTree":null,"channel":"auto","rule":"flow:low"},
{"gate":"DESIGN","hash":"h2","approver":"dev","at":"2026-11-03T01:00:00Z","evaluatedTree":null,"channel":"web","rule":null},
{"gate":"CASES","hash":"h3","approver":"auto","at":"2026-11-03T01:00:00Z","evaluatedTree":null,"channel":"web","rule":"flow:low"}]}`)
	r, err := ciSource(s.URL, "t", "r").Approvals(context.Background(), "F-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if b := r.Approvals[gate.BRD]; !b.IsAuto() || b.Rule != "flow:low" {
		t.Errorf("BRD %+v", b)
	}
	if d := r.Approvals[gate.DESIGN]; d.IsAuto() || d.Rule != "" {
		t.Errorf("DESIGN %+v", d)
	}
	// chỉ channel quyết định bản tự duyệt: người duyệt tên "auto" qua web không phải bản tự duyệt
	if c := r.Approvals[gate.CASES]; c.IsAuto() || c.Rule != "" {
		t.Errorf("CASES %+v", c)
	}
}

func TestCiParsesPinned(t *testing.T) {
	for _, c := range []struct {
		body   string
		pinned bool
	}{
		{`{"mode":"local","pinned":true,"approvals":[]}`, true},
		// server cũ không có "pinned": local là mặc định (chưa ghim), web thì chắc chắn đã ghim
		{`{"mode":"local","approvals":[]}`, false},
		{`{"mode":"web","approvals":[]}`, true},
	} {
		s := startCI(t, 200, c.body)
		r, err := ciSource(s.URL, "t", "r").Approvals(context.Background(), "F-1", "")
		if err != nil || r.Pinned != c.pinned {
			t.Errorf("%s: pinned=%v err=%v", c.body, r.Pinned, err)
		}
	}
}

func TestCiNon200IsErrorWithoutToken(t *testing.T) {
	s := startCI(t, 401, `{}`)
	_, err := ciSource(s.URL, "sekret", "r").Approvals(context.Background(), "F-1", "")
	if err == nil || err.Error() != "HTTP 401" {
		t.Errorf("err = %v", err)
	}
}

func TestCiUnreachableIsErrorWithoutToken(t *testing.T) {
	_, err := ciSource("http://127.0.0.1:1", "sekret", "r").Approvals(context.Background(), "F-1", "")
	if err == nil || strings.Contains(err.Error(), "sekret") {
		t.Errorf("err = %v", err)
	}
}

func TestCiBadBodyIsUnreadable(t *testing.T) {
	s := startCI(t, 200, `{"approvals":[{"gate":"CASES","at":"không phải ngày"}]}`)
	_, err := ciSource(s.URL, "sekret", "r").Approvals(context.Background(), "F-1", "")
	if err == nil || err.Error() != "phản hồi web không đọc được" {
		t.Errorf("err = %v", err)
	}
}
