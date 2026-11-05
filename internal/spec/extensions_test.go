package spec

import (
	"reflect"
	"testing"

	"aisdlc/internal/yamlx"
)

func TestOldYamlStillParses(t *testing.T) {
	cases, err := ReadCases(tmplFile(t, "cases.full.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDesign(tmplFile(t, "design.yaml")); err != nil {
		t.Fatal(err)
	}
	if len(cases.Cases) == 0 {
		t.Fatal("không có case")
	}
	for _, c := range cases.Cases {
		if c.When.Repeat != 1 || c.When.Parallel != 1 || len(c.Then.StatusCounts) != 0 {
			t.Errorf("%s: %+v %+v", c.ID, c.When, c.Then)
		}
	}
}

func TestRepeatParallelParse(t *testing.T) {
	s := parseCases(t, `
cases:
  - id: C1
    step: S1
    kind: error
    title: a
    when: {method: POST, path: /x, repeat: 2}
    then: {status: 200}
  - id: C2
    step: S1
    kind: error
    title: b
    when: {method: POST, path: /x, parallel: 8}
    then: {statusCounts: {200: 1, 409: 7}}
`)
	c0, c1 := s.Cases[0], s.Cases[1]
	if c0.When.Repeat != 2 || c0.When.Parallel != 1 || c1.When.Parallel != 8 || c1.When.Repeat != 1 {
		t.Errorf("repeat/parallel sai: %+v %+v", c0.When, c1.When)
	}
	if c1.Then.Status != 0 {
		t.Errorf("status = %d", c1.Then.Status)
	}
	if !reflect.DeepEqual(c1.Then.StatusCounts, map[int]int{200: 1, 409: 7}) {
		t.Errorf("statusCounts = %v", c1.Then.StatusCounts)
	}
}

func TestDesignInvariantsImpact(t *testing.T) {
	var d DesignSpec
	err := yamlx.Parse([]byte(`
steps: []
invariants:
  - {name: balanced, sql: "SELECT 1 WHERE 1=0"}
impact:
  level: high
  labels: [money-movement]
  reason: chuyển tiền
`), &d)
	if err != nil {
		t.Fatal(err)
	}
	if d.Invariants[0].Name != "balanced" || !reflect.DeepEqual(d.Impact.Labels, []string{"money-movement"}) {
		t.Errorf("d = %+v", d)
	}
	tmpl, err := ReadDesign(tmplFile(t, "design.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpl.Invariants) != 0 {
		t.Errorf("invariants = %v", tmpl.Invariants)
	}
}

func TestInvariantStepParsedAndAppliesFromThatStepOn(t *testing.T) {
	var d DesignSpec
	err := yamlx.Parse([]byte(`
steps: [{id: S1}, {id: S2}, {id: S3}]
invariants:
  - {name: a, sql: "SELECT 1 WHERE 1=0"}
  - {name: b, sql: "SELECT 1 WHERE 1=0", step: S2}
`), &d)
	if err != nil {
		t.Fatal(err)
	}
	if d.Invariants[0].Step != "" || d.Invariants[1].Step != "S2" {
		t.Errorf("step: %+v", d.Invariants)
	}
	order := d.StepOrder()
	if !reflect.DeepEqual(order, []string{"S1", "S2", "S3"}) {
		t.Fatalf("order = %v", order)
	}
	b := d.Invariants[1]
	checks := []struct {
		name string
		got  bool
		want bool
	}{
		{"b S1", b.AppliesTo("S1", order), false},
		{"b S2", b.AppliesTo("S2", order), true},
		{"b S3", b.AppliesTo("S3", order), true},
		{"a S1", d.Invariants[0].AppliesTo("S1", order), true},
		{"b lite", b.AppliesTo("S1", nil), true},
		{"b SX", b.AppliesTo("SX", order), true},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}
