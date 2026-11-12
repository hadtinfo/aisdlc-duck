package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"aisdlc/internal/i18n"
)

// Ngôn ngữ trong server web: mỗi request mang locale riêng trên context (không dùng locale toàn cục của gói i18n, để
// các request đồng thời không lẫn ngôn ngữ). Thứ tự: users.locale của phiên đã chọn → Accept-Language → vi.

type localeKey struct{}

// WithLocale gắn locale của request vào ctx.
func WithLocale(ctx context.Context, l i18n.Locale) context.Context {
	return context.WithValue(ctx, localeKey{}, l)
}

// LocaleFrom lấy locale của request; chưa gắn thì theo Accept-Language không có gì, tức vi.
func LocaleFrom(ctx context.Context) i18n.Locale {
	if l, ok := ctx.Value(localeKey{}).(i18n.Locale); ok {
		return l
	}
	return i18n.VI
}

// ParseAcceptLanguage đọc dịu header Accept-Language theo hợp đồng: chỉ xét tag ĐẦU TIÊN (bỏ qua q); primary subtag
// là en thì en, còn lại (vi, ngôn ngữ khác, thiếu, hỏng) là vi. CLI gửi đúng "vi" hoặc "en". Cùng luật với
// navigator.language của UI (en* → en).
func ParseAcceptLanguage(h string) i18n.Locale {
	first, _, _ := strings.Cut(h, ",")
	tag, _, _ := strings.Cut(first, ";")
	lang := strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(lang, "-_"); i >= 0 {
		lang = lang[:i]
	}
	if lang == "en" {
		return i18n.EN
	}
	return i18n.VI
}

// Localize là middleware gốc: đặt locale của request theo Accept-Language. Middleware xác thực ghi đè bằng lựa chọn
// đã lưu của người dùng (phiên trình duyệt).
func Localize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(WithLocale(r.Context(), ParseAcceptLanguage(r.Header.Get("Accept-Language")))))
	})
}

// Text là một câu cho người dùng: khóa tiếng Việt (khóa của catalog i18n) cùng đối số, dịch lúc ghi phản hồi theo
// locale của request. Tạo bằng T / Tf với literal để test catalog thấy khóa.
type Text struct {
	key   string
	args  []any
	plain bool
}

// Plain là câu không dịch (đã là tiếng Anh chuẩn của HTTP, như "Bad Request").
func Plain(s string) Text { return Text{key: s, plain: true} }

// T là câu không có đối số.
func T(key string) Text { return Text{key: key} }

// Tf là câu định dạng: format là khóa tiếng Việt, args giữ nguyên (không dịch).
func Tf(format string, args ...any) Text { return Text{key: format, args: append([]any{}, args...)} }

// Error là lỗi nghiệp vụ có câu dịch được: HTTP status + {"error": "<câu theo locale của request>"} (ghi bằng Fail).
// Error() là bản tiếng Việt (log). Dùng thay ApiError ở mọi chỗ có câu cho người dùng.
type Error struct {
	Status int
	Text   Text
}

func (e *Error) Error() string { return e.Text.String() }

// Err tạo lỗi nghiệp vụ với status và câu dịch được.
func Err(status int, t Text) *Error { return &Error{Status: status, Text: t} }

// Fail ghi lỗi theo locale của r: *Error → status + {"error": câu đã dịch}; *ApiError (DecodeJSON, giới hạn body…)
// → status + câu tiếng Việt của nó đã dịch qua catalog; lỗi khác → WriteError (500 {"error":"internal"} và log).
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	loc := LocaleFrom(r.Context())
	var te *Error
	if errors.As(err, &te) {
		WriteJSON(w, te.Status, map[string]string{"error": te.Text.In(loc)})
		return
	}
	var ae *ApiError
	if errors.As(err, &ae) {
		WriteJSON(w, ae.Status, map[string]string{"error": apiMessage(ae.Message, loc)})
		return
	}
	WriteError(w, err)
}

// apiMessage dịch câu của ApiError tạo trong httpx.go (vẫn là chuỗi tiếng Việt đã định dạng): câu cố định tra thẳng
// catalog, câu "Payload vượt <n> …" tách số rồi định dạng lại.
func apiMessage(msg string, loc i18n.Locale) string {
	if loc == i18n.VI {
		return msg
	}
	for _, f := range []string{i18n.VI.T("Payload vượt %d MB"), i18n.VI.T("Payload vượt %d byte")} {
		var n int64
		if k, err := fmt.Sscanf(msg, f, &n); err == nil && k == 1 && fmt.Sprintf(f, n) == msg {
			return loc.Tf(f, n)
		}
	}
	return loc.T(msg)
}

// In dịch câu theo locale l.
func (t Text) In(l i18n.Locale) string {
	if t.plain {
		return t.key
	}
	if len(t.args) == 0 {
		return l.T(t.key)
	}
	return l.Tf(t.key, t.args...)
}

// String là bản tiếng Việt (log, Error()).
func (t Text) String() string { return t.In(i18n.VI) }

var (
	opOnce sync.Once
	opLoc  i18n.Locale
)

// OpLocale là ngôn ngữ của người vận hành server (nhật ký, lỗi khởi động, lệnh quản trị `aisdlc-web user …`): biến
// AISDLC_LOCALE của tiến trình, thiếu/sai → vi. Không dùng cho phản hồi của request.
func OpLocale() i18n.Locale {
	opOnce.Do(func() { opLoc = i18n.FromEnv(os.LookupEnv) })
	return opLoc
}
