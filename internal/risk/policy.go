package risk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"aisdlc/internal/i18n"
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
	Agent    AgentMode `yaml:"agent,omitempty" json:"agent,omitempty"` // chỉ schema 1
	DevLower DevLower  `yaml:"devLower" json:"devLower"`
}

// DefaultClassification là giá trị mặc định (agent raise, devLower reason).
func DefaultClassification() ClassificationRules {
	return ClassificationRules{Agent: AgentRaise, DevLower: DevLowerReason}
}

// RiskRule là một rule phân loại. Schema 1 dùng Kind/Match/Note (luật so khớp); schema 2 chỉ dùng Describe
// (mô tả bằng lời cho LLM). Note rỗng nghĩa là không có.
type RiskRule struct {
	ID       string    `yaml:"id" json:"id"`
	Kind     MatchKind `yaml:"kind,omitempty" json:"kind,omitempty"`
	Match    []string  `yaml:"match,omitempty" json:"match,omitempty"`
	Describe string    `yaml:"describe,omitempty" json:"describe,omitempty"`
	Level    string    `yaml:"level" json:"level"`
	Note     string    `yaml:"note,omitempty" json:"note,omitempty"`
	Enabled  bool      `yaml:"enabled" json:"enabled"`
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
	// Warnings: cảnh báo khi đọc (không làm hỏng chính sách), không ghi ra YAML/JSON.
	Warnings []string `yaml:"-" json:"-"`
}

// FlowUnits là các cổng có thể khai trong flow (Review luôn duyệt tay).
var FlowUnits = []string{"brd", "design", "cases", "step"}

// PolicyError gom mọi lỗi của một chính sách; Errors đã theo locale lúc đọc. loc rỗng (dựng ngoài gói) là vi.
type PolicyError struct {
	Errors []string
	loc    i18n.Locale
}

func (e *PolicyError) Error() string {
	return e.loc.Tf("Chính sách rủi ro không hợp lệ: %s", strings.Join(e.Errors, "; "))
}

func policyErr(loc i18n.Locale, msgs ...string) error { return &PolicyError{Errors: msgs, loc: loc} }

// Usable báo chính sách có dùng được để phân loại không: chỉ schema 2 (luật mô tả bằng lời) dùng được.
func (p RiskPolicy) Usable() error { return p.UsableIn(i18n.Default()) }

// UsableIn như Usable, thông điệp theo loc (web server dùng locale của request).
func (p RiskPolicy) UsableIn(loc i18n.Locale) error {
	if p.Schema == 1 {
		return policyErr(loc, loc.T("Chính sách dùng schema 1 (luật so khớp); chạy `aisdlc policy migrate` để chuyển sang schema 2."))
	}
	return nil
}

// ParsePolicy đọc chính sách từ YAML; lỗi có kiểu *PolicyError, theo locale mặc định của tiến trình.
func ParsePolicy(y string) (RiskPolicy, error) { return ParsePolicyIn(i18n.Default(), y) }

// ParsePolicyIn như ParsePolicy, lỗi và cảnh báo theo loc.
func ParsePolicyIn(loc i18n.Locale, y string) (RiskPolicy, error) {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(y), &n); err != nil {
		return RiskPolicy{}, policyErr(loc, loc.Tf("Không đọc được chính sách: %s", strings.TrimPrefix(err.Error(), "yaml: ")))
	}
	tree, err := yamlx.JSONValue(&n)
	if err != nil {
		return RiskPolicy{}, policyErr(loc, loc.Tf("Không đọc được chính sách: %s", err.Error()))
	}
	return build(loc, tree)
}

// PolicyFromJSON đọc chính sách từ JSON (bản chụp trong cache, server).
func PolicyFromJSON(js string) (RiskPolicy, error) { return PolicyFromJSONIn(i18n.Default(), js) }

