package spec

import (
	"reflect"
	"strings"
	"testing"

	"aisdlc/internal/yamlx"
)

func dstep(id string, scope []string, b *Budget) StepSpec {
	return StepSpec{ID: id, Title: "t", Scope: scope, Budget: b}
}

func dcodes(md string, d DesignSpec) []string { return codesOf(ValidateDesign(md, d)) }

func TestDesignTemplatesAreValid(t *testing.T) {
	if f := ValidateDesign(tmplText(t, "design.md"), spec2(t)); len(f) != 0 {
		t.Fatalf("%v", f)
	}
}

func TestDesignNoSequence(t *testing.T) {
	v := spec2(t)
	if !contains(dcodes("# x\n```mermaid\nflowchart LR\n A-->B\n```\n", v), "DESIGN_NO_SEQUENCE") {
		t.Fatal("thiếu DESIGN_NO_SEQUENCE")
	}
	if got := dcodes("# x", v); !reflect.DeepEqual(got, []string{"DESIGN_NO_SEQUENCE"}) {
		t.Fatalf("%v", got)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestDesignStepNotInDiagram(t *testing.T) {
	md := strings.ReplaceAll(tmplText(t, "design.md"), "S2", "S9")
	md = strings.ReplaceAll(md, "S9 tính", "tính")
	if !contains(dcodes(md, spec2(t)), "DESIGN_STEP_NOT_IN_DIAGRAM") {
		t.Fatal("thiếu")
	}
}

func TestDesignUnknownStepInDiagram(t *testing.T) {
	md := strings.Replace(tmplText(t, "design.md"), "Note over A,D: S2", "Note over A,D: S2 và S7", 1)
	if got := dcodes(md, spec2(t)); !reflect.DeepEqual(got, []string{"DESIGN_UNKNOWN_STEP_IN_DIAGRAM"}) {
		t.Fatalf("%v", got)
	}
}

func TestDesignStepWithoutScope(t *testing.T) {
	v := spec2(t)
	d := DesignSpec{Steps: []StepSpec{dstep("S1", nil, &Budget{1, 1}), v.Steps[1]}}
	if got := dcodes(tmplText(t, "design.md"), d); !reflect.DeepEqual(got, []string{"DESIGN_STEP_NO_SCOPE"}) {
		t.Fatalf("%v", got)
	}
}

func TestDesignStepWithoutOrInvalidBudget(t *testing.T) {
	v := spec2(t)
	for _, b := range []*Budget{nil, {0, 10}, {3, 0}} {
		d := DesignSpec{Steps: []StepSpec{dstep("S1", []string{"a/**"}, b), v.Steps[1]}}
		if got := dcodes(tmplText(t, "design.md"), d); !reflect.DeepEqual(got, []string{"DESIGN_STEP_NO_BUDGET"}) {
			t.Fatalf("%v: %v", b, got)
		}
	}
}

func TestDesignDuplicateStep(t *testing.T) {
	v := spec2(t)
	d := DesignSpec{Steps: []StepSpec{v.Steps[0], v.Steps[0], v.Steps[1]}}
	if got := dcodes(tmplText(t, "design.md"), d); !reflect.DeepEqual(got, []string{"DESIGN_DUP_STEP"}) {
		t.Fatalf("%v", got)
	}
}

func TestDesignOpenQuestion(t *testing.T) {
	md := tmplText(t, "design.md")
	f := ValidateDesign(md+"\n- [ ] Có cần idempotency key không?\n", spec2(t))
	if len(f) != 1 || f[0].Code != "DESIGN_OPEN_QUESTION" || !strings.Contains(f[0].Message, "idempotency") {
		t.Fatalf("%v", f)
	}
	if got := dcodes(md+"\n- [x] Có cần idempotency key không? → có\n", spec2(t)); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestDesignInvariantStepMustExist(t *testing.T) {
	var d DesignSpec
	err := yamlx.Parse([]byte(`steps: [{id: S1, scope: [a/**], budget: {files: 1, lines: 1}}]
invariants:
  - {name: b, sql: "SELECT 1 WHERE 1=0", step: S9}
`), &d)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range ValidateDesign(tmplText(t, "design.md"), d) {
		if f.Code == "DESIGN_INVARIANT_STEP" && strings.Contains(f.Message, "S9") {
			found = true
		}
	}
	if !found {
		t.Fatal("thiếu DESIGN_INVARIANT_STEP")
	}
}

func TestDesignImpactNoLongerValidated(t *testing.T) {
	md := tmplText(t, "design.md")
	for _, d := range []DesignSpec{{}, {Impact: &Impact{Level: "urgent", Reason: "r"}}} {
		g := codesOf(ValidateDesign(md, d))
		if contains(g, "IMPACT_MISSING") || contains(g, "IMPACT_LEVEL") {
			t.Fatalf("%v", g)
		}
	}
}

func TestImpactDeprecationWarning(t *testing.T) {
	if got := ImpactDeprecationWarning(nil); got != "" {
		t.Fatalf("không có impact thì không cảnh báo: %q", got)
	}
	want := "IMPACT_DEPRECATED: Khối impact không còn dùng: subagent aisdlc-risk-classifier xếp loại rủi ro; xóa khối này."
	if got := ImpactDeprecationWarning(&Impact{Level: "low"}); got != want {
		t.Fatalf("%q", got)
	}
}
