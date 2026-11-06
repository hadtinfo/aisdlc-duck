package webclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/webclient"
)

func TestGetAndPostJSONSendBearer(t *testing.T) {
	var auth, ctype, method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, ctype, method = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.Method
		_, _ = w.Write([]byte(`{"a":1}`))
	}))
	defer srv.Close()
	c := &webclient.Client{BaseURL: srv.URL, Token: "tok-123"}
	var out struct{ A int }
	if err := c.GetJSON(context.Background(), "/x", &out); err != nil || out.A != 1 || auth != "Bearer tok-123" {
		t.Fatalf("get: %v %+v %q", err, out, auth)
	}
	if err := c.PostJSON(context.Background(), "/x", map[string]int{"k": 1}, &out); err != nil ||
		method != "POST" || ctype != "application/json" {
		t.Fatalf("post: %v %s %s", err, method, ctype)
	}
}

func TestHTTPErrorNeverContainsToken(t *testing.T) {
	const tok = "SECRET-TOKEN-xyz"
	for _, status := range []int{401, 403, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// server tồi phản chiếu cả header vào lỗi
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"bad ` + r.Header.Get("Authorization") + `"}`))
		}))
		c := &webclient.Client{BaseURL: srv.URL, Token: tok}
		err := c.GetJSON(context.Background(), "/p", nil)
		var he *webclient.HTTPError
		if !errors.As(err, &he) || he.Status != status {
			t.Fatalf("%d: %v", status, err)
		}
		if strings.Contains(err.Error(), tok) || strings.Contains(he.Reason, tok) {
			t.Errorf("%d: lỗi chứa token: %v", status, err)
		}
		if !strings.Contains(err.Error(), "HTTP") {
			t.Errorf("%d: %v", status, err)
		}
		srv.Close()
	}
	// lỗi mạng cũng không lộ token
	c := &webclient.Client{BaseURL: "http://127.0.0.1:1", Token: tok}
	if err := c.PostJSON(context.Background(), "/p", map[string]string{}, nil); err == nil || strings.Contains(err.Error(), tok) {
		t.Errorf("lỗi mạng: %v", err)
	}
}

func TestHTTPErrorMessage(t *testing.T) {
	e := &webclient.HTTPError{Status: 409, Reason: "Cổng trước chưa được duyệt"}
	if e.Error() != "Web trả HTTP 409: Cổng trước chưa được duyệt" {
		t.Error(e.Error())
	}
	if (&webclient.HTTPError{Status: 500}).Error() != "Web trả HTTP 500" {
		t.Error("không lý do")
	}
}

func TestDeadlineOnHangingServer(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	c := &webclient.Client{BaseURL: srv.URL, Token: "t"}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := c.GetJSON(ctx, "/slow", nil); err == nil {
		t.Fatal("phải lỗi")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("không tôn trọng deadline: %v", d)
	}
}

func TestInvalidURL(t *testing.T) {
	c := &webclient.Client{BaseURL: "://bad"}
	if err := c.GetJSON(context.Background(), "/x", nil); err == nil || !strings.Contains(err.Error(), "URL không hợp lệ") {
		t.Error(err)
	}
}
