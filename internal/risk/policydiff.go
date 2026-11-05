package risk

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// PolicyChange là một mục khác nhau giữa hai bản chính sách. Kind thuộc ADD_RULE, REMOVE_RULE, CHANGE_RULE,
// ADD_REQ, REMOVE_REQ, CHANGE_PARAM, CHANGE_FLOW, CHANGE_CLASSIFICATION. Key: id rule, "<mức>/<id yêu cầu>",
// "<mức>/<id>.<tham số>" hoặc tên trường classification. Before/After rỗng khi không có (null của Java).
// Loosening: thay đổi làm nới yêu cầu (spec §6).
type PolicyChange struct {
	Kind      string
	Key       string
	Before    string
	After     string
	Loosening bool
}

// tightParams: tham số số mà giảm đi là nới yêu cầu.
var tightParams = map[string]bool{"min": true, "threads": true}

// DiffPolicies so hai bản chính sách theo từng mục.
func DiffPolicies(from, to RiskPolicy) []PolicyChange {
	out := []PolicyChange{}
	diffRules(from, to, &out)
	diffRequirements(from, to, &out)
	diffFlow(from, to, &out)
	diffClassification(from, to, &out)
	return out
}

func diffRules(from, to RiskPolicy, out *[]PolicyChange) {
	b := map[string]RiskRule{}
	for _, r := range to.Rules {
		b[r.ID] = r
	}
	a := map[string]bool{}
	for _, r := range from.Rules {
		a[r.ID] = true
		n, ok := b[r.ID]
		switch {
		case !ok:
			*out = append(*out, PolicyChange{"REMOVE_RULE", r.ID, describeRule(r), "", true})
		case !reflect.DeepEqual(r, n):
			disabled := r.Enabled && !n.Enabled
			lowered := to.Rank(n.Level) < rankIn(to, from, r.Level)
			*out = append(*out, PolicyChange{"CHANGE_RULE", r.ID, describeRule(r), describeRule(n), disabled || lowered})
		}
	}
	for _, n := range to.Rules {
		if !a[n.ID] {
			*out = append(*out, PolicyChange{"ADD_RULE", n.ID, "", describeRule(n), false})
		}
	}
}

// rankIn: thứ hạng của mức cũ theo thang mới (đổi tên mức thì theo thang cũ).
func rankIn(to, from RiskPolicy, level string) int {
	if r := to.Rank(level); r >= 0 {
		return r
	}
	return from.Rank(level)
}

func describeRule(r RiskRule) string {
	kind := "?"
	if r.Kind != "" {
		kind = strings.ToLower(string(r.Kind))
	}
	s := kind + " " + strings.Join(r.Match, " | ") + " → " + r.Level
	if !r.Enabled {
		s += " (tắt)"
	}
	return s
}

// orderedKeys: mức theo thang của from rồi của to (nếu có trong map), phần còn lại theo bảng chữ cái.
func orderedKeys[V any](a, b map[string]V, scales ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, scale := range scales {
		for _, l := range scale {
			_, inA := a[l]
			_, inB := b[l]
			if (inA || inB) && !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	var rest []string
	for _, m := range []map[string]V{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				rest = append(rest, k)
			}
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func reqByID(list []Requirement) (map[string]Requirement, []string) {
	m := map[string]Requirement{}
	var ids []string
	for _, q := range list {
		if _, dup := m[q.ID]; !dup {
			ids = append(ids, q.ID)
		}
		m[q.ID] = q
	}
	return m, ids
}

func diffRequirements(from, to RiskPolicy, out *[]PolicyChange) {
	for _, level := range orderedKeys(from.Requirements, to.Requirements, from.Levels, to.Levels) {
		a, aIDs := reqByID(from.RequirementsFor(level))
		b, bIDs := reqByID(to.RequirementsFor(level))
		for _, id := range aIDs {
			q, key := a[id], level+"/"+id
			n, ok := b[id]
			if !ok {
				*out = append(*out, PolicyChange{"REMOVE_REQ", key, describeReq(q), "", true})
				continue
			}
			for _, p := range orderedKeys(q.Params, n.Params) {
				before, hasB := q.Params[p]
				after, hasA := n.Params[p]
				if hasB == hasA && fmt.Sprint(before) == fmt.Sprint(after) {
					continue
				}
				c := PolicyChange{Kind: "CHANGE_PARAM", Key: key + "." + p, Loosening: tightParams[p] && decreased(before, hasB, after, hasA)}
				if hasB {
					c.Before = fmt.Sprint(before)
				}
				if hasA {
					c.After = fmt.Sprint(after)
				}
				*out = append(*out, c)
			}
		}
		for _, id := range bIDs {
			if _, ok := a[id]; !ok {
				*out = append(*out, PolicyChange{"ADD_REQ", level + "/" + id, "", describeReq(b[id]), false})
			}
		}
	}
}

func describeReq(q Requirement) string {
	if len(q.Params) == 0 {
		return q.ID
	}
	var b strings.Builder
	b.WriteString(q.ID)
	for _, k := range sortedKeys(q.Params) {
		fmt.Fprintf(&b, " %s=%v", k, q.Params[k])
	}
	return b.String()
}

// decreased: giảm giá trị số, hoặc bỏ tham số đang có.
func decreased(before any, hasBefore bool, after any, hasAfter bool) bool {
	if !hasBefore || before == nil {
		return false
	}
	if !hasAfter || after == nil {
		return true
	}
	x, err1 := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(after)), 64)
	y, err2 := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(before)), 64)
	return err1 == nil && err2 == nil && x < y
}

func diffFlow(from, to RiskPolicy, out *[]PolicyChange) {
	for _, level := range orderedKeys(from.Flow, to.Flow, from.Levels, to.Levels) {
		for _, unit := range FlowUnits {
			a, b := from.FlowFor(level, unit), to.FlowFor(level, unit)
			if a != b {
				*out = append(*out, PolicyChange{"CHANGE_FLOW", level + "/" + unit, a, b, b == "auto"})
			}
		}
	}
}

func diffClassification(from, to RiskPolicy, out *[]PolicyChange) {
	a, b := from.Classification, to.Classification
	if a.Agent != b.Agent {
		*out = append(*out, PolicyChange{"CHANGE_CLASSIFICATION", "agent", string(a.Agent), string(b.Agent),
			a.Agent == AgentRaise && b.Agent == AgentOff})
	}
	if a.DevLower != b.DevLower {
		*out = append(*out, PolicyChange{"CHANGE_CLASSIFICATION", "devLower", string(a.DevLower), string(b.DevLower),
			a.DevLower == DevLowerNever && b.DevLower == DevLowerReason})
	}
	if from.DefaultLevel != to.DefaultLevel {
		*out = append(*out, PolicyChange{"CHANGE_CLASSIFICATION", "default", from.DefaultLevel, to.DefaultLevel,
			to.Rank(to.DefaultLevel) < rankIn(to, from, from.DefaultLevel)})
	}
}
