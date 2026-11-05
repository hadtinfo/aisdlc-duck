package spec

import (
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

// ValidateDesign kiểm design.md và design.yaml. levels == nil thì bỏ kiểm impact.
func ValidateDesign(designMd string, spec DesignSpec, levels []string) []Finding {
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
			"design.md thiếu sơ đồ tuần tự (khối ```mermaid bắt đầu bằng sequenceDiagram)."})
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
					"Bước " + s.ID + " có trong design.yaml nhưng không xuất hiện trong sơ đồ tuần tự."})
			}
		}
		for _, id := range inDiagram {
			if !inYaml[id] {
				findings = append(findings, Finding{"DESIGN_UNKNOWN_STEP_IN_DIAGRAM",
					"Sơ đồ tuần tự nhắc bước " + id + " nhưng design.yaml không khai báo."})
			}
		}
	}

	seen := map[string]bool{}
	for _, s := range spec.Steps {
		if seen[s.ID] {
			findings = append(findings, Finding{"DESIGN_DUP_STEP", "Bước " + s.ID + " bị khai báo trùng trong design.yaml."})
		}
		seen[s.ID] = true
		if len(s.Scope) == 0 {
			findings = append(findings, Finding{"DESIGN_STEP_NO_SCOPE", "Bước " + s.ID + " chưa khai báo scope."})
		}
		if s.Budget == nil || !s.Budget.Valid() {
			findings = append(findings, Finding{"DESIGN_STEP_NO_BUDGET",
				"Bước " + s.ID + " thiếu budget hợp lệ (files và lines phải > 0)."})
		}
	}
	for _, inv := range spec.Invariants {
		if inv.Step != "" && !seen[inv.Step] {
			findings = append(findings, Finding{"DESIGN_INVARIANT_STEP", "Bất biến '" + inv.Name + "' khai step " +
				inv.Step + " nhưng design.yaml không có bước này."})
		}
	}
	if levels != nil {
		if spec.Impact == nil {
			findings = append(findings, Finding{"IMPACT_MISSING", "design.yaml thiếu khối impact (level, labels, reason)."})
		} else if !containsStr(levels, spec.Impact.Level) {
			findings = append(findings, Finding{"IMPACT_LEVEL",
				"impact.level \"" + spec.Impact.Level + "\" không thuộc các mức hợp lệ [" + strings.Join(levels, ", ") + "]."})
		}
	}
	for _, q := range OpenQuestions(designMd) {
		findings = append(findings, Finding{"DESIGN_OPEN_QUESTION", "Câu hỏi mở chưa được trả lời: " + q})
	}
	return findings
}

func containsStr(l []string, s string) bool { return indexOf(l, s) >= 0 }
