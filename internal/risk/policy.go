package risk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"aisdlc/internal/model"
	"aisdlc/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// MatchKind là loại khớp của một rule; chuỗi như Jackson ghi (chữ thường).
type MatchKind string

const (
	MatchKeyword  MatchKind = "keyword"
	MatchPath     MatchKind = "path"
	MatchTable    MatchKind = "table"
	MatchEndpoint MatchKind = "endpoint"
	MatchLabel    MatchKind = "label"
)

var matchKinds = []MatchKind{MatchKeyword, MatchPath, MatchTable, MatchEndpoint, MatchLabel}

// AgentMode: agent có được nâng mức không.
type AgentMode string

const (
	AgentRaise AgentMode = "raise"
	AgentOff   AgentMode = "off"
)

// DevLower: dev hạ mức cần lý do hay không được hạ.
type DevLower string

const (
	DevLowerReason DevLower = "reason"
	DevLowerNever  DevLower = "never"
)

// ClassificationRules là khối classification của chính sách.
type ClassificationRules struct {
	Agent    AgentMode `yaml:"agent" json:"agent"`
	DevLower DevLower  `yaml:"devLower" json:"devLower"`
}

// DefaultClassification là giá trị mặc định (agent raise, devLower reason).
func DefaultClassification() ClassificationRules {
	return ClassificationRules{Agent: AgentRaise, DevLower: DevLowerReason}
}

// RiskRule là một rule phân loại. Note rỗng nghĩa là không có.
type RiskRule struct {
	ID      string    `yaml:"id" json:"id"`
	Kind    MatchKind `yaml:"kind" json:"kind"`
	Match   []string  `yaml:"match" json:"match"`
	Level   string    `yaml:"level" json:"level"`
	Note    string    `yaml:"note,omitempty" json:"note,omitempty"`
	Enabled bool      `yaml:"enabled" json:"enabled"`
}

// PresetRef ghi mẫu mà chính sách xuất phát.
type PresetRef struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version" json:"version"`
}

// RiskPolicy là chính sách rủi ro (risk-policy.yaml).
type RiskPolicy struct {
	Schema         int                          `yaml:"schema" json:"schema"`
	Version        int                          `yaml:"version" json:"version"`
	Preset         *PresetRef                   `yaml:"preset" json:"preset"`
	Levels         []string                     `yaml:"levels" json:"levels"`
	DefaultLevel   string                       `yaml:"default" json:"default"`
	Rules          []RiskRule                   `yaml:"rules" json:"rules"`
	Classification ClassificationRules          `yaml:"classification" json:"classification"`
	Requirements   map[string][]Requirement     `yaml:"requirements" json:"requirements"`
	Flow           map[string]map[string]string `yaml:"flow,omitempty" json:"flow,omitempty"`
}

// FlowUnits là các cổng có thể khai trong flow (Review luôn duyệt tay).
var FlowUnits = []string{"brd", "design", "cases", "step"}

// PolicyError gom mọi lỗi của một chính sách.
type PolicyError struct{ Errors []string }

func (e *PolicyError) Error() string {
	return "Chính sách rủi ro không hợp lệ: " + strings.Join(e.Errors, "; ")
}

func policyErr(msgs ...string) error { return &PolicyError{Errors: msgs} }

// ParsePolicy đọc chính sách từ YAML; lỗi có kiểu *PolicyError.
func ParsePolicy(y string) (RiskPolicy, error) {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(y), &n); err != nil {
		return RiskPolicy{}, policyErr("Không đọc được chính sách: " + strings.TrimPrefix(err.Error(), "yaml: "))
	}
	tree, err := yamlx.JSONValue(&n)
	if err != nil {
		return RiskPolicy{}, policyErr("Không đọc được chính sách: " + err.Error())
	}
	return build(tree)
}

// PolicyFromJSON đọc chính sách từ JSON (bản chụp trong cache, server).
func PolicyFromJSON(js string) (RiskPolicy, error) {
	dec := json.NewDecoder(strings.NewReader(js))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return RiskPolicy{}, policyErr("JSON chính sách không hợp lệ: " + err.Error())
	}
	if dec.More() {
		return RiskPolicy{}, policyErr("JSON chính sách không hợp lệ: còn dữ liệu sau giá trị JSON")
	}
	return build(normalizeJSON(tree))
}

