package spec

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"aisdlc/internal/i18n"
	"aisdlc/internal/yamlx"
)

// CaseKind là loại case: happy, error, boundary.
type CaseKind string

const (
	KindHappy    CaseKind = "happy"
	KindError    CaseKind = "error"
	KindBoundary CaseKind = "boundary"
)

// UnmarshalYAML trim, không phân biệt hoa thường; giá trị lạ là lỗi.
func (k *CaseKind) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v := CaseKind(strings.ToLower(strings.TrimSpace(s)))
	switch v {
	case KindHappy, KindError, KindBoundary:
		*k = v
		return nil
	}
	return fmt.Errorf(i18n.T("dòng %d: kind không hợp lệ: %q (chỉ nhận happy, error, boundary)"), n.Line, s)
}

// Given là dữ liệu dựng trước case.
type Given struct {
	SQL   []string `yaml:"sql"`
	Stubs []string `yaml:"stubs"`
}

// When là request của case.
type When struct {
	Method   string            `yaml:"method"`
	Path     string            `yaml:"path"`
	Headers  map[string]string `yaml:"headers"`
	Body     any               `yaml:"body"`
	Repeat   int               `yaml:"repeat"`
	Parallel int               `yaml:"parallel"`
}

// UnmarshalYAML đặt Repeat và Parallel nhỏ hơn hoặc bằng 0 thành 1.
func (w *When) UnmarshalYAML(n *yaml.Node) error {
	var raw struct {
		Method   string            `yaml:"method"`
		Path     string            `yaml:"path"`
		Headers  map[string]string `yaml:"headers"`
		Body     yaml.Node         `yaml:"body"`
		Repeat   int               `yaml:"repeat"`
		Parallel int               `yaml:"parallel"`
	}
	if err := n.Decode(&raw); err != nil {
		return err
	}
	body, err := yamlx.JSONValue(&raw.Body)
	if err != nil {
		return err
	}
	p := When{raw.Method, raw.Path, raw.Headers, body, raw.Repeat, raw.Parallel}
	if p.Repeat <= 0 {
		p.Repeat = 1
	}
	if p.Parallel <= 0 {
		p.Parallel = 1
	}
	*w = When(p)
	return nil
}

// DbExpect là kỳ vọng DB sau case.
type DbExpect struct {
	Query string           `yaml:"query"`
	Rows  []map[string]any `yaml:"rows"`
}

// UnmarshalYAML giữ rows ở dạng JSON-compatible (ngày giữ nguyên chuỗi).
func (d *DbExpect) UnmarshalYAML(n *yaml.Node) error {
	var raw struct {
		Query string    `yaml:"query"`
		Rows  yaml.Node `yaml:"rows"`
	}
	if err := n.Decode(&raw); err != nil {
		return err
	}
	rows, err := jsonRows(&raw.Rows)
	if err != nil {
		return err
	}
	*d = DbExpect{Query: raw.Query, Rows: rows}
	return nil
}

func jsonRows(n *yaml.Node) ([]map[string]any, error) {
	v, err := yamlx.JSONValue(n)
	if err != nil || v == nil {
		return nil, err
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf(i18n.T("dòng %d: rows phải là danh sách"), n.Line)
	}
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf(i18n.T("dòng %d: mỗi phần tử của rows phải là mapping"), n.Line)
		}
		out = append(out, m)
	}
	return out, nil
}

func jsonMap(n *yaml.Node) (map[string]any, error) {
	v, err := yamlx.JSONValue(n)
	if err != nil || v == nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(i18n.T("dòng %d: giá trị phải là mapping"), n.Line)
	}
	return m, nil
}

// MessageExpect là kỳ vọng message.
type MessageExpect struct {
	Topic string `yaml:"topic"`
	Value any    `yaml:"value"`
}

