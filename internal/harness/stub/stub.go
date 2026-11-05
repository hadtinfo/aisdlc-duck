// Package stub là stub server HTTP, tập con của WireMock, thay WireMock trong harness.
package stub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"aisdlc/internal/spec"
)

// Server là stub server lắng nghe 127.0.0.1:0.
type Server struct {
	srv   *http.Server
	ln    net.Listener
	mu    sync.RWMutex
	stubs []spec.StubSpec
}

// Start khởi động server.
func Start() (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{ln: ln}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.handle)}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

// URL là địa chỉ gốc, ví dụ http://127.0.0.1:12345.
func (s *Server) URL() string { return "http://" + s.ln.Addr().String() }

// Close dừng server.
func (s *Server) Close() { _ = s.srv.Close() }

// Register xóa mọi stub rồi đăng ký các stub có id trong ids (lấy từ all).
func (s *Server) Register(all []spec.StubSpec, ids []string) error {
	var picked []spec.StubSpec
	for _, id := range ids {
		found := false
		for _, st := range all {
			if st.ID == id {
				picked = append(picked, st)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("stub không tồn tại: %s", id)
		}
	}
	s.mu.Lock()
	s.stubs = picked
	s.mu.Unlock()
	return nil
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.RLock()
	stubs := s.stubs
	s.mu.RUnlock()
	for i := len(stubs) - 1; i >= 0; i-- {
		if matches(stubs[i].Request, r, body) {
			respond(w, stubs[i].Response)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(404)
	b, _ := json.Marshal(map[string]string{"error": "không có stub khớp", "method": r.Method, "url": r.URL.RequestURI()})
	_, _ = w.Write(b)
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func fullMatch(pattern, s string) bool {
	re, err := regexp.Compile("^(?:" + pattern + ")$")
	return err == nil && re.MatchString(s)
}

func valueMatches(matcher any, got string) bool {
	m, _ := matcher.(map[string]any)
	if v, ok := m["equalTo"]; ok {
		return got == str(v)
	}
	if v, ok := m["matches"]; ok {
		return fullMatch(str(v), got)
	}
	return false
}

func matches(req map[string]any, r *http.Request, body []byte) bool {
	if m, ok := req["method"]; ok {
		if ms := strings.ToUpper(str(m)); ms != "ANY" && ms != r.Method {
			return false
		}
	}
	if v, ok := req["url"]; ok && r.URL.RequestURI() != str(v) {
		return false
	}
	if v, ok := req["urlPath"]; ok && r.URL.Path != str(v) {
		return false
	}
	if v, ok := req["urlPathPattern"]; ok && !fullMatch(str(v), r.URL.Path) {
		return false
	}
	if qp, ok := req["queryParameters"].(map[string]any); ok {
		q, _ := url.ParseQuery(r.URL.RawQuery)
		for name, mt := range qp {
			vals, present := q[name]
			if !present || !valueMatches(mt, vals[0]) {
				return false
			}
		}
	}
	if hs, ok := req["headers"].(map[string]any); ok {
		for name, mt := range hs {
			vals := r.Header.Values(name)
			if len(vals) == 0 || !valueMatches(mt, vals[0]) {
				return false
			}
		}
	}
	if bp, ok := req["bodyPatterns"].([]any); ok {
		for _, p := range bp {
			pm, _ := p.(map[string]any)
			if v, ok := pm["contains"]; ok {
				if !bytes.Contains(body, []byte(str(v))) {
					return false
				}
			} else if v, ok := pm["equalToJson"]; ok {
				if !jsonEqual(str(v), body) {
					return false
				}
			} else {
				return false
			}
		}
	}
	return true
}

func jsonEqual(expected string, actual []byte) bool {
	var a, b any
	if json.Unmarshal([]byte(expected), &a) != nil || json.Unmarshal(actual, &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func respond(w http.ResponseWriter, resp map[string]any) {
	if d, ok := toInt(resp["fixedDelayMilliseconds"]); ok && d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
	hasCT := false
	if hs, ok := resp["headers"].(map[string]any); ok {
		for k, v := range hs {
			w.Header().Set(k, str(v))
			if strings.EqualFold(k, "Content-Type") {
				hasCT = true
			}
		}
	}
	var out []byte
	if jb, ok := resp["jsonBody"]; ok {
		out, _ = json.Marshal(jb)
		if !hasCT {
			w.Header().Set("Content-Type", "application/json")
		}
	} else if b, ok := resp["body"]; ok {
		out = []byte(str(b))
	}
	status := 200
	if n, ok := toInt(resp["status"]); ok {
		status = n
	}
	w.WriteHeader(status)
	_, _ = w.Write(out)
}