func normalizeJSON(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := strconv.Atoi(string(x)); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	case []any:
		for i := range x {
			x[i] = normalizeJSON(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = normalizeJSON(x[k])
		}
	}
	return v
}

func build(tree any) (RiskPolicy, error) {
	root, ok := tree.(map[string]any)
	if !ok {
		return RiskPolicy{}, policyErr("Chính sách rỗng hoặc sai dạng")
	}
	var errs []string
	if checkShape(root, &errs) {
		return RiskPolicy{}, policyErr(errs...)
	}
	p, err := bind(root)
	if err != nil {
		errs = append(errs, "Không đọc được chính sách: "+err.Error())
		return RiskPolicy{}, policyErr(errs...)
	}
	p.validate(&errs)
	if len(errs) > 0 {
		return RiskPolicy{}, policyErr(errs...)
	}
	return p, nil
}

// asText như JsonNode.asText: giá trị đơn thành chuỗi, null là "null", mảng/bảng là "".
func asText(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return formatFloat(x)
	case []any, map[string]any:
		return ""
	}
	return fmt.Sprint(v)
}

func formatFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") && !math.IsInf(f, 0) && !math.IsNaN(f) {
		s += ".0"
	}
	return s
}

func isScalar(v any) bool {
	switch v.(type) {
	case []any, map[string]any:
		return false
	}
	return true
}

var (
	topKeys  = map[string]bool{"schema": true, "version": true, "preset": true, "levels": true, "default": true, "rules": true, "classification": true, "requirements": true, "flow": true}
	ruleKeys = map[string]bool{"id": true, "kind": true, "match": true, "level": true, "note": true, "enabled": true}
)

func unknownKeys(m map[string]any, known map[string]bool, where string, errs *[]string) {
	for _, k := range sortedKeys(m) {
		if !known[k] {
			*errs = append(*errs, where+": khóa lạ '"+k+"' (có thể gõ sai)")
		}
	}
}

func enumCheck(m map[string]any, field string, valid []string, where string, errs *[]string) bool {
	v, ok := m[field]
	if !ok || v == nil {
		return false
	}
	text := strings.TrimSpace(asText(v))
	for _, e := range valid {
		if strings.EqualFold(e, text) {
			return false
		}
	}
	*errs = append(*errs, where+" '"+asText(v)+"' không hợp lệ")
	return true
}