// UnmarshalYAML giữ value ở dạng JSON-compatible.
func (m *MessageExpect) UnmarshalYAML(n *yaml.Node) error {
	var raw struct {
		Topic string    `yaml:"topic"`
		Value yaml.Node `yaml:"value"`
	}
	if err := n.Decode(&raw); err != nil {
		return err
	}
	v, err := yamlx.JSONValue(&raw.Value)
	if err != nil {
		return err
	}
	*m = MessageExpect{Topic: raw.Topic, Value: v}
	return nil
}

// Then là kỳ vọng của case. Status = 0 nghĩa là không khai báo.
type Then struct {
	Status       int             `yaml:"status"`
	Body         any             `yaml:"body"`
	DB           []DbExpect      `yaml:"db"`
	Messages     []MessageExpect `yaml:"messages"`
	StatusCounts map[int]int     `yaml:"statusCounts"`
}

// UnmarshalYAML giữ body ở dạng JSON-compatible.
func (t *Then) UnmarshalYAML(n *yaml.Node) error {
	var raw struct {
		Status       int             `yaml:"status"`
		Body         yaml.Node       `yaml:"body"`
		DB           []DbExpect      `yaml:"db"`
		Messages     []MessageExpect `yaml:"messages"`
		StatusCounts map[int]int     `yaml:"statusCounts"`
	}
	if err := n.Decode(&raw); err != nil {
		return err
	}
	body, err := yamlx.JSONValue(&raw.Body)
	if err != nil {
		return err
	}
	*t = Then{raw.Status, body, raw.DB, raw.Messages, raw.StatusCounts}
	return nil
}

// CaseSpec là một case.
type CaseSpec struct {
	ID    string   `yaml:"id"`
	Step  string   `yaml:"step"`
	Kind  CaseKind `yaml:"kind"`
	Title string   `yaml:"title"`
	Given Given    `yaml:"given"`
	When  *When    `yaml:"when"`
	Then  *Then    `yaml:"then"`
	Rules []string `yaml:"rules"`
}

// UnmarshalYAML chuẩn hóa Rules.
func (c *CaseSpec) UnmarshalYAML(n *yaml.Node) error {
	type plain CaseSpec
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	p.Rules = normalizeRules(p.Rules)
	*c = CaseSpec(p)
	return nil
}

// StubSpec theo định dạng JSON mapping của WireMock.
type StubSpec struct {
	ID       string         `yaml:"id"`
	Request  map[string]any `yaml:"request"`
	Response map[string]any `yaml:"response"`
}

// UnmarshalYAML giữ request/response ở dạng JSON-compatible.
func (s *StubSpec) UnmarshalYAML(n *yaml.Node) error {
	var raw struct {
		ID       string    `yaml:"id"`
		Request  yaml.Node `yaml:"request"`
		Response yaml.Node `yaml:"response"`
	}
	if err := n.Decode(&raw); err != nil {
		return err
	}
	req, err := jsonMap(&raw.Request)
	if err != nil {
		return err
	}
	resp, err := jsonMap(&raw.Response)
	if err != nil {
		return err
	}
	*s = StubSpec{ID: raw.ID, Request: req, Response: resp}
	return nil
}

// CasesSpec là nội dung cases.yaml. Scope, Budget, Impact, Invariants chỉ dùng cho luồng LITE.
type CasesSpec struct {
	ResetSQL   []string    `yaml:"reset_sql"`
	Stubs      []StubSpec  `yaml:"stubs"`
	Cases      []CaseSpec  `yaml:"cases"`
	Scope      []string    `yaml:"scope"`
	Budget     *Budget     `yaml:"budget"`
	Impact     *Impact     `yaml:"impact"`
	Invariants []Invariant `yaml:"invariants"`
}

// ReadCases đọc cases.yaml.
func ReadCases(path string) (CasesSpec, error) {
	var c CasesSpec
	if err := yamlx.Read(path, &c); err != nil {
		return CasesSpec{}, err
	}
	return c, nil
}
