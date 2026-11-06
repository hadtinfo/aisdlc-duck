package gates

import (
	"encoding/json"
	"strconv"
	"strings"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/httpx"
)

// Lớp phủ trạng thái cổng cho repo chế độ web (GateOverlay.java): trạng thái hiển thị lấy theo dữ liệu server
// (approval/submission đối chiếu currentHash của snapshot), không chờ CLI gửi snapshot mới. Cây JSON là cây đã giải
// (map[string]any, []any, string, json.Number, bool, nil).

// Apply trả bản sao snapshot đã phủ trạng thái server; không sửa đầu vào. Snapshot không phải object → {}.
func Apply(snap any, server snapshot.FeatureGateState) any {
	out, ok := deepCopy(snap).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	gates, ok := out["gates"].([]any)
	if !ok {
		return out
	}
	previousApproved := true // thứ tự cổng theo snapshot (CLI ghi theo Gate.sequence của luồng)
	for _, n := range gates {
		g, ok := n.(map[string]any)
		if !ok {
			continue
		}
		name := asText(g["gate"], g != nil && hasKey(g, "gate"))
		current, hasCurrent := g["currentHash"].(string)
		a := lastAppr(server.Approvals, name)
		s := lastSub(server.Submissions, name)
		f := lastFeedback(server.Feedback, name)

		g["approval"] = nil
		if a != nil {
			g["approval"] = approvalNode(*a)
		}
		g["submission"] = nil
		if s != nil {
			g["submission"] = submissionNode(*s)
		}
		g["lastFeedback"] = nil
		if f != nil {
			g["lastFeedback"] = map[string]any{"kind": f.Kind, "by": f.By, "at": httpx.Instant(f.At), "comment": f.Comment}
		}

		var st string
		switch {
		case !previousApproved:
			st = "LOCKED_BY_PREVIOUS"
		case a != nil:
			st = "STALE"
			if hasCurrent && a.Hash == current {
				st = "APPROVED"
			}
		case s != nil && hasCurrent && s.Hash == current:
			st = "AWAITING_APPROVAL"
			if len(s.Findings) > 0 {
				st = "SUBMITTED_WITH_FINDINGS"
			}
		default:
			st = "NOT_SUBMITTED"
		}
		g["state"] = st
		previousApproved = previousApproved && st == "APPROVED"
	}
	return out
}

// MarkSuperseded đánh dấu superseded: true cho cổng đang không có bản duyệt (approval null) mà bản gần nhất bị thay
// thế tự động. Sửa tại chỗ.
func MarkSuperseded(snap any, gates map[string]bool) {
	if len(gates) == 0 {
		return
	}
	m, _ := snap.(map[string]any)
	arr, ok := m["gates"].([]any)
	if !ok {
		return
	}
	for _, n := range arr {
		g, ok := n.(map[string]any)
		if !ok {
			continue
		}
		if v, has := g["approval"]; has && v == nil && gates[asText(g["gate"], hasKey(g, "gate"))] {
			g["superseded"] = true
		}
	}
}

func hasKey(m map[string]any, k string) bool { _, ok := m[k]; return ok }

// asText như JsonNode.asText() (thiếu/container → "", null → "null").
func asText(v any, present bool) string {
	if !present {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return string(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return "null"
	}
	return ""
}

func approvalNode(a snapshot.Appr) map[string]any {
	o := map[string]any{"hash": a.Hash, "approver": a.Approver, "at": httpx.Instant(a.At)}
	if a.Channel == "auto" {
		// bản tự duyệt của server: hiện "tự duyệt (mức low)" thay cho người duyệt
		level := "?"
		if strings.HasPrefix(a.Rule, "flow:") {
			level = strings.TrimPrefix(a.Rule, "flow:")
		}
		var rule any
		if a.Rule != "" {
			rule = a.Rule
		}
		o["rule"] = rule
		o["label"] = "tự duyệt (mức " + level + ")"
	}
	if a.Channel == "claude-code" {
		o["label"] = a.Approver + " (trong Claude Code)"
	}
	return o
}

func submissionNode(s snapshot.Sub) map[string]any {
	fs := make([]any, 0, len(s.Findings))
	for _, f := range s.Findings {
		fs = append(fs, map[string]any{"code": f.Code, "message": f.Message})
	}
	return map[string]any{"hash": s.Hash, "at": httpx.Instant(s.At), "findings": fs}
}

// mục cuối của cổng trong danh sách (như WebGateStore phía CLI)
func lastAppr(items []snapshot.Appr, gate string) *snapshot.Appr {
	var found *snapshot.Appr
	for i := range items {
		if items[i].Gate == gate {
			found = &items[i]
		}
	}
	return found
}

func lastSub(items []snapshot.Sub, gate string) *snapshot.Sub {
	var found *snapshot.Sub
	for i := range items {
		if items[i].Gate == gate {
			found = &items[i]
		}
	}
	return found
}

func lastFeedback(items []snapshot.Fb, gate string) *snapshot.Fb {
	var found *snapshot.Fb
	for i := range items {
		if items[i].Gate == gate && (found == nil || items[i].ID > found.ID) {
			found = &items[i]
		}
	}
	return found
}

func deepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = deepCopy(e)
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i, e := range x {
			a[i] = deepCopy(e)
		}
		return a
	}
	return v
}
