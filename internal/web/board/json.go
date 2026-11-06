package board

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
)

// Cây JSON đã giải với UseNumber: nil (null), bool, json.Number, string, []any, map[string]any; missing là
// MissingNode của Jackson (path() trên trường không có). Các hàm dưới đây theo ngữ nghĩa JsonNode mà BoardRules dùng.

type missingT struct{}

var missing any = missingT{}

// ParseTree giải snapshot thành cây JSON (UseNumber).
func ParseTree(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// path như JsonNode.path(name).
func path(n any, names ...string) any {
	for _, name := range names {
		m, ok := n.(map[string]any)
		if !ok {
			return missing
		}
		v, ok := m[name]
		if !ok {
			return missing
		}
		n = v
	}
	return n
}

// asText như JsonNode.asText(): chuỗi → giá trị, số/bool → dạng chữ, null → "null", thiếu/container → "".
func asText(n any) string {
	switch v := n.(type) {
	case string:
		return v
	case json.Number:
		return string(v)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return "null"
	}
	return ""
}

// asTextOr như JsonNode.asText(def): null/thiếu → def, còn lại như asText.
func asTextOr(n any, def *string) *string {
	switch n.(type) {
	case nil, missingT:
		return def
	}
	s := asText(n)
	return &s
}

// elems như vòng for trên JsonNode: phần tử mảng, hoặc giá trị object theo thứ tự khóa của jsonb
// (ngắn trước, rồi theo byte), vì snapshot đọc từ jsonb; giá trị khác không có phần tử.
func elems(n any) []any {
	switch v := n.(type) {
	case []any:
		return v
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if len(keys[i]) != len(keys[j]) {
				return len(keys[i]) < len(keys[j])
			}
			return keys[i] < keys[j]
		})
		out := make([]any, len(keys))
		for i, k := range keys {
			out[i] = v[k]
		}
		return out
	}
	return nil
}

func isBool(n any) bool { _, ok := n.(bool); return ok }

func isObject(n any) bool { _, ok := n.(map[string]any); return ok }
