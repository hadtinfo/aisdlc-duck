package gate

import (
	"os"
	"path/filepath"

	"aisdlc/internal/model"
)

// Gate là một cổng duyệt; chuỗi trùng với tên enum Java.
type Gate string

const (
	BRD    Gate = "BRD"
	DESIGN Gate = "DESIGN"
	CASES  Gate = "CASES"
	REVIEW Gate = "REVIEW"
)

// GateState là trạng thái của một cổng.
type GateState string

const (
	LockedByPrevious      GateState = "LOCKED_BY_PREVIOUS"
	NotSubmitted          GateState = "NOT_SUBMITTED"
	SubmittedWithFindings GateState = "SUBMITTED_WITH_FINDINGS"
	AwaitingApproval      GateState = "AWAITING_APPROVAL"
	Approved              GateState = "APPROVED"
	Stale                 GateState = "STALE"
)

// ApprovalMode là nơi duyệt cổng: cục bộ (gates.json) hay trên web.
type ApprovalMode string

const (
	ModeLocal ApprovalMode = "LOCAL"
	ModeWeb   ApprovalMode = "WEB"
)

// Sequence là thứ tự cổng của luồng.
func Sequence(flow model.Flow) []Gate {
	if flow == model.FlowFull {
		return []Gate{BRD, DESIGN, CASES, REVIEW}
	}
	return []Gate{CASES, REVIEW}
}

// Artifacts là danh sách file tạo nên hash của cổng, theo thứ tự cố định. CASES chỉ gồm cases.yaml,
// schema-expected.json (LITE) và risk.yaml khi file có; không còn file suite test.
func (g Gate) Artifacts(p model.FeaturePaths, f model.Flow) []string {
	var out []string
	switch g {
	case BRD:
		out = append(out, p.Brd())
	case DESIGN:
		out = append(out, p.DesignMd(), p.DesignYaml())
		out = append(out, model.DbDraftFiles(p.DbDrafts())...)
		out = append(out, p.SchemaExpected())
	case CASES:
		out = append(out, p.Cases())
		if f == model.FlowLite {
			out = append(out, p.SchemaExpected())
		}
		// chỉ khi có file: tính năng cũ (không risk.yaml) giữ nguyên hash
		if _, err := os.Stat(p.Risk()); err == nil {
			out = append(out, p.Risk())
		}
	case REVIEW:
		out = append(out, filepath.Join(p.EvalDir(), "report.json"), p.ReviewAgent())
	}
	return out
}

// ArtifactContents là nội dung artifact đúng thứ tự Artifacts; Data == nil khi file không có.
func (g Gate) ArtifactContents(p model.FeaturePaths, f model.Flow) ([]model.Content, error) {
	return model.Contents(p.RepoRoot, g.Artifacts(p, f))
}
