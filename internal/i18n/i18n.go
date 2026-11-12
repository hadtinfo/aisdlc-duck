// Package i18n dịch chuỗi kiểu gettext: chuỗi tiếng Việt trong code chính là khóa,
// bản tiếng Anh nằm trong catalog en (en.go). Thiếu bản dịch thì rơi về chính khóa.
// Đây là gói lá, chỉ dùng thư viện chuẩn.
package i18n

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Locale là mã ngôn ngữ của repo hoặc của người xem.
type Locale string

const (
	VI Locale = "vi"
	EN Locale = "en"
)

// envKey là biến môi trường chọn locale.
const envKey = "AISDLC_LOCALE"

var def atomic.Value // lưu Locale

// Parse đọc mã locale: rỗng là VI, khác vi/en là lỗi.
func Parse(s string) (Locale, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "vi":
		return VI, nil
	case "en":
		return EN, nil
	}
	return VI, fmt.Errorf("%s", Default().Tf("Giá trị locale không hợp lệ: %q (chỉ nhận vi hoặc en)", s))
}

// FromEnv đọc AISDLC_LOCALE; thiếu hoặc sai thì VI.
func FromEnv(getenv func(string) (string, bool)) Locale {
	v, ok := getenv(envKey)
	if !ok {
		return VI
	}
	l, err := Parse(v)
	if err != nil {
		return VI
	}
	return l
}

// SetDefault đặt locale mặc định cho hàm gói T/Tf.
func SetDefault(l Locale) { def.Store(l) }

// Default trả locale mặc định; chưa đặt thì VI.
func Default() Locale {
	if l, ok := def.Load().(Locale); ok {
		return l
	}
	return VI
}

// T dịch khóa theo locale mặc định.
func T(key string) string { return Default().T(key) }

// Tf dịch chuỗi định dạng theo locale mặc định rồi Sprintf.
func Tf(format string, args ...any) string { return Default().Tf(format, args...) }

// T dịch khóa; không phải EN hoặc thiếu mục thì trả nguyên khóa.
func (l Locale) T(key string) string {
	if l == EN {
		if v, ok := en[key]; ok {
			return v
		}
	}
	return key
}

// Tf dịch chuỗi định dạng rồi Sprintf.
func (l Locale) Tf(format string, args ...any) string {
	return fmt.Sprintf(l.T(format), args...)
}
