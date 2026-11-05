package risk

// RiskFile là bản chụp risk.yaml. Task 7 bổ sung đủ field; ở đây chỉ có phần AutoStep cần.
type RiskFile struct {
	Flow map[string]string `yaml:"flow,omitempty" json:"flow,omitempty"`
}
