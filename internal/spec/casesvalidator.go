package spec

import (
	"fmt"

	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
)

// ValidateCases kiểm cases.yaml theo các bước hiệu lực.
func ValidateCases(cases CasesSpec, steps []StepSpec, flow model.Flow) []Finding {
	var findings []Finding
	stepIDs := map[string]bool{}
	for _, s := range steps {
		stepIDs[s.ID] = true
	}
	stubIDs := map[string]bool{}
	for _, s := range cases.Stubs {
		stubIDs[s.ID] = true
	}

	if flow == model.FlowFull && len(cases.Invariants) > 0 {
		findings = append(findings, Finding{"CASES_INVARIANTS_FULL",
			i18n.T("Luồng đầy đủ khai bất biến trong design.yaml (invariants), không trong cases.yaml.")})
	}
	if flow == model.FlowLite && (len(cases.Scope) == 0 || cases.Budget == nil || !cases.Budget.Valid()) {
		findings = append(findings, Finding{"CASES_LITE_NO_SCOPE",
			i18n.T("Luồng nhẹ yêu cầu cases.yaml khai báo scope và budget hợp lệ.")})
	}

	ids := map[string]bool{}
	covered := map[string]bool{}
	hasError := false
	for _, c := range cases.Cases {
		if ids[c.ID] {
			findings = append(findings, Finding{"CASES_DUP_ID", i18n.Tf("Case %s bị trùng id.", c.ID)})
		}
		ids[c.ID] = true
		if !stepIDs[c.Step] {
			findings = append(findings, Finding{"CASES_UNKNOWN_STEP",
				i18n.Tf("Case %s trỏ tới bước %s không tồn tại.", c.ID, c.Step)})
		} else {
			covered[c.Step] = true
		}
		if c.Kind == KindError {
			hasError = true
		}
		if c.When != nil && c.When.Parallel > 1 && c.Then != nil {
			t := c.Then
			if t.Status == 0 && len(t.StatusCounts) == 0 {
				findings = append(findings, Finding{"CASE_PARALLEL_STATUS",
					i18n.Tf("Case %s chạy song song nhưng chưa khai báo status hoặc statusCounts.", c.ID)})
			}
			sum := 0
			for _, v := range t.StatusCounts {
				sum += v
			}
			if len(t.StatusCounts) > 0 && sum != c.When.Parallel {
				findings = append(findings, Finding{"CASE_STATUS_COUNTS",
					fmt.Sprintf(i18n.T("Case %s: tổng statusCounts (%d) khác when.parallel (%d)."), c.ID, sum, c.When.Parallel)})
			}
		}
		for _, stub := range c.Given.Stubs {
			if !stubIDs[stub] {
				findings = append(findings, Finding{"CASES_UNKNOWN_STUB",
					i18n.Tf("Case %s dùng stub %s chưa được khai báo.", c.ID, stub)})
			}
		}
	}
	for _, s := range steps {
		if !covered[s.ID] {
			findings = append(findings, Finding{"CASES_STEP_UNCOVERED", i18n.Tf("Bước %s chưa có case nào.", s.ID)})
		}
	}
	if !hasError {
		findings = append(findings, Finding{"CASES_NO_ERROR_CASE", i18n.T("Bộ case chưa có case lỗi (kind: error) nào.")})
	}
	for _, st := range cases.Stubs {
		findings = append(findings, validateStub(st)...)
	}
	return findings
}
