// Package risk: chính sách rủi ro, mẫu có sẵn, phân loại và quyết định luồng duyệt.
package risk

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Requirement là một yêu cầu chất lượng theo mức. YAML/JSON phẳng: { id: coverage, min: 90 }.
type Requirement struct {
	ID     string
	Params map[string]any
}

// NewRequirement tạo Requirement, Params nil thành rỗng (sao chép).
func NewRequirement(id string, params map[string]any) Requirement {
	p := make(map[string]any, len(params))
	for k, v := range params {
		p[k] = v
	}
	return Requirement{ID: id, Params: p}
}

// IntParam trả tham số số nguyên; thiếu hoặc không đọc được thì def.
func (r Requirement) IntParam(name string, def int) int {
	switch n := r.Params[name].(type) {
	case nil:
		return def
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		v, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(n)))
		if err != nil {
			return def
		}
		return v
	}
}

// StrParam trả tham số dạng chuỗi; ok=false khi không có.
func (r Requirement) StrParam(name string) (string, bool) {
	v, ok := r.Params[name]
	if !ok || v == nil {
		return "", false
	}
	return fmt.Sprint(v), true
}

// MarshalYAML ghi dạng phẳng.
func (r Requirement) MarshalYAML() (any, error) { return r.flat(), nil }

func (r Requirement) flat() map[string]any {
	m := map[string]any{"id": r.ID}
	for k, v := range r.Params {
		m[k] = v
	}
	return m
}

// Mã yêu cầu trong thư viện.
const (
	RulePosNeg = "rule-pos-neg"
	Boundary   = "boundary"
	Idempotent = "idempotent"
	Concurrent = "concurrent"
	Invariant  = "invariant"
	Coverage   = "coverage"
	SQLZero    = "sql-zero"
)

var requirementIDs = []string{RulePosNeg, Boundary, Idempotent, Concurrent, Invariant, Coverage, SQLZero}

// RequirementKnown cho biết id có trong thư viện không.
func RequirementKnown(id string) bool {
	for _, k := range requirementIDs {
		if k == id {
			return true
		}
	}
	return false
}

// RequirementRequiredParams là các tham số bắt buộc của yêu cầu.
func RequirementRequiredParams(id string) []string {
	switch id {
	case Concurrent:
		return []string{"threads"}
	case Coverage:
		return []string{"min"}
	case SQLZero:
		return []string{"name", "sql"}
	}
	return nil
}

// RequirementDisplayName là tên hiển thị tiếng Việt.
func RequirementDisplayName(r Requirement) string {
	switch r.ID {
	case RulePosNeg:
		return "Case đúng và case sai cho mỗi rule BRD"
	case Boundary:
		return "Case biên cho rule gắn thẻ [biên]"
	case Idempotent:
		return "Case gửi trùng"
	case Concurrent:
		return "Case đồng thời " + strconv.Itoa(r.IntParam("threads", 0)) + " luồng"
	case Invariant:
		return "Bất biến DB sau mọi case"
	case Coverage:
		return "Coverage dòng mới tối thiểu " + strconv.Itoa(r.IntParam("min", 0)) + "%"
	case SQLZero:
		if s, ok := r.StrParam("name"); ok {
			return s
		}
		return SQLZero
	}
	return r.ID
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
