package spec

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

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
	return fmt.Errorf("dòng %d: kind không hợp lệ: %q (chỉ nhận happy, error, boundary)", n.Line, s)
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
	type plain When
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
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

// MessageExpect là kỳ vọng message.
type MessageExpect struct {
	Topic string `yaml:"topic"`
	Value any    `yaml:"value"`
}

// Then là kỳ vọng của case. Status = 0 nghĩa là không khai báo.
type Then struct {
	Status       int             `yaml:"status"`
	Body         any             `yaml:"body"`
	DB           []DbExpect      `yaml:"db"`
	Messages     []MessageExpect `yaml:"messages"`
	StatusCounts map[int]int     `yaml:"statusCounts"`
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