// PolicyFromJSONIn như PolicyFromJSON, lỗi và cảnh báo theo loc.
func PolicyFromJSONIn(loc i18n.Locale, js string) (RiskPolicy, error) {
	dec := json.NewDecoder(strings.NewReader(js))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return RiskPolicy{}, policyErr(loc, loc.Tf("JSON chính sách không hợp lệ: %s", err.Error()))
	}
	if dec.More() {
		return RiskPolicy{}, policyErr(loc, loc.T("JSON chính sách không hợp lệ: còn dữ liệu sau giá trị JSON"))
	}
	return build(loc, normalizeJSON(tree))
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

func build(loc i18n.Locale, tree any) (RiskPolicy, error) {
	root, ok := tree.(map[string]any)
	if !ok {
		return RiskPolicy{}, policyErr(loc, loc.T("Chính sách rỗng hoặc sai dạng"))
	}
	var errs []string
	if checkShape(loc, root, &errs) {
		return RiskPolicy{}, policyErr(loc, errs...)
	}
	p, err := bind(loc, root)
	if err != nil {
		errs = append(errs, loc.Tf("Không đọc được chính sách: %s", err.Error()))
		return RiskPolicy{}, policyErr(loc, errs...)
	}
	p.validate(loc, &errs)
	if len(errs) > 0 {
		return RiskPolicy{}, policyErr(loc, errs...)
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
	topKeys   = map[string]bool{"schema": true, "version": true, "preset": true, "levels": true, "default": true, "rules": true, "classification": true, "requirements": true, "flow": true}
	ruleKeys  = map[string]bool{"id": true, "kind": true, "match": true, "level": true, "note": true, "enabled": true}
	ruleKeys2 = map[string]bool{"id": true, "level": true, "describe": true, "enabled": true}
)

func unknownKeys(loc i18n.Locale, m map[string]any, known map[string]bool, where string, errs *[]string) {
	for _, k := range sortedKeys(m) {
		if !known[k] {
			*errs = append(*errs, loc.Tf("%s: khóa lạ '%s' (có thể gõ sai)", where, k))
		}
	}
}

func enumCheck(loc i18n.Locale, m map[string]any, field string, valid []string, where string, errs *[]string) bool {
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
	*errs = append(*errs, loc.Tf("%s '%s' không hợp lệ", where, asText(v)))
	return true
}

// checkShape kiểm cấu trúc; trả true khi có lỗi làm không thể bind (các lỗi khác chỉ gom thêm).
func checkShape(loc i18n.Locale, root map[string]any, errs *[]string) bool {
	fatal := false
	schema2 := rawSchema(root) == 2
	unknownKeys(loc, root, topKeys, loc.T("chính sách"), errs)
	if preset, ok := root["preset"].(map[string]any); ok {
		unknownKeys(loc, preset, map[string]bool{"name": true, "version": true}, "preset", errs)
	}
	if cls, ok := root["classification"].(map[string]any); ok {
		if schema2 {
			unknownKeys(loc, cls, map[string]bool{"devLower": true}, "classification", errs)
		} else {
			unknownKeys(loc, cls, map[string]bool{"agent": true, "devLower": true}, "classification", errs)
			fatal = enumCheck(loc, cls, "agent", []string{"raise", "off"}, "classification.agent", errs) || fatal
		}
		fatal = enumCheck(loc, cls, "devLower", []string{"reason", "never"}, "classification.devLower", errs) || fatal
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
				*errs = append(*errs, loc.Tf("%s: entry rỗng hoặc không phải object", where))
				fatal = true
				continue
			}
			if id, has := r["id"]; has && id != nil {
				where = asText(id)
			}
			if schema2 {
				unknownKeys(loc, r, ruleKeys2, where, errs)
			} else {
				unknownKeys(loc, r, ruleKeys, where, errs)
				fatal = enumCheck(loc, r, "kind", kinds, where+": kind", errs) || fatal
			}
		}
	}
	if flow, has := root["flow"]; has && flow != nil {
		fm, ok := flow.(map[string]any)
		if !ok {
			*errs = append(*errs, loc.T("flow: phải là bảng theo mức"))
			fatal = true
		} else {
			for _, level := range sortedKeys(fm) {
				where := "flow." + level
				if fm[level] == nil {
					continue
				}
				units, ok := fm[level].(map[string]any)
				if !ok {
					*errs = append(*errs, loc.Tf("%s: phải là bảng cổng → auto|manual", where))
					fatal = true
					continue
				}
				for _, u := range sortedKeys(units) {
					key := where + "." + u
					if u == "review" {
						*errs = append(*errs, loc.Tf("%s: Review luôn duyệt tay, không có khóa review trong flow", key))
						continue
					}
					if !isFlowUnit(u) {
						*errs = append(*errs, loc.Tf("%s: khóa lạ '%s' (chỉ có brd, design, cases, step)", key, u))
						continue
					}
					s, isStr := units[u].(string)
					if !isStr || (s != "auto" && s != "manual") {
						*errs = append(*errs, loc.Tf("%s '%s' không hợp lệ (auto hoặc manual)", key, asText(units[u])))
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
				*errs = append(*errs, loc.Tf("requirements.%s: phải là danh sách", level))
				fatal = true
				continue
			}
			for i, q := range list {
				if _, ok := q.(map[string]any); !ok {
					*errs = append(*errs, loc.Tf("requirements.%s[%d]: entry rỗng hoặc không phải object", level, i))
					fatal = true
				}
			}
		}
	}
	return fatal
}

// rawSchema đọc khóa schema trước khi bind; thiếu hoặc sai dạng thì coi là 1.
func rawSchema(root map[string]any) int {
	n, err := toInt(i18n.VI, root["schema"], 1, "schema")
	if err != nil {
		return 1
	}
	return n
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

func scalarString(loc i18n.Locale, v any, what string) (string, error) {
	if v == nil {
		return "", nil
	}
	if !isScalar(v) {
		return "", fmt.Errorf(loc.T("%s phải là giá trị đơn"), what)
	}
	return asText(v), nil
}

func toInt(loc i18n.Locale, v any, def int, what string) (int, error) {
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
	return 0, fmt.Errorf(loc.T("%s '%s' không phải số nguyên"), what, asText(v))
}

func bind(loc i18n.Locale, root map[string]any) (RiskPolicy, error) {
	var p RiskPolicy
	var err error
	if p.Schema, err = toInt(loc, root["schema"], 1, "schema"); err != nil {
		return p, err
	}
	if p.Version, err = toInt(loc, root["version"], 0, "version"); err != nil { // không khai version: 0 (spec §4.1)
		return p, err
	}
	switch pr := root["preset"].(type) {
	case nil:
	case map[string]any:
		name, err := scalarString(loc, pr["name"], "preset.name")
		if err != nil {
			return p, err
		}
		ver, err := scalarString(loc, pr["version"], "preset.version")
		if err != nil {
			return p, err
		}
		p.Preset = &PresetRef{Name: name, Version: ver}
	default:
		return p, errors.New(loc.T("preset phải là bảng"))
	}
	p.Levels = []string{}
	switch l := root["levels"].(type) {
	case nil:
	case []any:
		for _, e := range l {
			if e == nil || !isScalar(e) {
				return p, errors.New(loc.T("levels chứa phần tử không hợp lệ"))
			}
			p.Levels = append(p.Levels, asText(e))
		}
	default:
		return p, errors.New(loc.T("levels phải là danh sách"))
	}
	if p.DefaultLevel, err = scalarString(loc, root["default"], "default"); err != nil {
		return p, err
	}
	p.Rules = []RiskRule{}
	switch rs := root["rules"].(type) {
	case nil:
	case []any:
		for _, rv := range rs {
			r, err := bindRule(loc, rv.(map[string]any), p.Schema)
			if err != nil {
				return p, err
			}
			p.Rules = append(p.Rules, r)
		}
	default:
		return p, errors.New(loc.T("rules phải là danh sách"))
	}
	p.Classification = DefaultClassification()
	if p.Schema == 2 {
		p.Classification.Agent = "" // schema 2 không có agent nâng mức
	}
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
		return p, errors.New(loc.T("classification phải là bảng"))
	}
	p.Requirements = map[string][]Requirement{}
	switch rq := root["requirements"].(type) {
	case nil:
	case map[string]any:
		for level, lv := range rq {
			list := []Requirement{}
			if lv != nil {
				for _, q := range lv.([]any) {
					r, err := bindRequirement(loc, q.(map[string]any))
					if err != nil {
						return p, err
					}
					list = append(list, r)
				}
			}
			p.Requirements[level] = list
		}
	default:
		return p, errors.New(loc.T("requirements phải là bảng"))
	}
	p.Flow = map[string]map[string]string{}
	if fm, ok := root["flow"].(map[string]any); ok {
		for _, level := range sortedKeys(fm) {
			units, ok := fm[level].(map[string]any)
			if !ok || len(units) == 0 {
				continue
			}
			m := map[string]string{}
			for _, u := range sortedKeys(units) {
				v := units[u]
				if p.Schema == 2 && u == "brd" {
					p.Warnings = append(p.Warnings, loc.Tf("flow.%s.brd bị bỏ qua: cổng BRD luôn duyệt tay", level))
					continue
				}
				s, err := scalarString(loc, v, "flow."+level+"."+u)
				if err != nil {
					return p, err
				}
				m[u] = s
			}
			if len(m) > 0 {
				p.Flow[level] = m
			}
		}
	}
	return p, nil
}

func bindRule(loc i18n.Locale, m map[string]any, schema int) (RiskRule, error) {
	r := RiskRule{Enabled: true}
	if schema != 2 {
		r.Match = []string{}
	}
	var err error
	if r.ID, err = scalarString(loc, m["id"], "id"); err != nil {
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
				return r, errors.New(loc.T("match chứa phần tử không hợp lệ"))
			}
			r.Match = append(r.Match, asText(e))
		}
	case map[string]any:
		return r, errors.New(loc.T("match phải là chuỗi hoặc danh sách"))
	default:
		r.Match = append(r.Match, asText(mv))
	}
	if r.Level, err = scalarString(loc, m["level"], "level"); err != nil {
		return r, err
	}
	if r.Note, err = scalarString(loc, m["note"], "note"); err != nil {
		return r, err
	}
	if r.Describe, err = scalarString(loc, m["describe"], "describe"); err != nil {
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
			return r, fmt.Errorf(loc.T("enabled '%s' không phải true/false"), e)
		}
	default:
		return r, fmt.Errorf(loc.T("enabled '%s' không phải true/false"), asText(e))
	}
	return r, nil
}

