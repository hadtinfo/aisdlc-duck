package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"aisdlc/internal/i18n"
)

// CompareJSON so expected (giá trị kiểu JSON) với actual (văn bản) theo kiểu khớp lỏng: object kỳ vọng là tập con,
// mảng cùng độ dài và không xét thứ tự, số so theo giá trị. Trả "" khi khớp, ngược lại mô tả chỗ lệch đầu tiên.
func CompareJSON(expected any, actual string) string {
	want, err := normalize(expected)
	if err != nil {
		return i18n.Tf("kỳ vọng không phải JSON hợp lệ: %s", err.Error())
	}
	got, err := decode([]byte(actual))
	if _, isObj := want.(map[string]any); isObj || isArr(want) {
		if err != nil {
			return i18n.Tf("phản hồi không phải JSON hợp lệ: %s", abbreviate(actual))
		}
		return diff("$", want, got)
	}
	if err != nil {
		got = actual // không phải JSON: so như chuỗi
	}
	if equalScalar(want, got) {
		return ""
	}
	return i18n.Tf("mong đợi %s, nhận %s", render(want), abbreviate(actual))
}

func isArr(v any) bool { _, ok := v.([]any); return ok }

func normalize(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return decode(b)
}

func decode(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if d.More() {
		return nil, errors.New(i18n.T("dữ liệu thừa sau JSON"))
	}
	return v, nil
}

func abbreviate(s string) string {
	r := []rune(s)
	if len(r) <= 200 {
		return s
	}
	return string(r[:200]) + "…"
}

func render(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func equalScalar(a, b any) bool {
	an, aok := a.(json.Number)
	bn, bok := b.(json.Number)
	if aok && bok {
		x, ok1 := new(big.Rat).SetString(string(an))
		y, ok2 := new(big.Rat).SetString(string(bn))
		return ok1 && ok2 && x.Cmp(y) == 0
	}
	if aok != bok {
		return false
	}
	return a == b
}

// diff trả "" khi want là tập con lỏng của got.
func diff(path string, want, got any) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return mismatch(path, want, got)
		}
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			gv, present := g[k]
			if !present {
				return i18n.Tf("%s.%s: mong đợi %s, không có trường", path, k, render(w[k]))
			}
			if m := diff(path+"."+k, w[k], gv); m != "" {
				return m
			}
		}
		return ""
	case []any:
		g, ok := got.([]any)
		if !ok {
			return mismatch(path, want, got)
		}
		if len(w) != len(g) {
			return i18n.Tf("%s: mong đợi mảng dài %d, nhận %d", path, len(w), len(g))
		}
		return diffArray(path, w, g)
	}
	if equalScalar(want, got) {
		return ""
	}
	return mismatch(path, want, got)
}

func mismatch(path string, want, got any) string {
	return i18n.Tf("%s: mong đợi %s, nhận %s", path, render(want), abbreviate(render(got)))
}

// diffArray ghép từng phần tử kỳ vọng với một phần tử thực tế khác nhau (không xét thứ tự).
func diffArray(path string, want, got []any) string {
	n := len(want)
	owner := make([]int, n) // got[j] đang ghép với want[owner[j]]
	for i := range owner {
		owner[i] = -1
	}
	match := func(i, j int) bool { return diff("$", want[i], got[j]) == "" }
	var try func(i int, seen []bool) bool
	try = func(i int, seen []bool) bool {
		for j := 0; j < n; j++ {
			if seen[j] || !match(i, j) {
				continue
			}
			seen[j] = true
			if owner[j] < 0 || try(owner[j], seen) {
				owner[j] = i
				return true
			}
		}
		return false
	}
	for i := 0; i < n; i++ {
		if !try(i, make([]bool, n)) {
			// so theo vị trí để báo chỗ lệch; nếu vị trí khớp thì lỗi nằm ở việc ghép không đủ phần tử
			if m := diff(fmt.Sprintf("%s[%d]", path, i), want[i], got[i]); m != "" {
				return m
			}
			return i18n.Tf("%s[%d]: không có phần tử nào khớp %s", path, i, render(want[i]))
		}
	}
	return ""
}
