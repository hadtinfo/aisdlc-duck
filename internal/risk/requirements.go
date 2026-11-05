package risk

import (
	"fmt"
	"regexp"
	"strings"

	"aisdlc/internal/review"
	"aisdlc/internal/spec"
)

// RequirementState là kết quả kiểm một yêu cầu ở cổng Case.
type RequirementState string

const (
	RequirementOK      RequirementState = "OK"      // đạt
	RequirementMissing RequirementState = "MISSING" // thiếu
	RequirementLater   RequirementState = "LATER"   // kiểm ở bước sau
)

// RequirementStatus là kết quả kiểm một yêu cầu.
type RequirementStatus struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Status RequirementState `json:"status"`
	Detail string           `json:"detail"`
}

var (
	mutating = map[string]bool{"POST": true, "PUT": true, "PATCH": true, "DELETE": true}
	apiLine  = regexp.MustCompile(`\b(GET|POST|PUT|PATCH|DELETE)\s+(/\S*)`)
	trailing = regexp.MustCompile("[`,;:.)|*\"'\\]]+$")
	pathVar  = regexp.MustCompile(`\{[^}/]*\}`)
)

const noBRD = "không có BRD"

// CheckRequirements kiểm các yêu cầu trong risk.yaml với cases.yaml (và BRD, design khi có) ở cổng Case.
// design, brd, designDoc là nil ở luồng lite (con trỏ vì "không có" khác "rỗng": BRD nil cho LATER).
func CheckRequirements(risk RiskFile, cases spec.CasesSpec, design *spec.DesignSpec, brd *review.BrdDoc, designDoc *review.DesignDoc) []RequirementStatus {
	out := []RequirementStatus{}
	cs := cases.Cases
	for _, r := range risk.Requirements {
		if !RequirementKnown(r.ID) {
			continue
		}
		name := RequirementDisplayName(r)
		var s RequirementStatus
		switch r.ID {
		case ReqRulePosNeg:
			s = rulePosNeg(r, name, cs, brd)
		case ReqBoundary:
			s = boundary(r, name, cs, brd)
		case ReqIdempotent:
			s = idempotent(r, name, cs, designDoc)
		case ReqConcurrent:
			s = concurrent(r, name, cs)
		case ReqInvariant:
			s = invariant(r, name, design, cases)
		case ReqCoverage:
			s = RequirementStatus{r.ID, name, RequirementLater, "kiểm ở Review"}
		default:
			s = RequirementStatus{r.ID, name, RequirementLater, "harness chạy sau mọi case"}
		}
		out = append(out, s)
	}
	return out
}

// RequirementFindings: mỗi mục MISSING thành một finding RISK_REQ.
func RequirementFindings(risk RiskFile, st []RequirementStatus) []spec.Finding {
	out := []spec.Finding{}
	for _, s := range st {
		if s.Status == RequirementMissing {
			out = append(out, spec.Finding{Code: "RISK_REQ", Message: "Mức " + risk.Level + " yêu cầu " + s.Name + ": " + s.Detail})
		}
	}
	return out
}

func statusOf(r Requirement, name string, missing []string, detail string) RequirementStatus {
	if len(missing) == 0 {
		return RequirementStatus{r.ID, name, RequirementOK, ""}
	}
	return RequirementStatus{r.ID, name, RequirementMissing, detail + strings.Join(missing, ", ")}
}

func covers(cs []spec.CaseSpec, rule string, kinds ...spec.CaseKind) bool {
	for _, c := range cs {
		hasKind := false
		for _, k := range kinds {
			hasKind = hasKind || c.Kind == k
		}
		if !hasKind {
			continue
		}
		for _, r := range c.Rules {
			if r == rule {
				return true
			}
		}
	}
	return false
}

func rulePosNeg(r Requirement, name string, cs []spec.CaseSpec, brd *review.BrdDoc) RequirementStatus {
	if brd == nil {
		return RequirementStatus{r.ID, name, RequirementLater, noBRD}
	}
	var noPos, noNeg []string
	for _, rule := range brd.Rules {
		if rule.ID == "" {
			continue
		}
		if !covers(cs, rule.ID, spec.KindHappy) {
			noPos = append(noPos, rule.ID)
		}
		if !covers(cs, rule.ID, spec.KindError, spec.KindBoundary) {
			noNeg = append(noNeg, rule.ID)
		}
	}
	var parts []string
	if len(noPos) > 0 {
		parts = append(parts, "thiếu case đúng cho "+strings.Join(noPos, ", "))
	}
	if len(noNeg) > 0 {
		parts = append(parts, "thiếu case sai hoặc biên cho "+strings.Join(noNeg, ", "))
	}
	if len(parts) == 0 {
		return RequirementStatus{r.ID, name, RequirementOK, ""}
	}
	return RequirementStatus{r.ID, name, RequirementMissing, strings.Join(parts, "; ")}
}

