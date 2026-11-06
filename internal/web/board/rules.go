// Package board là bảng tính năng, chi tiết tính năng và SSE của server web (port board/*.java).
package board

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"aisdlc/internal/web/ingest"
)

// Quy tắc bảng (§5b.2): hàm thuần trên cây JSON snapshot, không đụng DB (BoardRules.java).

// InboxItem là một việc trong hộp thư của chủ tính năng.
type InboxItem struct {
	Kind, Text, Command string
}

// EvalSummary: CasesPassed/CasesTotal nil khi không đọc được từ CASES_ALL_PASS.
type EvalSummary struct {
	Passed      bool `json:"passed"`
	CasesPassed *int `json:"casesPassed"`
	CasesTotal  *int `json:"casesTotal"`
}

var (
	fullGates = []string{"BRD", "DESIGN", "CASES"}
	liteGates = []string{"CASES"}
	ratio     = regexp.MustCompile(`(\d+)/(\d+)`)
	shellSafe = regexp.MustCompile(`^[A-Za-z0-9._/@+=:,-]+$`)
)

// Column là cột của thẻ: cổng đầu tiên chưa APPROVED, rồi DONE (REVIEW đã duyệt), CODE (còn bước chưa ACCEPTED), REVIEW.
func Column(snap any) string {
	gates := fullGates
	if asText(path(snap, "feature", "flow")) == "LITE" {
		gates = liteGates
	}
	for _, g := range gates {
		if gateState(snap, g) != "APPROVED" {
			return g
		}
	}
	if gateState(snap, "REVIEW") == "APPROVED" {
		return "DONE"
	}
	for _, s := range elems(path(snap, "steps")) {
		if asText(path(s, "status")) != "ACCEPTED" {
			return "CODE"
		}
	}
	return "REVIEW"
}

// Labels là nhãn của thẻ (luôn khác nil).
func Labels(snap any) []string {
	out := []string{}
	for _, g := range elems(path(snap, "gates")) {
		gate := asText(path(g, "gate"))
		switch asText(path(g, "state")) {
		case "AWAITING_APPROVAL":
			out = append(out, "AWAITING_APPROVAL:"+gate)
		case "STALE":
			out = append(out, "STALE:"+gate)
		case "SUBMITTED_WITH_FINDINGS":
			out = append(out, "FINDINGS:"+gate)
		case "NOT_SUBMITTED":
			if asText(path(g, "lastFeedback", "kind")) == "REJECTED" {
				out = append(out, "REJECTED:"+gate)
			}
		}
	}
	for _, s := range elems(path(snap, "steps")) {
		id := asText(path(s, "id"))
		switch asText(path(s, "status")) {
		case "IN_PROGRESS":
			out = append(out, "CODING:"+id)
		case "AWAITING_DEV":
			out = append(out, "AWAITING_ACCEPT:"+id)
		}
	}
	if evalFailed(snap) {
		out = append(out, "EVAL_FAIL")
	}
	return out
}

func evalFailed(snap any) bool {
	p, ok := path(snap, "eval", "passed").(bool)
	return ok && !p
}

// Inbox là việc chủ tính năng cần làm, kèm lệnh chạy được.
func Inbox(snap any) []InboxItem {
	out := []InboxItem{}
	for _, g := range elems(path(snap, "gates")) {
		gate := asText(path(g, "gate"))
		lower := strings.ToLower(gate)
		switch asText(path(g, "state")) {
		case "AWAITING_APPROVAL":
			out = append(out, InboxItem{"APPROVE", "Cổng " + gate + " chờ bạn duyệt", "aisdlc approve " + lower})
		case "STALE":
			out = append(out, InboxItem{"STALE", "Cổng " + gate + " đã đổi sau khi duyệt (STALE): nộp lại, hoặc `aisdlc reopen " +
				lower + "` nếu muốn sửa lại từ đầu", "aisdlc submit " + lower})
		case "SUBMITTED_WITH_FINDINGS":
			out = append(out, InboxItem{"FINDINGS", "Cổng " + gate + " có phát hiện cần sửa", "aisdlc submit " + lower})
		}
	}
	brd := "brd"
	for _, q := range elems(path(snap, "openQuestions")) {
		gate := strings.ToLower(*asTextOr(path(q, "gate"), &brd))
		out = append(out, InboxItem{"OPEN_QUESTION", "Câu hỏi mở: " + asText(path(q, "text")) + " (trả lời câu hỏi rồi nộp lại)",
			"aisdlc submit " + gate})
	}
	for _, s := range elems(path(snap, "steps")) {
		id := asText(path(s, "id"))
		status := asText(path(s, "status"))
		if status == "AWAITING_DEV" {
			out = append(out, InboxItem{"ACCEPT_STEP", "Bước " + id + " chờ bạn accept", "aisdlc step accept " + id})
		}
		if status == "ACCEPTED" {
			continue // bước đã accept thì không còn việc
		}
		check := path(s, "record", "check")
		// Chỉ có khi bước AWAITING_DEV: `step finish` cần IN_PROGRESS, còn `step start` mở lại bước để làm tiếp.
		if notPassed := elems(path(check, "casesNotPassed")); len(notPassed) > 0 {
			out = append(out, InboxItem{"CASES_NOT_PASSED", "Bước " + id + " còn case chưa đạt: " + join(notPassed) +
				" (mở lại bước để agent sửa, rồi `aisdlc step finish " + id + "`)", "aisdlc step start " + id})
		}
		for _, f := range elems(path(check, "outOfScope")) {
			p := asText(f)
			out = append(out, InboxItem{"OUT_OF_SCOPE", "Bước " + id + " sửa file ngoài phạm vi: " + p +
				" (cho phép file này, hoặc để agent hoàn tác rồi `aisdlc step start " + id + "`)", "aisdlc scope allow " + shellArg(p)})
		}
	}
	if evalFailed(snap) {
		out = append(out, InboxItem{"EVAL_FAIL", "Eval không đạt", "aisdlc eval"})
	}
	return out
}

