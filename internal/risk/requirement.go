// Package risk: chính sách rủi ro, mẫu có sẵn, phân loại và quyết định luồng duyệt.
package risk

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
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
	ReqRulePosNeg = "rule-pos-neg"
	ReqBoundary   = "boundary"
	ReqIdempotent = "idempotent"
	ReqConcurrent = "concurrent"
	ReqInvariant  = "invariant"
	ReqCoverage   = "coverage"
	ReqSQLZero    = "sql-zero"
)

var requirementIDs = []string{ReqRulePosNeg, ReqBoundary, ReqIdempotent, ReqConcurrent, ReqInvariant, ReqCoverage, ReqSQLZero}

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
	case ReqConcurrent:
		return []string{"threads"}
	case ReqCoverage:
		return []string{"min"}
	case ReqSQLZero:
		return []string{"name", "sql"}
	}
	return nil
}

// RequirementDisplayName là tên hiển thị tiếng Việt.
func RequirementDisplayName(r Requirement) string {
	switch r.ID {
	case ReqRulePosNeg:
		return "Case đúng và case sai cho mỗi rule BRD"
	case ReqBoundary:
		return "Case biên cho rule gắn thẻ [biên]"
	case ReqIdempotent:
		return "Case gửi trùng"
	case ReqConcurrent:
		return "Case đồng thời " + strconv.Itoa(r.IntParam("threads", 0)) + " luồng"
	case ReqInvariant:
		return "Bất biến DB sau mọi case"
	case ReqCoverage:
		return "Coverage dòng mới tối thiểu " + strconv.Itoa(r.IntParam("min", 0)) + "%"
	case ReqSQLZero:
		if s, ok := r.StrParam("name"); ok {
			return s
		}
		return ReqSQLZero
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

// UnmarshalYAML đọc dạng phẳng { id: coverage, min: 90 }.
func (r *Requirement) UnmarshalYAML(n *yaml.Node) error {
	var m map[string]any
	if err := n.Decode(&m); err != nil {
		return err
	}
	out := Requirement{Params: map[string]any{}}
	for k, v := range m {
		if k == "id" {
			out.ID = fmt.Sprint(v)
			continue
		}
		out.Params[k] = v
	}
	*r = out
	return nil
}