func boundary(r Requirement, name string, cs []spec.CaseSpec, brd *review.BrdDoc) RequirementStatus {
	if brd == nil {
		return RequirementStatus{r.ID, name, RequirementLater, noBRD}
	}
	min := r.IntParam("min", 1)
	var missing []string
	for _, rule := range brd.Rules {
		if !rule.Boundary {
			continue
		}
		n := 0
		if rule.ID != "" {
			for _, c := range cs {
				if c.Kind != spec.KindBoundary {
					continue
				}
				for _, id := range c.Rules {
					if id == rule.ID {
						n++
						break
					}
				}
			}
		}
		if n < min {
			if rule.ID != "" {
				missing = append(missing, rule.ID)
			} else {
				missing = append(missing, rule.Text)
			}
		}
	}
	return statusOf(r, name, missing, fmt.Sprintf("rule gắn [biên] thiếu case biên (cần %d): ", min))
}

func idempotent(r Requirement, name string, cs []spec.CaseSpec, doc *review.DesignDoc) RequirementStatus {
	var endpoints [][2]string
	if doc != nil {
		for _, line := range doc.APILines {
			for _, m := range apiLine.FindAllStringSubmatch(line, -1) {
				// như RiskInputsCollector: bỏ backtick, dấu câu cuối (`POST /x`, body ...)
				path := trailing.ReplaceAllString(m[2], "")
				if mutating[m[1]] {
					endpoints = append(endpoints, [2]string{m[1], path})
				}
			}
		}
	}
	var repeats []spec.CaseSpec
	for _, c := range cs {
		if c.When != nil && c.When.Repeat >= 2 {
			repeats = append(repeats, c)
		}
	}
	if len(endpoints) == 0 {
		if len(repeats) == 0 {
			return RequirementStatus{r.ID, name, RequirementMissing, "chưa có case nào gửi trùng (when.repeat >= 2)"}
		}
		return RequirementStatus{r.ID, name, RequirementOK, ""}
	}
	var missing []string
	for _, e := range endpoints {
		re := pathPattern(e[1])
		covered := false
		for _, c := range repeats {
			if strings.EqualFold(e[0], c.When.Method) && c.When.Path != "" && re.MatchString(stripQuery(c.When.Path)) {
				covered = true
				break
			}
		}
		if !covered {
			missing = append(missing, e[0]+" "+e[1])
		}
	}
	return statusOf(r, name, missing, "endpoint ghi chưa có case gửi trùng (when.repeat >= 2): ")
}

func stripQuery(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i]
	}
	return path
}

// pathPattern: {x} khớp mọi đoạn đường dẫn; khớp toàn chuỗi, không phân biệt hoa thường.
func pathPattern(path string) *regexp.Regexp {
	p := stripQuery(path)
	var re strings.Builder
	re.WriteString("(?i)^")
	last := 0
	for _, loc := range pathVar.FindAllStringIndex(p, -1) {
		re.WriteString(regexp.QuoteMeta(p[last:loc[0]]) + "[^/]+")
		last = loc[1]
	}
	re.WriteString(regexp.QuoteMeta(p[last:]) + "$")
	return regexp.MustCompile(re.String())
}

func concurrent(r Requirement, name string, cs []spec.CaseSpec) RequirementStatus {
	threads := r.IntParam("threads", 0)
	for _, c := range cs {
		if c.When != nil && c.When.Parallel >= threads {
			return RequirementStatus{r.ID, name, RequirementOK, ""}
		}
	}
	return RequirementStatus{r.ID, name, RequirementMissing, fmt.Sprintf("chưa có case nào có when.parallel >= %d", threads)}
}

// invariant: FULL đếm bất biến trong design.yaml; LITE (không có design.yaml) đếm trong cases.yaml.
func invariant(r Requirement, name string, design *spec.DesignSpec, cases spec.CasesSpec) RequirementStatus {
	min := r.IntParam("min", 1)
	n, file := len(cases.Invariants), "cases.yaml"
	if design != nil {
		n, file = len(design.Invariants), "design.yaml"
	}
	if n >= min {
		return RequirementStatus{r.ID, name, RequirementOK, ""}
	}
	return RequirementStatus{r.ID, name, RequirementMissing, fmt.Sprintf("%s có %d/%d invariants", file, n, min)}
}
