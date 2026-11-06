// Package auth là xác thực của server web: token có loại, phiên trình duyệt + CSRF, luật vùng theo đường dẫn,
// audit và lệnh quản trị user (port auth/*.java).
package auth

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf16"
)

// Area là vùng đường dẫn có luật xác thực riêng (AuthFilter.Area).
type Area int

// Các vùng, theo thứ tự kiểm của AuthFilter.area.
const (
	AreaLogin Area = iota
	AreaIngest
	AreaCLI
	AreaCI
	AreaGates
	AreaControl
	AreaOther
)

var areaNames = [...]string{"LOGIN", "INGEST", "CLI", "CI", "GATES", "CONTROL", "OTHER"}

func (a Area) String() string {
	if a >= 0 && int(a) < len(areaNames) {
		return areaNames[a]
	}
	return "?"
}

// AreaOf trả vùng của path (đúng bằng tiền tố hoặc tiền tố + "/").
func AreaOf(path string) Area {
	switch {
	case under(path, "/api/login"):
		return AreaLogin
	case under(path, "/api/ingest"):
		return AreaIngest
	case under(path, "/api/cli"):
		return AreaCLI
	case under(path, "/api/ci"):
		return AreaCI
	case under(path, "/api/gates"):
		return AreaGates
	case under(path, "/api/control"):
		return AreaControl
	}
	return AreaOther
}

func under(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// Normalize chuẩn hóa đường thô như AuthFilter.normalize: '\' như '/', bỏ ;params mỗi đoạn, giải %-mã một lần
// (%2F/%5C giải ra thì tách tiếp), bỏ đoạn rỗng và ".", ".." lùi một đoạn. %-mã sai → "/invalid".
func Normalize(raw string) string {
	var out []string
	push := func(seg string) {
		switch seg {
		case "", ".":
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}
	for _, seg := range strings.Split(strings.ReplaceAll(raw, `\`, "/"), "/") {
		if i := strings.IndexByte(seg, ';'); i >= 0 {
			seg = seg[:i]
		}
		// PathUnescape không đổi '+' thành dấu cách: như URLDecoder sau replace("+", "%2B") của Java.
		dec, err := url.PathUnescape(seg)
		if err != nil {
			return "/invalid"
		}
		for _, sub := range strings.Split(strings.ReplaceAll(dec, `\`, "/"), "/") {
			push(sub)
		}
	}
	return "/" + strings.Join(out, "/")
}

// javaWhitespace là Character.isWhitespace của Java.
func javaWhitespace(r rune) bool {
	switch r {
	case ' ', ' ', ' ':
		return false
	case '\t', '\n', '\u000b', '\f', '\r', '\u001c', '\u001d', '\u001e', '\u001f':
		return true
	}
	return unicode.In(r, unicode.Zs, unicode.Zl, unicode.Zp)
}

// javaBlank là String.isBlank của Java.
func javaBlank(s string) bool { return strings.TrimFunc(s, javaWhitespace) == "" }

// javaStrip là String.strip của Java.
func javaStrip(s string) string { return strings.TrimFunc(s, javaWhitespace) }

// utf16Len là String.length() của Java (số đơn vị UTF-16).
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }
