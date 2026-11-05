package review

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"time"

	"aisdlc/internal/spec"
)

// ReviewCard là thẻ review của một cổng. Phần không thuộc cổng đang xét là nil và bị bỏ khỏi JSON như
// @JsonInclude(NON_NULL) của Jackson; slice nil là null của Java, slice rỗng không nil là "[]".
// Assumptions và AssumptionsSection chỉ có ở thẻ BRD và DESIGN.
type ReviewCard struct {
	Gate               string            `json:"gate"`
	Flags              []ReviewFlag      `json:"flags"`
	Assumptions        []Assumption      `json:"assumptions"`
	AssumptionsSection *bool             `json:"assumptionsSection"`
	Brd                *BrdPart          `json:"brd"`
	Design             *DesignPart       `json:"design"`
	Cases              *CasesPart        `json:"cases"`
	Risk               *RiskPart         `json:"risk"`
	RiskRequirements   []RiskRequirement `json:"riskRequirements"`
	RiskPreview        *RiskPreview      `json:"riskPreview"`
}

// WithRiskPreview trả bản sao của thẻ kèm mức rủi ro tạm (chỉ thẻ BRD; CLI tính theo chính sách hiện tại).
func (c ReviewCard) WithRiskPreview(p RiskPreview) ReviewCard {
	c.RiskPreview = &p
	return c
}

// RiskPreview là mức rủi ro tạm lúc nộp BRD: chỉ rule keyword trên tiêu đề và brd.md; không ghi file.
type RiskPreview struct {
	Level   string      `json:"level"`
	Matched []RiskMatch `json:"matched"`
}

// RiskPart là mức rủi ro từ risk.yaml; chỉ có ở thẻ DESIGN và CASES của tính năng có risk.yaml.
type RiskPart struct {
	Level         string        `json:"level"`
	PolicyVersion int           `json:"policyVersion"`
	Matched       []RiskMatch   `json:"matched"`
	Override      *RiskOverride `json:"override"`
	Agent         *RiskAgent    `json:"agent"`
}

// RiskAgent là đánh giá tác động agent ghi lúc phân loại.
type RiskAgent struct {
	Level  string   `json:"level"`
	Labels []string `json:"labels"`
	Reason string   `json:"reason"`
}

// RiskMatch là một rule khớp.
type RiskMatch struct {
	Rule  string `json:"rule"`
	Level string `json:"level"`
	On    string `json:"on"`
}

// RiskOverride là lần dev đổi mức tay; At rỗng thì bỏ khỏi JSON.
type RiskOverride struct {
	From   string `json:"from"`
	To     string `json:"to"`
	By     string `json:"by"`
	Reason string `json:"reason"`
	At     string `json:"at"`
}

// RiskRequirement có Status là OK | MISSING | LATER; chỉ có ở thẻ CASES.
type RiskRequirement struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// BrdPart tóm tắt brd.md.
type BrdPart struct {
	Rules              int      `json:"rules"`
	RulesWithID        int      `json:"rulesWithId"`
	AcceptanceCriteria int      `json:"acceptanceCriteria"`
	OutOfScope         []string `json:"outOfScope"`
	AnsweredQuestions  []string `json:"answeredQuestions"`
}

// DesignPart tóm tắt design.md, design.yaml và SQL nháp.
type DesignPart struct {
	Steps           []StepPart         `json:"steps"`
	APILines        []string           `json:"apiLines"`
	StatusCodes     []string           `json:"statusCodes"`
	APIRows         []ApiRow           `json:"apiRows"`
	Tables          []TablePart        `json:"tables"`
	TouchesExisting []SqlStatementView `json:"touchesExisting"`
}

// StepPart: MatchedFiles tối đa 20; PlannedFiles là đích migration nháp; EmptyGlobs là glob "sẽ tạo mới".
type StepPart struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Scope        []string     `json:"scope"`
	Budget       *spec.Budget `json:"budget"`
	MatchedCount int          `json:"matchedCount"`
	MatchedFiles []string     `json:"matchedFiles"`
	PlannedFiles []string     `json:"plannedFiles"`
	EmptyGlobs   []string     `json:"emptyGlobs"`
}

// TablePart có Status NEW | EXISTING | ALTERED | DROPPED; AddedColumns chỉ có ở ALTERED.
type TablePart struct {
	Name         string      `json:"name"`
	Status       string      `json:"status"`
	Columns      []SqlColumn `json:"columns"`
	AddedColumns []SqlColumn `json:"addedColumns"`
}

// SqlStatementView là câu SQL động vào bảng có sẵn.
type SqlStatementView struct {
	Kind  string `json:"kind"`
	Table string `json:"table"`
	Text  string `json:"text"`
}

// CasesPart: RuleMatrix nil khi BRD không có ID quy tắc.
type CasesPart struct {
	Rows       []CaseRow  `json:"rows"`
	Coverage   Coverage   `json:"coverage"`
	RuleMatrix RuleMatrix `json:"ruleMatrix"`
}

// CaseRow là một dòng bảng case.
type CaseRow struct {
	ID     string   `json:"id"`
	Step   string   `json:"step"`
	Kind   string   `json:"kind"`
	Title  string   `json:"title"`
	Given  string   `json:"given"`
	Expect string   `json:"expect"`
	Rules  []string `json:"rules"`
}

// CoverageRow là số case theo loại của một bước.
type CoverageRow struct {
	Step   string
	Counts map[string]int
}

// Coverage giữ thứ tự bước (S1, S2, S10) như TreeMap của Java; ghi ra JSON thành object.
type Coverage []CoverageRow

