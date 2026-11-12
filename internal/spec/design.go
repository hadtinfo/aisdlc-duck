package spec

import (
	"strings"

	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/yamlx"
)

// Budget là ngân sách số file và số dòng của một bước.
type Budget struct {
	Files int `yaml:"files" json:"files"`
	Lines int `yaml:"lines" json:"lines"`
}

// Valid đúng khi cả hai lớn hơn 0.
func (b Budget) Valid() bool { return b.Files > 0 && b.Lines > 0 }

// Impact là đánh giá tác động.
type Impact struct {
	Level  string   `yaml:"level"`
	Labels []string `yaml:"labels"`
	Reason string   `yaml:"reason"`
}

// StepSpec là một bước Sx của thiết kế.
type StepSpec struct {
	ID     string   `yaml:"id"`
	Title  string   `yaml:"title"`
	Scope  []string `yaml:"scope"`
	Budget *Budget  `yaml:"budget"`
}

// Invariant là bất biến DB: SQL trả các dòng vi phạm. Step (tùy chọn): chỉ kiểm từ bước đó trở đi.
type Invariant struct {
	Name string `yaml:"name"`
	SQL  string `yaml:"sql"`
	Step string `yaml:"step"`
}

// AppliesTo cho biết có kiểm sau case ở bước caseStep không. Không khai Step, không có thứ tự bước (luồng lite),
// hoặc bước không có trong thứ tự: luôn kiểm.
func (i Invariant) AppliesTo(caseStep string, stepOrder []string) bool {
	if strings.TrimSpace(i.Step) == "" || len(stepOrder) == 0 {
		return true
	}
	from, at := indexOf(stepOrder, i.Step), indexOf(stepOrder, caseStep)
	return from < 0 || at < 0 || at >= from
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// DesignSpec là nội dung design.yaml.
type DesignSpec struct {
	Steps      []StepSpec  `yaml:"steps"`
	Invariants []Invariant `yaml:"invariants"`
	Impact     *Impact     `yaml:"impact"`
}

// StepOrder trả id các bước theo thứ tự khai trong design.yaml.
func (d DesignSpec) StepOrder() []string {
	var out []string
	for _, s := range d.Steps {
		out = append(out, s.ID)
	}
	return out
}

// ReadDesign đọc design.yaml.
func ReadDesign(path string) (DesignSpec, error) {
	var d DesignSpec
	if err := yamlx.Read(path, &d); err != nil {
		return DesignSpec{}, err
	}
	return d, nil
}

// EffectiveSteps: LITE là một bước S1 lấy scope/budget từ cases; FULL là các bước của design.
func EffectiveSteps(flow model.Flow, design *DesignSpec, cases CasesSpec) []StepSpec {
	if flow == model.FlowLite {
		return []StepSpec{{ID: "S1", Title: i18n.T("Thay đổi"), Scope: cases.Scope, Budget: cases.Budget}}
	}
	if design == nil {
		return nil
	}
	return design.Steps
}

// Finding là một phát hiện của validator.
type Finding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
