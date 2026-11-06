// Package httpx chứa tiện ích HTTP dùng chung của server web: lỗi JSON, giới hạn body, principal.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// ApiError là lỗi nghiệp vụ: HTTP status + {"error": "<câu tiếng Việt>"} như ApiError.java.
// Code là mã máy đọc tùy chọn, không ghi ra JSON.
type ApiError struct {
	Status  int
	Code    string
	Message string
}

func (e *ApiError) Error() string { return e.Message }

// NewError tạo ApiError chỉ có status và thông báo.
func NewError(status int, msg string) *ApiError { return &ApiError{Status: status, Message: msg} }

// WriteJSON ghi v dạng JSON với status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("httpx: marshal: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// WriteError ghi *ApiError thành status + {"error": msg}; lỗi khác thành 500 {"error":"internal"} và ghi log.
func WriteError(w http.ResponseWriter, err error) {
	var ae *ApiError
	if errors.As(err, &ae) {
		WriteJSON(w, ae.Status, map[string]string{"error": ae.Message})
		return
	}
	log.Printf("httpx: lỗi nội bộ: %v", err)
	WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
}

// DecodeJSON đọc body thành dst. Body sai thì trả *ApiError 400 {"error":"JSON không hợp lệ"} (như BadRequestException);
// body vượt giới hạn của LimitBody thì trả *ApiError 413.
func DecodeJSON(r *http.Request, dst any) error {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		var ae *ApiError
		if errors.As(err, &ae) {
			return ae
		}
		return NewError(http.StatusBadRequest, "JSON không hợp lệ")
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return NewError(http.StatusBadRequest, "JSON không hợp lệ")
	}
	return nil
}

// TooLarge là lỗi trả về khi body vượt giới hạn.
func TooLarge() *ApiError { return NewError(http.StatusRequestEntityTooLarge, "Payload vượt 8 MB") }

const maxDrain = 256 << 20

// Drain đọc bỏ phần thân request còn lại (tối đa 256 MB) để client đang gửi nhận được phản hồi chứ không bị reset,
// như Tomcat max-swallow-size -1 của Java.
func Drain(r *http.Request) {
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, maxDrain))
	}
}

// LimitBody giới hạn body tối đa max byte. Quá giới hạn (theo Content-Length hoặc khi đọc) thì đọc bỏ phần còn lại
// (để client nhận được phản hồi chứ không bị reset) rồi trả 413 {"error":"Payload vượt 8 MB"}.
func LimitBody(max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > max {
				_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, maxDrain))
				WriteError(w, TooLarge())
				return
			}
			if r.Body != nil {
				r.Body = &limitedBody{rc: r.Body, left: max}
			}
			next.ServeHTTP(w, r)
		})
	}
}

type limitedBody struct {
	rc   io.ReadCloser
	left int64
	over bool
}

func (l *limitedBody) Read(p []byte) (int, error) {
	if l.over {
		return 0, TooLarge()
	}
	if int64(len(p)) > l.left+1 {
		p = p[:l.left+1]
	}
	n, err := l.rc.Read(p)
	if int64(n) > l.left {
		l.over = true
		_, _ = io.Copy(io.Discard, io.LimitReader(l.rc, maxDrain))
		return int(l.left), TooLarge()
	}
	l.left -= int64(n)
	return n, err
}

func (l *limitedBody) Close() error { return l.rc.Close() }

// Principal là danh tính đã xác thực của request.
type Principal struct {
	UserID int64
	Login  string
	Role   string
	Via    string // "session" | "login" | "ingest" | "ci" | "approve"
	// TokenID là id token Bearer; 0 khi đi bằng phiên.
	TokenID int64
}

// IsLead cho biết vai trò lead.
func (p Principal) IsLead() bool { return p.Role == "lead" }

// IsSession cho biết xác thực bằng cookie phiên.
func (p Principal) IsSession() bool { return p.Via == "session" }

type principalKey struct{}

// WithPrincipal gắn p vào ctx.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom lấy principal từ ctx.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// Instant định dạng thời điểm ISO-8601 UTC như DateTimeFormatter.ISO_INSTANT của Java (Jackson, Instant.toString):
// không có phần lẻ giây khi bằng 0, ngược lại đệm tới 3, 6 hoặc 9 chữ số (".500Z", ".123456Z", ".123456789Z").
func Instant(t time.Time) string {
	t = t.UTC()
	base := t.Format("2006-01-02T15:04:05")
	ns := t.Nanosecond()
	switch {
	case ns == 0:
		return base + "Z"
	case ns%1_000_000 == 0:
		return fmt.Sprintf("%s.%03dZ", base, ns/1_000_000)
	case ns%1_000 == 0:
		return fmt.Sprintf("%s.%06dZ", base, ns/1_000)
	default:
		return fmt.Sprintf("%s.%09dZ", base, ns)
	}
}
