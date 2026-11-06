package harness

import (
	"strings"
	"testing"
)

func TestCompareJSONLenient(t *testing.T) {
	obj := map[string]any{"a": 1, "b": map[string]any{"c": 2}}
	actual := `{"a":1.0,"b":{"c":2,"d":3},"list":[{"code":"x","n":1},{"code":"y","n":2}]}`
	tests := []struct {
		name string
		want any
		got  string
		msg  string // "" = khớp; ngược lại là chuỗi con phải có
	}{
		{"subset", obj, actual, ""},
		{"number 1 vs 1.0", map[string]any{"a": 1.0}, `{"a":1}`, ""},
		{"nested mismatch", map[string]any{"b": map[string]any{"c": 9}}, actual, "$.b.c: mong đợi 9, nhận 2"},
		{"missing key", map[string]any{"zz": 1}, actual, "$.zz: mong đợi 1, không có trường"},
		{"array any order", []any{map[string]any{"code": "y"}, map[string]any{"code": "x"}},
			`[{"code":"x","n":1},{"code":"y"}]`, ""},
		{"array length", []any{1}, `[1,2]`, "$: mong đợi mảng dài 1, nhận 2"},
		{"array element", map[string]any{"list": []any{map[string]any{"code": "x"}, map[string]any{"code": "q"}}},
			actual, "$.list[1].code: mong đợi \"q\", nhận \"y\""},
		{"array needs distinct elements", []any{1, 1}, `[1,2]`, "$[1]"},
		{"scalar parsed", 5, `5`, ""},
		{"scalar string", "ok", `"ok"`, ""},
		{"scalar plain text", "ok", `ok`, ""},
		{"scalar mismatch", 5, `6`, "mong đợi 5, nhận 6"},
		{"type mismatch", map[string]any{"a": "1"}, `{"a":1}`, "$.a: mong đợi \"1\", nhận 1"},
		{"null", map[string]any{"a": nil}, `{"a":null}`, ""},
		{"not json", obj, "<html>" + strings.Repeat("x", 300), "phản hồi không phải JSON hợp lệ: <html>"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CompareJSON(tc.want, tc.got)
			if tc.msg == "" && got != "" || tc.msg != "" && !strings.Contains(got, tc.msg) {
				t.Errorf("got %q, want contains %q", got, tc.msg)
			}
		})
	}
	m := CompareJSON(obj, strings.Repeat("x", 300))
	if !strings.HasSuffix(m, strings.Repeat("x", 200)+"…") {
		t.Errorf("not abbreviated: %q", m)
	}
}