// checkShape kiểm cấu trúc; trả true khi có lỗi làm không thể bind (các lỗi khác chỉ gom thêm).
func checkShape(root map[string]any, errs *[]string) bool {
	fatal := false
	unknownKeys(root, topKeys, "chính sách", errs)
	if preset, ok := root["preset"].(map[string]any); ok {
		unknownKeys(preset, map[string]bool{"name": true, "version": true}, "preset", errs)
	}
	if cls, ok := root["classification"].(map[string]any); ok {
		unknownKeys(cls, map[string]bool{"agent": true, "devLower": true}, "classification", errs)
		fatal = enumCheck(cls, "agent", []string{"raise", "off"}, "classification.agent", errs) || fatal
		fatal = enumCheck(cls, "devLower", []string{"reason", "never"}, "classification.devLower", errs) || fatal
	}
	kinds := make([]string, len(matchKinds))
	for i, k := range matchKinds {
		kinds[i] = string(k)
	}
	if rules, ok := root["rules"].([]any); ok {
		for i, rv := range rules {
			where := "rules[" + strconv.Itoa(i) + "]"
			r, ok := rv.(map[string]any)
			if !ok {
				*errs = append(*errs, where+": entry rỗng hoặc không phải object")
				fatal = true
				continue
			}
			if id, has := r["id"]; has && id != nil {
				where = asText(id)
			}
			unknownKeys(r, ruleKeys, where, errs)
			fatal = enumCheck(r, "kind", kinds, where+": kind", errs) || fatal
		}
	}
	if flow, has := root["flow"]; has && flow != nil {
		fm, ok := flow.(map[string]any)
		if !ok {
			*errs = append(*errs, "flow: phải là bảng theo mức")
			fatal = true
		} else {
			for _, level := range sortedKeys(fm) {
				where := "flow." + level
				if fm[level] == nil {
					continue
				}
				units, ok := fm[level].(map[string]any)
				if !ok {
					*errs = append(*errs, where+": phải là bảng cổng → auto|manual")
					fatal = true
					continue
				}
				for _, u := range sortedKeys(units) {
					key := where + "." + u
					if u == "review" {
						*errs = append(*errs, key+": Review luôn duyệt tay, không có khóa review trong flow")
						continue
					}
					if !isFlowUnit(u) {
						*errs = append(*errs, key+": khóa lạ '"+u+"' (chỉ có brd, design, cases, step)")
						continue
					}
					s, isStr := units[u].(string)
					if !isStr || (s != "auto" && s != "manual") {
						*errs = append(*errs, key+" '"+asText(units[u])+"' không hợp lệ (auto hoặc manual)")
						if !isScalar(units[u]) {
							fatal = true
						}
					}
				}
			}
		}
	}
	if reqs, ok := root["requirements"].(map[string]any); ok {
		for _, level := range sortedKeys(reqs) {
			if reqs[level] == nil {
				continue
			}
			list, ok := reqs[level].([]any)
			if !ok {
				*errs = append(*errs, "requirements."+level+": phải là danh sách")
				fatal = true
				continue
			}
			for i, q := range list {
				if _, ok := q.(map[string]any); !ok {
					*errs = append(*errs, "requirements."+level+"["+strconv.Itoa(i)+"]: entry rỗng hoặc không phải object")
					fatal = true
				}
			}
		}
	}
	return fatal
}

func isFlowUnit(u string) bool {
	for _, f := range FlowUnits {
		if f == u {
			return true
		}
	}
	return false
}

// --- bind ---

func scalarString(v any, what string) (string, error) {
	if v == nil {
		return "", nil
	}
	if !isScalar(v) {
		return "", fmt.Errorf("%s phải là giá trị đơn", what)
	}
	return asText(v), nil
}

