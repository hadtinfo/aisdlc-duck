package cli

import (
	"strconv"
	"strings"

	"aisdlc/internal/gate"
	"aisdlc/internal/step"
)

// Các dòng hiển thị dùng chung giữa status, approve, review card, step (AutoFlow.label/display và
// StepCommand.summary của Java).

// autoLabel là nhãn bản tự duyệt.
func autoLabel(a gate.Approval) string {
	lvl := a.RuleLevel()
	if lvl == "" {
		lvl = "?"
	}
	return "tự duyệt (mức " + lvl + ")"
}

// supersededNote là ghi chú cho bản tự duyệt đã bị thay thế: cổng hiện như đang chờ dev duyệt.
func supersededNote(a gate.Approval) string {
	return "chờ dev duyệt (bản " + autoLabel(a) + " đã bị thay thế khi nâng mức)"
}

// approvalDisplay là dòng hiển thị bản duyệt: bản tự duyệt còn hiệu lực hoặc đã bị thay thế, bản duyệt tay trong
// Claude Code; bản duyệt tay ở terminal hoặc chưa duyệt (ok=false) thì "".
func approvalDisplay(a gate.Approval, ok bool) string {
	if !ok {
		return ""
	}
	if !a.IsAuto() && a.Channel == gate.ApprovalClaudeCode {
		return "duyệt trong Claude Code (" + a.Approver + ")"
	}
	if !a.IsAuto() {
		return ""
	}
	if a.Superseded {
		return supersededNote(a)
	}
	return autoLabel(a)
}

// stepCheckSummary là một dòng tóm tắt kết quả kiểm khi kết thúc bước.
func stepCheckSummary(ch step.StepCheck) string {
	total := ch.CasesPassed + int64(len(ch.CasesNotPassed))
	var sb strings.Builder
	sb.WriteString("Case: " + strconv.FormatInt(ch.CasesPassed, 10) + "/" + strconv.FormatInt(total, 10) + " pass")
	if len(ch.CasesNotPassed) > 0 {
		sb.WriteString(" (" + strings.Join(ch.CasesNotPassed, ", ") + " fail)")
	}
	sb.WriteString(" · " + strconv.Itoa(ch.Files) + " file / " + strconv.Itoa(ch.Lines) + " dòng (ngân sách ")
	if ch.Budget.Files == 0 && ch.Budget.Lines == 0 {
		sb.WriteString("?")
	} else {
		sb.WriteString(strconv.Itoa(ch.Budget.Files) + "/" + strconv.Itoa(ch.Budget.Lines))
	}
	if ch.OverBudget {
		sb.WriteString(" → vượt")
	}
	sb.WriteString(") · Ngoài phạm vi: ")
	if len(ch.OutOfScope) == 0 {
		sb.WriteString("không có")
	} else {
		sb.WriteString(strings.Join(ch.OutOfScope, ", "))
	}
	return sb.String()
}
