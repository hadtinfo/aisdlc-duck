// Package eval chạy eval tầng 1 (Service: build, unit test, bộ case qua harness, hard/soft check) và đọc, ghi báo
// cáo eval (eval/report.json, report.md, review-agent.json).
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"aisdlc/internal/fsx"
	"aisdlc/internal/review"
	"aisdlc/internal/risk"
)

// HardCheck là kiểm tra cứng: phải passed thì eval mới đạt.
type HardCheck struct {
	ID     string `json:"id"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// Mức của chỉ số mềm.
const (
	SoftOK   = "OK"
	SoftWarn = "WARN"
	SoftNA   = "NA"
)

// SoftCheck là chỉ số mềm: chỉ cảnh báo. Level là OK, WARN hoặc NA.
type SoftCheck struct {
	ID     string `json:"id"`
	Level  string `json:"level"`
	Detail string `json:"detail"`
}

// ConfigChanged giữ nguyên văn EvalService.CONFIG_CHANGED.
const ConfigChanged = " bị sửa trong thay đổi này — sửa cấu hình phải ở MR riêng"

// RiskSection là mục rủi ro của report; nil khi tính năng không có risk.yaml nên report cũ không đổi.
// Override nil là null của Java.
type RiskSection struct {
	Level         string                   `json:"level"`
	PolicyVersion int                      `json:"policyVersion"`
	Matched       []string                 `json:"matched"`
	Override      *string                  `json:"override"`
	Requirements  []risk.RequirementStatus `json:"requirements"`
}

// EvalReport là kết quả eval tầng 1 (eval/report.json). Passed = mọi hard check đều passed.
// EvaluatedTree là snapshot tree của working tree đã eval, không gồm thư mục tính năng.
type EvalReport struct {
	Feature       string
	At            time.Time
	Mode          string
	Hard          []HardCheck
	Soft          []SoftCheck
	Passed        bool
	EvaluatedTree string
	Risk          *RiskSection
}

type reportJSON struct {
	Feature       string       `json:"feature"`
	At            string       `json:"at"`
	Mode          string       `json:"mode"`
	Hard          []HardCheck  `json:"hard"`
	Soft          []SoftCheck  `json:"soft"`
	Passed        bool         `json:"passed"`
	EvaluatedTree string       `json:"evaluatedTree"`
	Risk          *RiskSection `json:"risk,omitempty"`
}

// NewEvalReport tạo report với Passed = mọi hard check đều passed.
func NewEvalReport(feature string, at time.Time, mode string, hard []HardCheck, soft []SoftCheck, evaluatedTree string, rs *RiskSection) EvalReport {
	passed := true
	for _, h := range hard {
		passed = passed && h.Passed
	}
	return EvalReport{feature, at, mode, hard, soft, passed, evaluatedTree, rs}
}

// MarshalJSON ghi như Jackson: thời gian UTC kiểu Instant, danh sách không bao giờ null, risk vắng khi nil.
func (r EvalReport) MarshalJSON() ([]byte, error) {
	j := reportJSON{r.Feature, review.InstantString(r.At), r.Mode, r.Hard, r.Soft, r.Passed, r.EvaluatedTree, r.Risk}
	if j.Hard == nil {
		j.Hard = []HardCheck{}
	}
	if j.Soft == nil {
		j.Soft = []SoftCheck{}
	}
	if j.Risk != nil {
		k := *j.Risk
		if k.Matched == nil {
			k.Matched = []string{}
		}
		if k.Requirements == nil {
			k.Requirements = []risk.RequirementStatus{}
		}
		j.Risk = &k
	}
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	if err := e.Encode(j); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// UnmarshalJSON đọc report, bỏ qua khóa lạ.
func (r *EvalReport) UnmarshalJSON(b []byte) error {
	var j reportJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	var at time.Time
	if j.At != "" {
		t, err := time.Parse(time.RFC3339Nano, j.At)
		if err != nil {
			return err
		}
		at = t.UTC()
	}
	*r = EvalReport{j.Feature, at, j.Mode, j.Hard, j.Soft, j.Passed, j.EvaluatedTree, j.Risk}
	return nil
}

// ReadEvalReport đọc eval/report.json.
func ReadEvalReport(path string) (EvalReport, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return EvalReport{}, fmt.Errorf("Không đọc được %s: %v", path, err)
	}
	var r EvalReport
	if err := json.Unmarshal(b, &r); err != nil {
		return EvalReport{}, fmt.Errorf("Không đọc được %s: %v", path, err)
	}
	return r, nil
}

// Write ghi report (thụt 2 dấu cách, kết thúc bằng xuống dòng) qua fsx.WriteAtomic.
func (r EvalReport) Write(path string) error {
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(r); err != nil {
		return fmt.Errorf("Không ghi được %s: %v", path, err)
	}
	return fsx.WriteAtomic(path, buf.Bytes(), 0o644)
}
