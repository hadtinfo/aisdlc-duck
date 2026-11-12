package spec

import (
	"aisdlc/internal/i18n"
	"regexp"
	"strings"
)

var (
	mermaidRe = regexp.MustCompile("(?s)```mermaid[^\n]*\r?\n(.*?)```")
	stepRefRe = regexp.MustCompile(`\bS\d+\b`)
)

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

func firstNonBlankLine(s string) string {
	for _, l := range newlineRe.Split(s, -1) {
		if !isBlank(l) {
			return strings.TrimSpace(l)
		}
	}
	return ""
}

// ValidateDesign kiểm design.md và design.yaml. Khối impact không còn được kiểm (xem ImpactDeprecationWarning).
func ValidateDesign(designMd string, spec DesignSpec) []Finding {
	var findings []Finding

	var sequences strings.Builder
	hasSequence := false
	for _, m := range mermaidRe.FindAllStringSubmatch(designMd, -1) {
		if firstNonBlankLine(m[1]) == "sequenceDiagram" {
			hasSequence = true
			sequences.WriteString(m[1])
			sequences.WriteByte('\n')
		}
	}
	if !hasSequence {
		findings = append(findings, Finding{"DESIGN_NO_SEQUENCE",
			i18n.T("design.md thiếu sơ đồ tuần tự (khối ```mermaid bắt đầu bằng sequenceDiagram).")})
	} else {
		var inDiagram []string
		seenRef := map[string]bool{}
		for _, r := range stepRefRe.FindAllString(sequences.String(), -1) {
			if !seenRef[r] {
				seenRef[r] = true
				inDiagram = append(inDiagram, r)
			}
		}
		inYaml := map[string]bool{}
		for _, s := range spec.Steps {
			inYaml[s.ID] = true
		}
		for _, s := range spec.Steps {
			if !seenRef[s.ID] {
				findings = append(findings, Finding{"DESIGN_STEP_NOT_IN_DIAGRAM",
					i18n.Tf("Bước %s có trong design.yaml nhưng không xuất hiện trong sơ đồ tuần tự.", s.ID)})
			}
		}
		for _, id := range inDiagram {
			if !inYaml[id] {
				findings = append(findings, Finding{"DESIGN_UNKNOWN_STEP_IN_DIAGRAM",
					i18n.Tf("Sơ đồ tuần tự nhắc bước %s nhưng design.yaml không khai báo.", id)})
			}
		}
	}

	seen := map[string]bool{}
	for _, s := range spec.Steps {
		if seen[s.ID] {
			findings = append(findings, Finding{"DESIGN_DUP_STEP", i18n.Tf("Bước %s bị khai báo trùng trong design.yaml.", s.ID)})
		}
		seen[s.ID] = true
		if len(s.Scope) == 0 {
			findings = append(findings, Finding{"DESIGN_STEP_NO_SCOPE", i18n.Tf("Bước %s chưa khai báo scope.", s.ID)})
		}
		if s.Budget == nil || !s.Budget.Valid() {
			findings = append(findings, Finding{"DESIGN_STEP_NO_BUDGET",
				i18n.Tf("Bước %s thiếu budget hợp lệ (files và lines phải > 0).", s.ID)})
		}
	}
	for _, inv := range spec.Invariants {
		if inv.Step != "" && !seen[inv.Step] {
			findings = append(findings, Finding{"DESIGN_INVARIANT_STEP", i18n.Tf("Bất biến '%s' khai step %s nhưng design.yaml không có bước này.", inv.Name, inv.Step)})
		}
	}
	for _, q := range OpenQuestions(designMd) {
		findings = append(findings, Finding{"DESIGN_OPEN_QUESTION", i18n.Tf("Câu hỏi mở chưa được trả lời: %s", q)})
	}
	return findings
}

func containsStr(l []string, s string) bool { return indexOf(l, s) >= 0 }

// ImpactDeprecationWarning là dòng cảnh báo không chặn khi file còn khối impact (design.yaml hoặc cases.yaml cũ);
// "" khi không còn khối này.
func ImpactDeprecationWarning(i *Impact) string {
	if i == nil {
		return ""
	}
	return i18n.T("IMPACT_DEPRECATED: Khối impact không còn dùng: subagent aisdlc-risk-classifier xếp loại rủi ro; xóa khối này.")
}