// DoneAt là lúc tính năng xong: approval.at của cổng REVIEW, nếu có và đọc được.
func DoneAt(snap any) (time.Time, bool) {
	for _, g := range elems(path(snap, "gates")) {
		if asText(path(g, "gate")) != "REVIEW" {
			continue
		}
		empty := ""
		at := *asTextOr(path(g, "approval", "at"), &empty)
		if at == "" {
			return time.Time{}, false
		}
		t, err := ingest.ParseInstant(at)
		if err != nil {
			return time.Time{}, false
		}
		return t, true
	}
	return time.Time{}, false
}

// EvalSummaryOf tóm tắt eval; nil khi không có eval hoặc eval.passed không phải boolean.
func EvalSummaryOf(snap any) *EvalSummary {
	eval := path(snap, "eval")
	passed, ok := path(eval, "passed").(bool)
	if !isObject(eval) || !ok {
		return nil
	}
	out := &EvalSummary{Passed: passed}
	for _, c := range elems(path(eval, "hard")) {
		if asText(path(c, "id")) != "CASES_ALL_PASS" {
			continue
		}
		empty := ""
		if m := ratio.FindStringSubmatch(*asTextOr(path(c, "detail"), &empty)); m != nil {
			// Java Integer.valueOf: số quá lớn ném lỗi (500); ở đây bỏ qua cặp đó.
			p, err1 := strconv.ParseInt(m[1], 10, 32)
			t, err2 := strconv.ParseInt(m[2], 10, 32)
			if err1 == nil && err2 == nil {
				pi, ti := int(p), int(t)
				out.CasesPassed, out.CasesTotal = &pi, &ti
			}
		}
	}
	return out
}

// Owner: login khớp email, rồi người gửi đầu tiên, rồi email thô (có thể nil).
func Owner(snap any, emailToLogin map[string]string, firstSender *string) *string {
	var email *string
	if o, ok := path(snap, "feature", "owner").(string); ok && !isBlank(o) {
		e := javaTrim(o)
		email = &e
	}
	if email != nil {
		login, ok := emailToLogin[*email]
		if !ok {
			login, ok = emailToLogin[strings.ToLower(*email)]
		}
		if ok {
			return &login
		}
	}
	if firstSender != nil {
		return firstSender
	}
	return email
}

func gateState(snap any, gate string) string {
	for _, g := range elems(path(snap, "gates")) {
		if asText(path(g, "gate")) == gate {
			return asText(path(g, "state"))
		}
	}
	return ""
}

// shellArg: đường dẫn dán thẳng vào shell; chỉ ký tự an toàn thì giữ nguyên, còn lại bọc nháy đơn.
func shellArg(p string) string {
	if shellSafe.MatchString(p) {
		return p
	}
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

func join(arr []any) string {
	items := make([]string, len(arr))
	for i, n := range arr {
		items[i] = asText(n)
	}
	return strings.Join(items, ", ")
}

// javaWhitespace như Character.isWhitespace.
func javaWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0x1c, 0x1d, 0x1e, 0x1f, 0x1680, 0x2028, 0x2029, 0x205f, 0x3000:
		return true
	case 0xa0, 0x2007, 0x202f:
		return false
	}
	return r >= 0x2000 && r <= 0x200a
}

func isBlank(s string) bool { return strings.TrimFunc(s, javaWhitespace) == "" }

// javaTrim như String.trim(): bỏ ký tự <= U+0020 ở hai đầu.
func javaTrim(s string) string { return strings.TrimFunc(s, func(r rune) bool { return r <= ' ' }) }
