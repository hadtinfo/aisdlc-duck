// Package run chứa kiểu kết quả chạy bộ case.
package run

// CaseOutcome là kết quả một case; ghi như Jackson ghi enum.
type CaseOutcome string

const (
	CaseOutcomePassed CaseOutcome = "PASSED"
	CaseOutcomeFailed CaseOutcome = "FAILED"
	CaseOutcomeError  CaseOutcome = "ERROR"
)

// CaseResult là một dòng trong results.json của harness.
type CaseResult struct {
	ID      string      `json:"id"`
	Step    string      `json:"step"`
	Outcome CaseOutcome `json:"outcome"`
	Message string      `json:"message"`
}

// CaseRunResult là kết quả cả lần chạy.
type CaseRunResult struct {
	ExitCode int
	Cases    []CaseResult
}

// Passed đếm case PASSED.
func (r CaseRunResult) Passed() int64 {
	var n int64
	for _, c := range r.Cases {
		if c.Outcome == CaseOutcomePassed {
			n++
		}
	}
	return n
}

// NotPassedIDs là id các case không PASSED, theo thứ tự.
func (r CaseRunResult) NotPassedIDs() []string {
	out := []string{}
	for _, c := range r.Cases {
		if c.Outcome != CaseOutcomePassed {
			out = append(out, c.ID)
		}
	}
	return out
}