// MarshalJSON ghi object theo thứ tự các bước.
func (c Coverage) MarshalJSON() ([]byte, error) {
	keys := make([]string, len(c))
	vals := make([]any, len(c))
	for i, r := range c {
		keys[i], vals[i] = r.Step, r.Counts
	}
	return orderedObject(keys, vals)
}

// Get trả số case theo loại của bước.
func (c Coverage) Get(step string) map[string]int {
	for _, r := range c {
		if r.Step == step {
			return r.Counts
		}
	}
	return nil
}

// RuleRow là các case theo loại của một quy tắc.
type RuleRow struct {
	Rule   string
	ByKind map[string][]string
}

// RuleMatrix giữ thứ tự quy tắc (BR-1, BR-2, BR-10); ghi ra JSON thành object.
type RuleMatrix []RuleRow

// MarshalJSON ghi object theo thứ tự các quy tắc.
func (m RuleMatrix) MarshalJSON() ([]byte, error) {
	keys := make([]string, len(m))
	vals := make([]any, len(m))
	for i, r := range m {
		keys[i], vals[i] = r.Rule, r.ByKind
	}
	return orderedObject(keys, vals)
}

// Get trả các case theo loại của quy tắc.
func (m RuleMatrix) Get(rule string) map[string][]string {
	for _, r := range m {
		if r.Rule == rule {
			return r.ByKind
		}
	}
	return nil
}

// Keys là các quy tắc theo thứ tự.
func (m RuleMatrix) Keys() []string {
	out := make([]string, len(m))
	for i, r := range m {
		out[i] = r.Rule
	}
	return out
}

// Keys là các bước theo thứ tự.
func (c Coverage) Keys() []string {
	out := make([]string, len(c))
	for i, r := range c {
		out[i] = r.Step
	}
	return out
}

func encodeJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

func orderedObject(keys []string, vals []any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := encodeJSON(k)
		if err != nil {
			return nil, err
		}
		vb, err := encodeJSON(vals[i])
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshalSkipNil ghi struct theo thứ tự field, bỏ field nil (con trỏ, slice, map) như NON_NULL của Jackson.
func marshalSkipNil(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	rt := rv.Type()
	var keys []string
	var vals []any
	for i := 0; i < rt.NumField(); i++ {
		fv := rv.Field(i)
		switch fv.Kind() {
		case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Interface:
			if fv.IsNil() {
				continue
			}
		}
		keys = append(keys, strings.Split(rt.Field(i).Tag.Get("json"), ",")[0])
		vals = append(vals, fv.Interface())
	}
	return orderedObject(keys, vals)
}

// MarshalJSON bỏ phần nil như Jackson NON_NULL.
func (c ReviewCard) MarshalJSON() ([]byte, error) { return marshalSkipNil(c) }

// MarshalJSON bỏ phần nil như Jackson NON_NULL.
func (p RiskPart) MarshalJSON() ([]byte, error) { return marshalSkipNil(p) }

// MarshalJSON bỏ At rỗng.
func (o RiskOverride) MarshalJSON() ([]byte, error) {
	keys := []string{"from", "to", "by", "reason"}
	vals := []any{o.From, o.To, o.By, o.Reason}
	if o.At != "" {
		keys, vals = append(keys, "at"), append(vals, o.At)
	}
	return orderedObject(keys, vals)
}

// MarshalJSON bỏ phần nil như Jackson NON_NULL.
func (p BrdPart) MarshalJSON() ([]byte, error) { return marshalSkipNil(p) }

// MarshalJSON bỏ phần nil như Jackson NON_NULL.
func (p DesignPart) MarshalJSON() ([]byte, error) { return marshalSkipNil(p) }

// MarshalJSON bỏ phần nil như Jackson NON_NULL.
func (p StepPart) MarshalJSON() ([]byte, error) { return marshalSkipNil(p) }

// MarshalJSON bỏ phần nil như Jackson NON_NULL.
func (p TablePart) MarshalJSON() ([]byte, error) { return marshalSkipNil(p) }

// MarshalJSON bỏ phần nil như Jackson NON_NULL.
func (p CasesPart) MarshalJSON() ([]byte, error) { return marshalSkipNil(p) }

// MarshalJSON bỏ phần nil như Jackson NON_NULL.
func (p CaseRow) MarshalJSON() ([]byte, error) { return marshalSkipNil(p) }

// InstantString viết thời điểm như Instant.toString của Java (UTC, phần lẻ theo nhóm 3 chữ số).
func InstantString(t time.Time) string {
	t = t.UTC()
	switch ns := t.Nanosecond(); {
	case ns == 0:
		return t.Format("2006-01-02T15:04:05Z")
	case ns%1_000_000 == 0:
		return t.Format("2006-01-02T15:04:05.000Z")
	case ns%1_000 == 0:
		return t.Format("2006-01-02T15:04:05.000000Z")
	default:
		return t.Format("2006-01-02T15:04:05.000000000Z")
	}
}

// SortFlags sắp cờ theo mã, mức (HIGH, WARN, INFO, NA) rồi nội dung để JSON ổn định.
func SortFlags(flags []ReviewFlag) []ReviewFlag {
	out := append([]ReviewFlag{}, flags...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if oa, ob := levelOrdinal(a.Level), levelOrdinal(b.Level); oa != ob {
			return oa < ob
		}
		return a.Message < b.Message
	})
	return out
}

func levelOrdinal(l Level) int {
	switch l {
	case HIGH:
		return 0
	case WARN:
		return 1
	case INFO:
		return 2
	}
	return 3
}
