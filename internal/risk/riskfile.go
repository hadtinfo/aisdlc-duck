package risk

import (
	"errors"
	"os"
	"time"

	"aisdlc/internal/fsx"
	"aisdlc/internal/spec"
	"aisdlc/internal/yamlx"
)

// PolicyRef ghi phiên bản và băm của chính sách đã dùng.
type PolicyRef struct {
	Version int    `yaml:"version" json:"version"`
	Hash    string `yaml:"hash" json:"hash"`
}

// Override là lần dev đổi mức tay.
type Override struct {
	From   string    `yaml:"from" json:"from"`
	To     string    `yaml:"to" json:"to"`
	By     string    `yaml:"by" json:"by"`
	Reason string    `yaml:"reason" json:"reason"`
	At     time.Time `yaml:"at" json:"at"`
}

// RiskFile là bản chụp risk.yaml: validator, harness và eval đọc Requirements ở đây, không đọc chính sách.
// Byte của file đi vào băm cổng CASES nên ghi phải ổn định (khóa map sắp theo bảng chữ cái, thứ tự field cố định).
type RiskFile struct {
	Level        string            `yaml:"level" json:"level"`
	Policy       PolicyRef         `yaml:"policy" json:"policy"`
	Matched      []Match           `yaml:"matched" json:"matched"`
	Agent        *spec.Impact      `yaml:"agent" json:"agent"`
	Override     *Override         `yaml:"override" json:"override"`
	Requirements []Requirement     `yaml:"requirements" json:"requirements"`
	Flow         map[string]string `yaml:"flow,omitempty" json:"flow,omitempty"`
}

// FlowOf chụp flow của mức: đủ bốn đơn vị; rỗng khi chính sách không khai flow cho mức.
func FlowOf(p RiskPolicy, level string) map[string]string {
	if _, ok := p.Flow[level]; !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(FlowUnits))
	for _, u := range FlowUnits {
		out[u] = p.FlowFor(level, u)
	}
	return out
}

func reqsOf(p RiskPolicy, level string) []Requirement {
	return append([]Requirement(nil), p.RequirementsFor(level)...)
}

// NewRiskFile tạo risk.yaml từ kết quả phân loại (RiskFile.of).
func NewRiskFile(p RiskPolicy, r RiskResult, agent *spec.Impact) RiskFile {
	return RiskFile{
		Level:        r.Level,
		Policy:       PolicyRef{Version: p.Version, Hash: p.Hash()},
		Matched:      append([]Match(nil), r.Matched...),
		Agent:        agent,
		Requirements: reqsOf(p, r.Level),
		Flow:         FlowOf(p, r.Level),
	}
}

// WithOverride đổi mức tay; From giữ mức do hệ thống tính ban đầu qua các lần đổi liên tiếp.
func (f RiskFile) WithOverride(p RiskPolicy, to, by, reason string, at time.Time) RiskFile {
	from := f.Level
	if f.Override != nil {
		from = f.Override.From
	}
	f.Level = to
	f.Override = &Override{From: from, To: to, By: by, Reason: reason, At: at}
	f.Requirements = reqsOf(p, to)
	f.Flow = FlowOf(p, to)
	return f
}

// RequiredByDesign: luồng FULL chỉ là "cũ" (không có risk.yaml vẫn hợp lệ) khi design.yaml không có impact.
// design.yaml có impact thì risk.yaml bắt buộc từ cổng Case. File thiếu hoặc hỏng: false (cổng Thiết kế/Case báo lỗi riêng).
func RequiredByDesign(designYaml string) (bool, error) {
	if _, err := os.Stat(designYaml); err != nil {
		return false, nil
	}
	d, err := spec.ReadDesign(designYaml)
	if err != nil {
		return false, nil
	}
	return d.Impact != nil, nil
}

// ReadRiskFile đọc risk.yaml; ok=false khi file không có.
func ReadRiskFile(path string) (RiskFile, bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return RiskFile{}, false, nil
	}
	var f RiskFile
	if err := yamlx.Read(path, &f); err != nil {
		return RiskFile{}, false, err
	}
	return f, true, nil
}

// Write ghi risk.yaml kiểu atomic.
func (f RiskFile) Write(path string) error {
	b, err := yamlx.Dump(f)
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(path, b, 0o644)
}
