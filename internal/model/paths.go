package model

import (
	"path/filepath"
	"strings"
)

// FeaturePaths cho đường dẫn tuyệt đối của các file trong một tính năng.
type FeaturePaths struct {
	RepoRoot string
	Key      FeatureKey
}

func (p FeaturePaths) abs() string {
	a, err := filepath.Abs(p.RepoRoot)
	if err != nil {
		return p.RepoRoot
	}
	return a
}

func (p FeaturePaths) Dir() string {
	return filepath.Join(p.abs(), "aisdlc", "features", string(p.Key))
}
func (p FeaturePaths) in(name string) string { return filepath.Join(p.Dir(), name) }

func (p FeaturePaths) Meta() string           { return p.in("feature.yaml") }
func (p FeaturePaths) Brd() string            { return p.in("brd.md") }
func (p FeaturePaths) DesignMd() string       { return p.in("design.md") }
func (p FeaturePaths) DesignYaml() string     { return p.in("design.yaml") }
func (p FeaturePaths) DbDrafts() string       { return p.in("db") }
func (p FeaturePaths) Cases() string          { return p.in("cases.yaml") }
func (p FeaturePaths) SchemaExpected() string { return p.in("schema-expected.json") }
func (p FeaturePaths) Gates() string          { return p.in("gates.json") }
func (p FeaturePaths) Steps() string          { return p.in("steps.json") }
func (p FeaturePaths) ScopeExtra() string     { return p.in("scope-extra.json") }
func (p FeaturePaths) Feedback() string       { return p.in("feedback.md") }
func (p FeaturePaths) EvalDir() string        { return p.in("eval") }
func (p FeaturePaths) ReviewAgent() string    { return p.in("review-agent.json") }
func (p FeaturePaths) Risk() string           { return p.in("risk.yaml") }
func (p FeaturePaths) RiskAgent() string      { return p.in("risk-agent.json") }

// Rel là đường dẫn tương đối so với repo, dùng dấu "/".
func (p FeaturePaths) Rel(path string) string {
	a, err := filepath.Abs(path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	r, err := filepath.Rel(p.abs(), a)
	if err != nil {
		return filepath.ToSlash(a)
	}
	return strings.ReplaceAll(filepath.ToSlash(r), "\\", "/")
}
