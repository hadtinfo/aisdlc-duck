// Package rewind tính kế hoạch quay tính năng về một mốc (gói thuần, không gọi git).
package rewind

import (
	"os"
	"strings"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
)

// Checkpoint là một mốc quay về. ID: "brd"|"design"|"cases"|"S2"|"review".
// Gate khác rỗng với mốc cổng; Step khác rỗng với mốc bước.
type Checkpoint struct {
	ID   string
	Gate gate.Gate
	Step string
}

// Input là dữ kiện để tính mốc và kế hoạch.
type Input struct {
	Key     model.FeatureKey
	Flow    model.Flow
	Paths   model.FeaturePaths
	Steps   []spec.StepSpec
	Records []step.StepRecord
	// Reached: cổng đã có bản nộp/duyệt hoặc là cổng đang mở.
	Reached func(gate.Gate) bool
	// RiskFromDesign: luồng đầy đủ, risk.yaml sinh ở submit design.
	RiskFromDesign bool
}

// Plan là kế hoạch quay về mốc.
type Plan struct {
	Target         Checkpoint
	ClearGates     []gate.Gate // từ mốc trở đi (mốc Sx/review: chỉ REVIEW)
	DropSteps      []string    // Sk..Sn có bản ghi
	DeletePaths    []string    // artifact tuyệt đối sẽ xoá (chỉ những file đang tồn tại)
	ScopeSteps     []string    // bước cần RemoveSteps
	CodeTree       string      // "" = không đổi code
	OwnChanges     []gitx.FileChange
	ForeignChanges []gitx.FileChange
}

func (in Input) record(id string) (step.StepRecord, bool) {
	for _, r := range in.Records {
		if r.ID == id {
			return r, true
		}
	}
	return step.StepRecord{}, false
}

func gateID(g gate.Gate) string { return strings.ToLower(string(g)) }

// Checkpoints liệt kê các mốc đã đạt tới theo thứ tự luồng.
func Checkpoints(in Input) []Checkpoint {
	var out []Checkpoint
	for _, g := range gate.Sequence(in.Flow) {
		if g == gate.REVIEW {
			continue
		}
		if in.Reached != nil && in.Reached(g) {
			out = append(out, Checkpoint{ID: gateID(g), Gate: g})
		}
	}
	for _, s := range in.Steps {
		if _, ok := in.record(s.ID); ok {
			out = append(out, Checkpoint{ID: s.ID, Step: s.ID})
		}
	}
	if in.Reached != nil && in.Reached(gate.REVIEW) {
		out = append(out, Checkpoint{ID: "review", Gate: gate.REVIEW})
	}
	return out
}

// Parse đổi tham số dòng lệnh thành mốc (không phân biệt hoa thường).
func Parse(in Input, arg string) (Checkpoint, error) {
	cps := Checkpoints(in)
	a := strings.TrimSpace(arg)
	ids := make([]string, 0, len(cps))
	for _, c := range cps {
		if strings.EqualFold(c.ID, a) {
			return c, nil
		}
		ids = append(ids, c.ID)
	}
	return Checkpoint{}, errorf(i18n.Tf("Mốc %q không hợp lệ hoặc chưa đạt tới. Các mốc hợp lệ: %s", arg, strings.Join(ids, ", ")))
}

type planError struct{ msg string }

func (e *planError) Error() string { return e.msg }

func errorf(msg string) error { return &planError{msg} }

// Build tính kế hoạch xoá cho mốc cp theo bảng trong thiết kế.
func Build(in Input, cp Checkpoint) (Plan, error) {
	p := in.Paths
	pl := Plan{Target: cp}
	var cand []string

	switch {
	case cp.Step != "":
		idx := -1
		for i, s := range in.Steps {
			if s.ID == cp.Step {
				idx = i
				break
			}
		}
		if idx < 0 {
			return Plan{}, errorf(i18n.Tf("Không tìm thấy bước %s trong thiết kế", cp.Step))
		}
		for _, s := range in.Steps[idx:] {
			pl.ScopeSteps = append(pl.ScopeSteps, s.ID)
			if _, ok := in.record(s.ID); ok {
				pl.DropSteps = append(pl.DropSteps, s.ID)
			}
		}
		pl.ClearGates = []gate.Gate{gate.REVIEW}
		cand = []string{p.EvalDir(), p.ReviewAgent()}
		if r, ok := in.record(cp.Step); ok {
			pl.CodeTree = r.StartTree
		}
	case cp.Gate == gate.REVIEW:
		pl.ClearGates = []gate.Gate{gate.REVIEW}
		cand = []string{p.EvalDir(), p.ReviewAgent()}
	default:
		seq := gate.Sequence(in.Flow)
		from := -1
		for i, g := range seq {
			if g == cp.Gate {
				from = i
				break
			}
		}
		if from < 0 {
			return Plan{}, errorf(i18n.Tf("Mốc %s không thuộc luồng của tính năng", cp.ID))
		}
		pl.ClearGates = append(pl.ClearGates, seq[from:]...)
		for _, s := range in.Steps {
			if _, ok := in.record(s.ID); ok {
				pl.DropSteps = append(pl.DropSteps, s.ID)
			}
		}
		if len(in.Steps) > 0 {
			if r, ok := in.record(in.Steps[0].ID); ok {
				pl.CodeTree = r.StartTree
			}
		}
		switch cp.Gate {
		case gate.BRD:
			cand = append(cand, p.Brd())
			fallthrough
		case gate.DESIGN:
			cand = append(cand, p.DesignMd(), p.DesignYaml(), p.DbDrafts(), p.SchemaExpected(), p.Cases(),
				p.Risk(), p.RiskAgent())
		case gate.CASES:
			cand = append(cand, p.Cases())
			if !in.RiskFromDesign {
				cand = append(cand, p.Risk(), p.RiskAgent())
			}
			if in.Flow != model.FlowFull {
				cand = append(cand, p.SchemaExpected())
			}
		}
		cand = append(cand, p.EvalDir(), p.ReviewAgent(), p.ScopeExtra())
	}

	for _, c := range cand {
		if _, err := os.Stat(c); err == nil {
			pl.DeletePaths = append(pl.DeletePaths, c)
		}
	}
	return pl, nil
}
