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
// body vượt giới hạn thì trả *ApiError 413. Giới hạn là của LimitBody/DefaultLimit nếu route đã đặt; không thì
// DecodeJSON tự chặn ở DefaultJSONLimit (không đọc bỏ phần thừa: nên đặt DefaultLimit ở router gốc).
func DecodeJSON(r *http.Request, dst any) error {
	var body io.Reader = r.Body
	if body == nil {
		body = http.NoBody
	}
	_, limited := r.Body.(*limitedBody)
	if !limited {
		body = io.LimitReader(body, DefaultJSONLimit+1)
	}
	b, err := io.ReadAll(body)
	if err != nil {
		var ae *ApiError
		if errors.As(err, &ae) {
			return ae
		}
		return NewError(http.StatusBadRequest, "JSON không hợp lệ")
	}
	if !limited && int64(len(b)) > DefaultJSONLimit {
		return TooLargeFor(DefaultJSONLimit)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return NewError(http.StatusBadRequest, "JSON không hợp lệ")
	}
	return nil
}

// DefaultJSONLimit là giới hạn body mặc định của API JSON (route cần lớn hơn, như ingest 8 MB, tự đặt LimitBody).
const DefaultJSONLimit int64 = 1 << 20

// TooLarge là lỗi 413 của giới hạn ingest 8 MB.
func TooLarge() *ApiError { return TooLargeFor(8 << 20) }

// TooLargeFor là lỗi 413 {"error":"Payload vượt <n> MB"} của giới hạn max byte.
func TooLargeFor(max int64) *ApiError {
	if max >= 1<<20 && max%(1<<20) == 0 {
		return NewError(http.StatusRequestEntityTooLarge, fmt.Sprintf("Payload vượt %d MB", max>>20))
	}
	return NewError(http.StatusRequestEntityTooLarge, fmt.Sprintf("Payload vượt %d byte", max))
}

const maxDrain = 256 << 20

// DrainTimeout bao thời gian đọc bỏ thân request (client gửi nhỏ giọt không giữ được goroutine mãi). Hết hạn thì
// ngừng đọc; phản hồi vẫn được ghi và server đóng kết nối.
var DrainTimeout = 15 * time.Second

// BodyTimeout bao thời gian đọc toàn bộ thân request có body (DefaultLimit/LimitBody), chống client gửi nhỏ giọt.
// Đọc tới EOF thì net/http tự bỏ hạn khi bắt đầu đọc nền (startBackgroundRead), nên ctx không bị hủy sau đó.
var BodyTimeout = 60 * time.Second

func setReadDeadline(w http.ResponseWriter, t time.Time) {
	if w != nil {
		// ResponseRecorder không hỗ trợ: bỏ qua lỗi.
		_ = http.NewResponseController(w).SetReadDeadline(t)
	}
}

// Drain đọc bỏ phần thân request còn lại (tối đa 256 MB, tối đa DrainTimeout) để client đang gửi nhận được phản hồi
// chứ không bị reset, như Tomcat max-swallow-size -1 của Java.
func Drain(w http.ResponseWriter, r *http.Request) {
	// Không có body thì server đã đọc nền (phát hiện client ngắt): không đặt hạn đọc lên đó.
	if r.Body != nil && r.Body != http.NoBody {
		drain(w, r.Body)
	}
}

func drain(w http.ResponseWriter, body io.Reader) {
	setReadDeadline(w, time.Now().Add(DrainTimeout))
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrain))
}

// LimitBody giới hạn body tối đa max byte. Quá giới hạn (theo Content-Length hoặc khi đọc) thì đọc bỏ phần còn lại
// (có hạn giờ, để client nhận được phản hồi chứ không bị reset) rồi trả 413 {"error":"Payload vượt <n> MB"}.
// Đặt sau DefaultLimit thì thay giới hạn mặc định.
func LimitBody(max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > max {
				Drain(w, r)
				WriteError(w, TooLargeFor(max))
				return
			}
			wrap(w, r, max)
			next.ServeHTTP(w, r)
		})
	}
}

// DefaultLimit đặt giới hạn đọc max byte cho mọi body (lười: chỉ báo 413 khi đọc quá, không từ chối theo
// Content-Length, để route đặt LimitBody lớn hơn sau đó vẫn hoạt động).
func DefaultLimit(max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrap(w, r, max)
			next.ServeHTTP(w, r)
		})
	}
}

func wrap(w http.ResponseWriter, r *http.Request, max int64) {
	if r.Body == nil || r.Body == http.NoBody {
		return
	}
	rc := r.Body
	if l, ok := rc.(*limitedBody); ok && l.read == 0 {
		rc = l.rc // thay giới hạn bên ngoài
	}
	if BodyTimeout > 0 {
		setReadDeadline(w, time.Now().Add(BodyTimeout))
	}
	r.Body = &limitedBody{rc: rc, left: max, max: max, w: w}
}

type limitedBody struct {
	rc        io.ReadCloser
	left, max int64
	read      int64
	over      bool
	w         http.ResponseWriter
}

func (l *limitedBody) Read(p []byte) (int, error) {
	if l.over {
		return 0, TooLargeFor(l.max)
	}
	if int64(len(p)) > l.left+1 {
		p = p[:l.left+1]
	}
	n, err := l.rc.Read(p)
	l.read += int64(n)
	if int64(n) > l.left {
		l.over = true
		drain(l.w, l.rc)
		return int(l.left), TooLargeFor(l.max)
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
