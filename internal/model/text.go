package model

import "golang.org/x/text/unicode/norm"

// NFC chuẩn hóa chuỗi về dạng NFC.
func NFC(s string) string { return norm.NFC.String(s) }
