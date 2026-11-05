package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"aisdlc/internal/spec"
)

// DefaultRequestTimeout là hạn mỗi request khi không cấu hình khác.
const DefaultRequestTimeout = 60 * time.Second

// Response là phản hồi HTTP.
type Response struct {
	Status int
	Body   string
}

// Caller gọi dịch vụ theo phần when của case.
type Caller interface {
	Call(ctx context.Context, w spec.When) (Response, error)
	Timeout() time.Duration
}

// HTTPCaller gọi dịch vụ đang chạy ở BaseURL; body của case được serialize JSON.
type HTTPCaller struct {
	BaseURL        string
	RequestTimeout time.Duration // 0: 60s
}

// Timeout là hạn của mỗi request.
func (h *HTTPCaller) Timeout() time.Duration {
	if h.RequestTimeout <= 0 {
		return DefaultRequestTimeout
	}
	return h.RequestTimeout
}

// Call gửi một request; Content-Type là application/json nếu case không đặt.
func (h *HTTPCaller) Call(ctx context.Context, w spec.When) (Response, error) {
	var body io.Reader
	if w.Body != nil {
		b, err := json.Marshal(w.Body)
		if err != nil {
			return Response{}, err
		}
		body = bytes.NewReader(b)
	}
	path := w.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(strings.TrimSpace(w.Method)),
		strings.TrimRight(h.BaseURL, "/")+path, body)
	if err != nil {
		return Response{}, err
	}
	hasCT := false
	for k, v := range w.Headers {
		req.Header.Set(k, v)
		hasCT = hasCT || strings.EqualFold(k, "Content-Type")
	}
	if !hasCT {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := (&http.Client{Timeout: h.Timeout()}).Do(req)
	if err != nil {
		return Response{}, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return Response{}, err
	}
	return Response{Status: res.StatusCode, Body: string(b)}, nil
}