func bindRequirement(loc i18n.Locale, m map[string]any) (Requirement, error) {
	params := map[string]any{}
	id := ""
	for k, v := range m {
		if k == "id" {
			s, err := scalarString(loc, v, "requirement.id")
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

func (p RiskPolicy) validate(loc i18n.Locale, errs *[]string) {
	add := func(s string) { *errs = append(*errs, s) }
	if p.Schema != 1 && p.Schema != 2 {
		add(loc.Tf("schema %d không được hỗ trợ (chỉ có 1 và 2)", p.Schema))
	}
	if len(p.Levels) < 2 || len(p.Levels) > 5 {
		add(loc.Tf("Thang mức phải có 2 đến 5 mức, hiện có %d", len(p.Levels)))
	}
	uniq := map[string]bool{}
	for _, l := range p.Levels {
		uniq[l] = true
	}
	if len(uniq) != len(p.Levels) {
		add(loc.T("Thang mức có tên trùng nhau"))
	}
	if p.DefaultLevel == "" || !contains(p.Levels, p.DefaultLevel) {
		d := p.DefaultLevel
		if d == "" {
			d = "null"
		}
		add(loc.Tf("Mức mặc định '%s' không có trong thang %s", d, listText(p.Levels)))
	}
	seen := map[string]bool{}
	for i, r := range p.Rules {
		id := r.ID
		if strings.TrimSpace(id) == "" {
			id = "rule #" + strconv.Itoa(i+1)
			add(loc.Tf("%s: thiếu id", id))
		} else if seen[r.ID] {
			add(loc.Tf("%s: id bị trùng", id))
		} else {
			seen[r.ID] = true
		}
		if p.Schema == 2 {
			if strings.TrimSpace(r.Describe) == "" {
				add(loc.Tf("%s: describe không được rỗng", id))
			}
		} else {
			if r.Kind == "" {
				add(loc.Tf("%s: thiếu kind", id))
			}
			if len(r.Match) == 0 {
				add(loc.Tf("%s: match không được rỗng", id))
			} else {
				for _, m := range r.Match {
					if strings.TrimSpace(m) == "" {
						add(loc.Tf("%s: match có phần tử rỗng", id))
						break
					}
				}
			}
		}
		if r.Level == "" || !contains(p.Levels, r.Level) {
			l := r.Level
			if l == "" {
				l = "null"
			}
			add(loc.Tf("%s: mức '%s' không có trong thang %s", id, l, listText(p.Levels)))
		}
		if r.Kind == MatchKeyword {
			for _, m := range r.Match {
				if strings.TrimSpace(m) == "" {
					continue
				}
				if _, err := regexp.Compile("(?i)" + m); err != nil {
					add(loc.Tf("%s: regex không hợp lệ '%s': %s", id, m, err.Error()))
				}
			}
		}
	}
	for _, level := range sortedKeys(p.Requirements) {
		if !contains(p.Levels, level) {
			add(loc.Tf("requirements: mức '%s' không có trong thang %s", level, listText(p.Levels)))
		}
		for _, q := range p.Requirements[level] {
			validateRequirement(loc, level, q, errs)
		}
	}
	for _, level := range sortedKeys(p.Flow) {
		if !contains(p.Levels, level) {
			add(loc.Tf("flow: mức '%s' không có trong thang %s", level, listText(p.Levels)))
		}
	}
}

func validateRequirement(loc i18n.Locale, level string, q Requirement, errs *[]string) {
	id := q.ID
	if id == "" {
		id = "null"
	}
	where := "requirements." + level + "." + id
	if !RequirementKnown(q.ID) {
		*errs = append(*errs, loc.Tf("%s: yêu cầu '%s' không có trong thư viện", where, id))
		return
	}
	for _, name := range RequirementRequiredParams(q.ID) {
		s, ok := q.StrParam(name)
		if !ok || strings.TrimSpace(s) == "" {
			*errs = append(*errs, loc.Tf("%s: thiếu tham số '%s'", where, name))
		}
	}
	if q.ID == ReqSQLZero {
		if sql, ok := q.StrParam("sql"); ok && strings.TrimSpace(sql) != "" {
			body := strings.TrimSpace(sql)
			body = strings.TrimSuffix(body, ";")
			if !readOnlySQL.MatchString(sql) || strings.Contains(body, ";") {
				*errs = append(*errs, loc.Tf("%s: sql chỉ được là một câu SELECT hoặc WITH", where))
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
	b.WriteString(`{"classification":{`)
	if p.Schema != 2 {
		b.WriteString(`"agent":`)
		writeJSONString(&b, string(cls.Agent))
		b.WriteByte(',')
	}
	b.WriteString(`"devLower":`)
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
		if p.Schema == 2 {
			b.WriteString(`{"describe":`)
			writeJSONString(&b, r.Describe)
			b.WriteString(`,"enabled":`)
			b.WriteString(strconv.FormatBool(r.Enabled))
			b.WriteString(`,"id":`)
			writeJSONString(&b, r.ID)
			b.WriteString(`,"level":`)
			writeJSONString(&b, r.Level)
			b.WriteByte('}')
			continue
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
