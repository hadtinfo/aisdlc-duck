// Package javaish là các phép trên chuỗi theo đúng ngữ nghĩa Java mà server web cần giữ (isBlank, trim...).
package javaish

import "strings"

// IsWhitespace như Character.isWhitespace: khoảng trắng Unicode trừ khoảng trắng không ngắt (U+00A0, U+2007, U+202F),
// cộng các ký tự điều khiển phân tách U+001C..U+001F.
func IsWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0x1c, 0x1d, 0x1e, 0x1f, 0x1680, 0x2028, 0x2029, 0x205f, 0x3000:
		return true
	case 0xa0, 0x2007, 0x202f:
		return false
	}
	return r >= 0x2000 && r <= 0x200a
}

// IsBlank như String.isBlank: rỗng hoặc chỉ gồm ký tự IsWhitespace.
func IsBlank(s string) bool {
	for _, r := range s {
		if !IsWhitespace(r) {
			return false
		}
	}
	return true
}

// Trim như String.trim: bỏ ký tự <= U+0020 ở hai đầu.
func Trim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r <= ' ' })
}