func toInt(v any, def int, what string) (int, error) {
	switch x := v.(type) {
	case nil:
		return def, nil
	case int:
		return x, nil
	case int64:
		return int(x), nil
	case float64:
		if x == math.Trunc(x) {
			return int(x), nil
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%s '%s' không phải số nguyên", what, asText(v))
}

func bind(root map[string]any) (RiskPolicy, error) {
	var p RiskPolicy
	var err error
	if p.Schema, err = toInt(root["schema"], 1, "schema"); err != nil {
		return p, err
	}
	if p.Version, err = toInt(root["version"], 0, "version"); err != nil { // không khai version: 0 (spec §4.1)
		return p, err
	}
	switch pr := root["preset"].(type) {
	case nil:
	case map[string]any:
		name, err := scalarString(pr["name"], "preset.name")
		if err != nil {
			return p, err
		}
		ver, err := scalarString(pr["version"], "preset.version")
		if err != nil {
			return p, err
		}
		p.Preset = &PresetRef{Name: name, Version: ver}
	default:
		return p, fmt.Errorf("preset phải là bảng")
	}
	p.Levels = []string{}
	switch l := root["levels"].(type) {
	case nil:
	case []any:
		for _, e := range l {
			if e == nil || !isScalar(e) {
				return p, fmt.Errorf("levels chứa phần tử không hợp lệ")
			}
			p.Levels = append(p.Levels, asText(e))
		}
	default:
		return p, fmt.Errorf("levels phải là danh sách")
	}
	if p.DefaultLevel, err = scalarString(root["default"], "default"); err != nil {
		return p, err
	}
	p.Rules = []RiskRule{}
	switch rs := root["rules"].(type) {
	case nil:
	case []any:
		for _, rv := range rs {
			r, err := bindRule(rv.(map[string]any))
			if err != nil {
				return p, err
			}
			p.Rules = append(p.Rules, r)
		}
	default:
		return p, fmt.Errorf("rules phải là danh sách")
	}
	p.Classification = DefaultClassification()
	switch c := root["classification"].(type) {
	case nil:
	case map[string]any:
		if a, ok := c["agent"]; ok && a != nil {
			p.Classification.Agent = AgentMode(strings.ToLower(strings.TrimSpace(asText(a))))
		}
		if d, ok := c["devLower"]; ok && d != nil {
			p.Classification.DevLower = DevLower(strings.ToLower(strings.TrimSpace(asText(d))))
		}
	default:
		return p, fmt.Errorf("classification phải là bảng")
	}
	p.Requirements = map[string][]Requirement{}
	switch rq := root["requirements"].(type) {
	case nil:
	case map[string]any:
		for level, lv := range rq {
			list := []Requirement{}
			if lv != nil {
				for _, q := range lv.([]any) {
					r, err := bindRequirement(q.(map[string]any))
					if err != nil {
						return p, err
					}
					list = append(list, r)
				}
			}
			p.Requirements[level] = list
		}
	default:
		return p, fmt.Errorf("requirements phải là bảng")
	}
	p.Flow = map[string]map[string]string{}
	if fm, ok := root["flow"].(map[string]any); ok {
		for level, uv := range fm {
			units, ok := uv.(map[string]any)
			if !ok || len(units) == 0 {
				continue
			}
			m := map[string]string{}
			for u, v := range units {
				s, err := scalarString(v, "flow."+level+"."+u)
				if err != nil {
					return p, err
				}
				m[u] = s
			}
			p.Flow[level] = m
		}
	}
	return p, nil
}

func bindRule(m map[string]any) (RiskRule, error) {
	r := RiskRule{Match: []string{}, Enabled: true}
	var err error
	if r.ID, err = scalarString(m["id"], "id"); err != nil {
		return r, err
	}
	if k := m["kind"]; k != nil {
		r.Kind = MatchKind(strings.ToLower(strings.TrimSpace(asText(k))))
	}
	switch mv := m["match"].(type) {
	case nil:
	case []any:
		for _, e := range mv {
			if e == nil {
				continue
			}
			if !isScalar(e) {
				return r, fmt.Errorf("match chứa phần tử không hợp lệ")
			}
			r.Match = append(r.Match, asText(e))
		}
	case map[string]any:
		return r, fmt.Errorf("match phải là chuỗi hoặc danh sách")
	default:
		r.Match = append(r.Match, asText(mv))
	}
	if r.Level, err = scalarString(m["level"], "level"); err != nil {
		return r, err
	}
	if r.Note, err = scalarString(m["note"], "note"); err != nil {
		return r, err
	}
	switch e := m["enabled"].(type) {
	case nil:
	case bool:
		r.Enabled = e
	case string:
		switch strings.ToLower(strings.TrimSpace(e)) {
		case "true":
		case "false":
			r.Enabled = false
		default:
			return r, fmt.Errorf("enabled '%s' không phải true/false", e)
		}
	default:
		return r, fmt.Errorf("enabled '%s' không phải true/false", asText(e))
	}
	return r, nil
}

func bindRequirement(m map[string]any) (Requirement, error) {
	params := map[string]any{}
	id := ""
	for k, v := range m {
		if k == "id" {
			s, err := scalarString(v, "requirement.id")
			if err != nil {
				return Requirement{}, err
			}
			id = s
			continue
		}
		params[k] = v
	}
	return Requirement{ID: id, Params: params}, nil
}

// --- validate ---

var readOnlySQL = regexp.MustCompile(`(?is)^\s*(select|with)\b.*$`)

func listText(l []string) string { return "[" + strings.Join(l, ", ") + "]" }

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func (p RiskPolicy) validate(errs *[]string) {
	add := func(s string) { *errs = append(*errs, s) }
	if p.Schema != 1 {
		add("schema " + strconv.Itoa(p.Schema) + " không được hỗ trợ (chỉ có 1)")
	}
	if len(p.Levels) < 2 || len(p.Levels) > 5 {
		add("Thang mức phải có 2 đến 5 mức, hiện có " + strconv.Itoa(len(p.Levels)))
	}
	uniq := map[string]bool{}
	for _, l := range p.Levels {
		uniq[l] = true
	}
	if len(uniq) != len(p.Levels) {
		add("Thang mức có tên trùng nhau")
	}
	if p.DefaultLevel == "" || !contains(p.Levels, p.DefaultLevel) {
		d := p.DefaultLevel
		if d == "" {
			d = "null"
		}
		add("Mức mặc định '" + d + "' không có trong thang " + listText(p.Levels))
	}
	seen := map[string]bool{}
	for i, r := range p.Rules {
		id := r.ID
		if strings.TrimSpace(id) == "" {
			id = "rule #" + strconv.Itoa(i+1)
			add(id + ": thiếu id")
		} else if seen[r.ID] {
			add(id + ": id bị trùng")
		} else {
			seen[r.ID] = true
		}
		if r.Kind == "" {
			add(id + ": thiếu kind")
		}
		if len(r.Match) == 0 {
			add(id + ": match không được rỗng")
		} else {
			for _, m := range r.Match {
				if strings.TrimSpace(m) == "" {
					add(id + ": match có phần tử rỗng")
					break
				}
			}
		}
		if r.Level == "" || !contains(p.Levels, r.Level) {
			l := r.Level
			if l == "" {
				l = "null"
			}
			add(id + ": mức '" + l + "' không có trong thang " + listText(p.Levels))
		}
		if r.Kind == MatchKeyword {
			for _, m := range r.Match {
				if strings.TrimSpace(m) == "" {
					continue
				}
				if _, err := regexp.Compile("(?i)" + m); err != nil {
					add(id + ": regex không hợp lệ '" + m + "': " + err.Error())
				}
			}
		}
	}
	for _, level := range sortedKeys(p.Requirements) {
		if !contains(p.Levels, level) {
			add("requirements: mức '" + level + "' không có trong thang " + listText(p.Levels))
		}
		for _, q := range p.Requirements[level] {
			validateRequirement(level, q, errs)
		}
	}
	for _, level := range sortedKeys(p.Flow) {
		if !contains(p.Levels, level) {
			add("flow: mức '" + level + "' không có trong thang " + listText(p.Levels))
		}
	}
}

func validateRequirement(level string, q Requirement, errs *[]string) {
	id := q.ID
	if id == "" {
		id = "null"
	}
	where := "requirements." + level + "." + id
	if !RequirementKnown(q.ID) {
		*errs = append(*errs, where+": yêu cầu '"+id+"' không có trong thư viện")
		return
	}
	for _, name := range RequirementRequiredParams(q.ID) {
		s, ok := q.StrParam(name)
		if !ok || strings.TrimSpace(s) == "" {
			*errs = append(*errs, where+": thiếu tham số '"+name+"'")
		}
	}
	if q.ID == ReqSQLZero {
		if sql, ok := q.StrParam("sql"); ok && strings.TrimSpace(sql) != "" {
			body := strings.TrimSpace(sql)
			body = strings.TrimSuffix(body, ";")
			if !readOnlySQL.MatchString(sql) || strings.Contains(body, ";") {
				*errs = append(*errs, where+": sql chỉ được là một câu SELECT hoặc WITH")
			}
		}
	}
}

// --- truy vấn ---

// Rank là vị trí của level trong thang, -1 nếu không có.
func (p RiskPolicy) Rank(level string) int {
	for i, l := range p.Levels {
		if l == level {
			return i
		}
	}
	return -1
}

// RequirementsFor trả yêu cầu của mức (rỗng nếu không có).
func (p RiskPolicy) RequirementsFor(level string) []Requirement { return p.Requirements[level] }

// FlowFor trả "auto" hoặc "manual"; unit thuộc brd|design|cases|step; thiếu thì "manual".
func (p RiskPolicy) FlowFor(level, unit string) string {
	if p.Flow[level][unit] == "auto" {
		return "auto"
	}
	return "manual"
}

// Hash là "sha256:" + băm của JSON chuẩn (khóa sắp theo bảng chữ cái, như Jackson).
func (p RiskPolicy) Hash() string {
	return "sha256:" + model.SHA256Contents([]model.Content{{Path: "risk-policy", Data: []byte(p.ToJSON())}})
}

// ToYAML ghi chính sách ra YAML.
func (p RiskPolicy) ToYAML() (string, error) {
	b, err := yamlx.Dump(p)
	return string(b), err
}

// ToJSON ghi JSON gọn, khóa sắp theo bảng chữ cái, giống ObjectMapper của Java (dùng để băm).
func (p RiskPolicy) ToJSON() string {
	var b bytes.Buffer
	cls := p.Classification
	b.WriteString(`{"classification":{"agent":`)
	writeJSONString(&b, string(cls.Agent))
	b.WriteString(`,"devLower":`)
	writeJSONString(&b, string(cls.DevLower))
	b.WriteString(`},"default":`)
	writeJSONString(&b, p.DefaultLevel)
	hasFlow := false
	for _, m := range p.Flow {
		if len(m) > 0 {
			hasFlow = true
		}
	}
	if hasFlow {
		b.WriteString(`,"flow":`)
		writeJSON(&b, flowForJSON(p.Flow))
	}
	b.WriteString(`,"levels":`)
	writeJSON(&b, stringsAny(p.Levels))
	b.WriteString(`,"preset":`)
	if p.Preset == nil {
		b.WriteString("null")
	} else {
		b.WriteString(`{"name":`)
		writeJSONString(&b, p.Preset.Name)
		b.WriteString(`,"version":`)
		writeJSONString(&b, p.Preset.Version)
		b.WriteString("}")
	}
	b.WriteString(`,"requirements":{`)
	for i, level := range sortedKeys(p.Requirements) {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSONString(&b, level)
		b.WriteString(":[")
		for j, q := range p.Requirements[level] {
			if j > 0 {
				b.WriteByte(',')
			}
			writeJSON(&b, q.flat())
		}
		b.WriteByte(']')
	}
	b.WriteString(`},"rules":[`)
	for i, r := range p.Rules {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"enabled":`)
		b.WriteString(strconv.FormatBool(r.Enabled))
		b.WriteString(`,"id":`)
		writeJSONString(&b, r.ID)
		b.WriteString(`,"kind":`)
		if r.Kind == "" {
			b.WriteString("null")
		} else {
			writeJSONString(&b, string(r.Kind))
		}
		b.WriteString(`,"level":`)
		writeJSONString(&b, r.Level)
		b.WriteString(`,"match":`)
		writeJSON(&b, stringsAny(r.Match))
		if r.Note != "" {
			b.WriteString(`,"note":`)
			writeJSONString(&b, r.Note)
		}
		b.WriteByte('}')
	}
	b.WriteString(`],"schema":`)
	b.WriteString(strconv.Itoa(p.Schema))
	b.WriteString(`,"version":`)
	b.WriteString(strconv.Itoa(p.Version))
	b.WriteByte('}')
	return b.String()
}

func stringsAny(l []string) []any {
	out := make([]any, len(l))
	for i, s := range l {
		out[i] = s
	}
	return out
}

func flowForJSON(f map[string]map[string]string) map[string]any {
	out := map[string]any{}
	for level, m := range f {
		if len(m) == 0 {
			continue
		}
		inner := map[string]any{}
		for u, v := range m {
			inner[u] = v
		}
		out[level] = inner
	}
	return out
}

// writeJSON ghi giá trị JSON gọn: khóa map sắp tăng dần; chuỗi thoát như Jackson (không thoát HTML, không thoát không-ASCII).
func writeJSON(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case string:
		writeJSONString(b, x)
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		b.WriteString(formatFloat(x))
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, e)
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, k)
			b.WriteByte(':')
			writeJSON(b, x[k])
		}
		b.WriteByte('}')
	default:
		writeJSONString(b, fmt.Sprint(x))
	}
}

func writeJSONString(b *bytes.Buffer, s string) {
	const hex = "0123456789ABCDEF"
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte(hex[r>>4])
			b.WriteByte(hex[r&0xf])
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
}
