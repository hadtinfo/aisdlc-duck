package gate

import (
	"strings"
	"time"

	"aisdlc/internal/spec"
)

const (
	// ApprovalAuto là người duyệt của bản tự duyệt.
	ApprovalAuto = "auto"
	// ApprovalClaudeCode là kênh: dev bấm Yes trong hộp xác nhận quyền của Claude Code.
	ApprovalClaudeCode = "claude-code"
)

// Approval là bản duyệt một cổng. Rule: lý do tự duyệt ("flow:<level>"), rỗng với bản duyệt tay. Superseded: bản
// tự duyệt bị thay thế khi nâng mức, không còn được tính là đã duyệt. Channel: kênh xác nhận khi khác terminal.
type Approval struct {
	Gate       Gate      `json:"gate"`
	Hash       string    `json:"hash"`
	Approver   string    `json:"approver"`
	At         time.Time `json:"at"`
	Rule       string    `json:"rule,omitempty"`
	Superseded bool      `json:"superseded,omitempty"`
	Channel    string    `json:"channel,omitempty"`
}

// IsAuto cho biết đây là bản tự duyệt.
func (a Approval) IsAuto() bool { return a.Approver == ApprovalAuto }

// RuleLevel là mức trong Rule ("flow:low" thành "low"); rỗng khi không có.
func (a Approval) RuleLevel() string {
	if strings.HasPrefix(a.Rule, "flow:") {
		return strings.TrimPrefix(a.Rule, "flow:")
	}
	return ""
}

// ApprovalFromWeb là bản duyệt đọc từ web: chỉ channel = "auto" là bản tự duyệt (giữ rule). Kênh khác là duyệt
// tay: bỏ rule, giữ channel, và người duyệt trùng tên "auto" ghi kèm kênh để không bị coi là tự duyệt.
func ApprovalFromWeb(gate Gate, hash, approver string, at time.Time, channel, rule string) Approval {
	if channel == ApprovalAuto {
		return Approval{Gate: gate, Hash: hash, Approver: ApprovalAuto, At: at, Rule: rule}
	}
	who := approver
	if approver == ApprovalAuto {
		who = approver + " (" + channel + ")"
	}
	return Approval{Gate: gate, Hash: hash, Approver: who, At: at, Channel: channel}
}

// AsSuperseded trả bản sao đã đánh dấu bị thay thế.
func (a Approval) AsSuperseded() Approval {
	a.Superseded = true
	return a
}

// Submission là một lần nộp cổng.
type Submission struct {
	Gate     Gate           `json:"gate"`
	Hash     string         `json:"hash"`
	At       time.Time      `json:"at"`
	Findings []spec.Finding `json:"findings"`
}
