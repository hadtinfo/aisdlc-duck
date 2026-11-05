package harness

import (
	"encoding/json"
	"os"
	"path/filepath"

	"aisdlc/internal/fsx"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
)

// WriteResults ghi outDir/results.json cho CLI đọc.
func WriteResults(outDir, feature, mode string, cases []run.CaseResult) error {
	if cases == nil {
		cases = []run.CaseResult{}
	}
	b, err := json.MarshalIndent(struct {
		Feature string           `json:"feature"`
		Mode    string           `json:"mode"`
		Cases   []run.CaseResult `json:"cases"`
	}{feature, mode, cases}, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(filepath.Join(outDir, "results.json"), b, 0o644)
}

// ReadResults đọc outDir/results.json. Case được chọn mà không có dòng kết quả thành ERROR NoRow; thiếu file hoặc
// file hỏng thì mọi case được chọn ERROR NoResults: harness bỏ sót case không bao giờ được coi là pass.
func ReadResults(outDir string, selected []spec.CaseSpec) []run.CaseResult {
	if b, err := os.ReadFile(filepath.Join(outDir, "results.json")); err == nil {
		var f struct {
			Cases []run.CaseResult `json:"cases"`
		}
		if json.Unmarshal(b, &f) == nil && validOutcomes(f.Cases) {
			out := append([]run.CaseResult{}, f.Cases...)
			seen := map[string]bool{}
			for _, c := range out {
				seen[c.ID] = true
			}
			for _, c := range selected {
				if !seen[c.ID] {
					out = append(out, run.CaseResult{ID: c.ID, Step: c.Step, Outcome: run.CaseOutcomeError, Message: NoRow})
				}
			}
			return out
		}
	}
	out := make([]run.CaseResult, 0, len(selected))
	for _, c := range selected {
		out = append(out, run.CaseResult{ID: c.ID, Step: c.Step, Outcome: run.CaseOutcomeError, Message: NoResults})
	}
	return out
}

func validOutcomes(cs []run.CaseResult) bool {
	for _, c := range cs {
		switch c.Outcome {
		case run.CaseOutcomePassed, run.CaseOutcomeFailed, run.CaseOutcomeError:
		default:
			return false
		}
	}
	return true
}
