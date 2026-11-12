package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/javaish"
)

// badRequest là BadRequestException: 400 {"error": msg}.
func badRequest(msg httpx.Text) *httpx.Error { return httpx.Err(400, msg) }

// node là một giá trị JSON đã giải (UseNumber): nil (null), bool, json.Number, string, []any, map[string]any.
// missing đại diện MissingNode của Jackson (path() trên trường không có).
type missingT struct{}

var missing any = missingT{}

func isMissing(n any) bool { _, ok := n.(missingT); return ok }

// path như JsonNode.path(name): object có trường thì trả giá trị, ngược lại missing.
func path(n any, name string) any {
	if m, ok := n.(map[string]any); ok {
		if v, ok := m[name]; ok {
			return v
		}
	}
	return missing
}

// parseTree đọc giá trị JSON đầu tiên của data như ObjectMapper.readTree(byte[]): thân rỗng → missing,
// phần thừa sau giá trị đầu bị bỏ qua, sai cú pháp (hoặc UTF-8 hỏng) → 400 "JSON không hợp lệ".
// Trả cả bytes thô của giá trị đầu (để lưu jsonb nguyên văn).
func parseTree(data []byte) (any, []byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return missing, nil, nil
		}
		return nil, nil, badRequest(httpx.T("JSON không hợp lệ"))
	}
	if !utf8.Valid(raw) {
		return nil, nil, badRequest(httpx.T("JSON không hợp lệ"))
	}
	d2 := json.NewDecoder(bytes.NewReader(raw))
	d2.UseNumber()
	var tree any
	if err := d2.Decode(&tree); err != nil {
		return nil, nil, badRequest(httpx.T("JSON không hợp lệ"))
	}
	return tree, raw, nil
}

// rejectNul từ chối chuỗi/tên trường chứa U+0000 (Postgres text/jsonb không nhận).
func rejectNul(root any) error {
	stack := []any{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch v := n.(type) {
		case string:
			if strings.IndexByte(v, 0) >= 0 {
				return badRequest(httpx.T("Chuỗi chứa ký tự NUL (U+0000)"))
			}
		case map[string]any:
			for k, c := range v {
				if strings.IndexByte(k, 0) >= 0 {
					return badRequest(httpx.T("Tên trường chứa ký tự NUL (U+0000)"))
				}
				stack = append(stack, c)
			}
		case []any:
			stack = append(stack, v...)
		}
	}
	return nil
}

// isInt như JsonNode.isInt(): số nguyên (không có phần thập phân/mũ) vừa int32.
func isInt(n any) (int64, bool) {
	num, ok := n.(json.Number)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseInt(string(num), 10, 64)
	if err != nil || v < math.MinInt32 || v > math.MaxInt32 {
		return 0, false
	}
	return v, true
}

// requireText: phải là chuỗi không rỗng/không chỉ khoảng trắng, nếu không 400 "Thiếu <name>".
func requireText(n any, name string) error {
	if s, ok := n.(string); !ok || isBlank(s) {
		return badRequest(httpx.Tf("Thiếu %s", name))
	}
	return nil
}

// text như ListText của Java: chuỗi thì trả, còn lại (null/thiếu/không phải chuỗi) → nil.
func text(n any, field string) *string {
	if s, ok := path(n, field).(string); ok {
		return &s
	}
	return nil
}

// asText như JsonNode.asText(""): chuỗi → giá trị, số/bool → dạng chữ, null/thiếu/container → "".
func asText(n any) string {
	switch v := n.(type) {
	case string:
		return v
	case json.Number:
		return string(v)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// asBoolean như JsonNode.asBoolean(false).
func asBoolean(n any) bool {
	switch v := n.(type) {
	case bool:
		return v
	case string:
		return strings.TrimSpace(v) == "true"
	case json.Number:
		if i, err := strconv.ParseInt(string(v), 10, 64); err == nil {
			return i != 0
		}
	}
	return false
}

// validateSnapshot như SnapshotValidator.validateSnapshot; trả capturedAt.
func validateSnapshot(s any) (time.Time, error) {
	if _, ok := s.(map[string]any); !ok {
		return time.Time{}, badRequest(httpx.T("Snapshot phải là JSON object"))
	}
	if v, ok := isInt(path(s, "schemaVersion")); !ok || v != 1 {
		return time.Time{}, badRequest(httpx.T("schemaVersion phải bằng 1"))
	}
	if err := requireText(path(path(s, "repo"), "id"), "repo.id"); err != nil {
		return time.Time{}, err
	}
	if err := requireText(path(s, "clientId"), "clientId"); err != nil {
		return time.Time{}, err
	}
	if err := requireText(path(s, "capturedAt"), "capturedAt"); err != nil {
		return time.Time{}, err
	}
	at, err := parseInstant(path(s, "capturedAt").(string))
	if err != nil {
		return time.Time{}, err
	}
	if err := requireText(path(path(s, "feature"), "key"), "feature.key"); err != nil {
		return time.Time{}, err
	}
	return at, nil
}

// instantRE là ISO_INSTANT/ISO_OFFSET_DATE_TIME của Java: giây và phần lẻ (tới 9 chữ số) tùy chọn, T/Z không phân biệt hoa thường,
// offset bắt buộc (Z hoặc ±hh:mm[:ss]).
var instantRE = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,9}))?)?([Zz]|[+-]\d{2}:\d{2}(?::\d{2})?)$`)

// parseInstant như SnapshotValidator.parseInstant; sai thì 400 "Thời điểm không hợp lệ: <text>".
func parseInstant(s string) (time.Time, error) {
	bad := badRequest(httpx.Tf("Thời điểm không hợp lệ: %s", s))
	m := instantRE.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, bad
	}
	num := func(x string) int { v, _ := strconv.Atoi(x); return v }
	y, mo, d, h, mi, sec := num(m[1]), num(m[2]), num(m[3]), num(m[4]), num(m[5]), num(m[6])
	nsec := 0
	if m[7] != "" {
		nsec = num((m[7] + "000000000")[:9])
	}
	if mo < 1 || mo > 12 || h > 23 || mi > 59 || sec > 59 {
		return time.Time{}, bad
	}
	t := time.Date(y, time.Month(mo), d, h, mi, sec, nsec, time.UTC)
	if t.Day() != d || d < 1 { // ngày không tồn tại (30/2...)
		return time.Time{}, bad
	}
	off := 0
	if z := m[8]; z != "Z" && z != "z" {
		oh, om, os := num(z[1:3]), num(z[4:6]), 0
		if len(z) > 6 {
			os = num(z[7:9])
		}
		if oh > 18 || om > 59 || os > 59 || oh*3600+om*60+os > 18*3600 {
			return time.Time{}, bad
		}
		off = oh*3600 + om*60 + os
		if z[0] == '-' {
			off = -off
		}
	}
	return t.Add(-time.Duration(off) * time.Second), nil
}

// utf16Len là String.length() của Java.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += len(utf16.Encode([]rune{r}))
	}
	return n
}

// ParseInstant đọc thời điểm như Instant.parse/OffsetDateTime.parse của Java (BoardRules.doneAt dùng).
func ParseInstant(s string) (time.Time, error) { return parseInstant(s) }

func isBlank(s string) bool { return javaish.IsBlank(s) }

func javaWhitespace(r rune) bool { return javaish.IsWhitespace(r) }
