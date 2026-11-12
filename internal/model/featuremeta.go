package model

import (
	"time"

	"aisdlc/internal/yamlx"
)

// FeatureMeta là nội dung feature.yaml.
type FeatureMeta struct {
	Key       FeatureKey `yaml:"key" json:"key"`
	Title     string     `yaml:"title" json:"title"`
	Flow      Flow       `yaml:"flow" json:"flow"`
	BaseTree  string     `yaml:"baseTree" json:"baseTree"`
	CreatedAt time.Time  `yaml:"createdAt" json:"createdAt"`
	Owner     string     `yaml:"owner" json:"owner"`
	// RiskSchema là schema chính sách rủi ro lúc tạo tính năng (0 = chưa ghi).
	RiskSchema int `yaml:"riskSchema,omitempty" json:"riskSchema,omitempty"`
}

// ReadFeatureMeta đọc feature.yaml.
func ReadFeatureMeta(path string) (FeatureMeta, error) {
	var m FeatureMeta
	if err := yamlx.Read(path, &m); err != nil {
		return FeatureMeta{}, err
	}
	return m, nil
}

// Write ghi feature.yaml.
func (m FeatureMeta) Write(path string) error { return yamlx.Write(path, m) }
