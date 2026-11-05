// Package policy chứa chính sách hook của Claude Code: agent được/không được làm gì.
package policy

import (
	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/step"
)

// Decision là quyết định của chính sách. Code là mã máy đọc được cho một số lý do chặn
// ("PAUSED", "OUT_OF_SCOPE", "AMBIGUOUS_FEATURE"); rỗng nếu không có.
type Decision struct {
	Allow  bool
	Reason string
	Code   string
}

// Ok cho phép.
func Ok() Decision { return Decision{Allow: true} }

// Deny chặn, không có mã.
func Deny(reason string) Decision { return Decision{Reason: reason} }

// DenyCode chặn kèm mã máy đọc được.
func DenyCode(code, reason string) Decision { return Decision{Reason: reason, Code: code} }

// ToolCall là một lần gọi tool của agent. FilePath và Command có thể rỗng. Cwd là thư mục làm việc của hook
// (`cwd` trong input JSON); rỗng nghĩa là gốc repo.
type ToolCall struct {
	ToolName, FilePath, Command, Cwd string
}

// PolicyState là dữ liệu chính sách cần; người gọi điền. Gates, Steps, Paths là nil khi không có tính năng
// đang làm; Active rỗng khi không có tính năng; Control có thể nil; Ambiguous là các tính năng đang mở khi
// session không phân giải được (rỗng nếu rõ).
type PolicyState struct {
	RepoRoot  string
	Config    model.RepoConfig
	Active    model.FeatureKey
	Gates     *gate.Service
	Steps     *step.Service
	Paths     *model.FeaturePaths
	Control   *snapshot.FeatureControl
	Ambiguous []model.FeatureKey
}
