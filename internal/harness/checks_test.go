package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"aisdlc/internal/model"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
)

func invCase(step string) spec.CaseSpec {
	c := mk(spec.Given{}, wh("GET", "/noop", 1, 1), spec.Then{Status: 200})
	c.Step = step
	return c
}

func TestInvariantViolationFailsCase(t *testing.T) {
	r := newRig(t)
	q := "SELECT id FROM t WHERE amount < 0"
	r.db.violations = map[string][]map[string]any{q: {{"id": 2}}}
	r.ex.Checks = []spec.Invariant{{Name: "so-tien-khong-am", SQL: q}}
	got := r.ex.Execute(context.Background(), invCase("S1"))
	if got.Outcome != run.CaseOutcomeFailed || got.Message != "Bất biến 'so-tien-khong-am' vi phạm: [{id=2}]" {
		t.Errorf("%+v", got)
	}
}

func TestInvariantPassesWhenZeroRows(t *testing.T) {
	r := newRig(t)
	r.ex.Checks = []spec.Invariant{{Name: "n", SQL: "q"}}
	if got := r.ex.Execute(context.Background(), invCase("S1")); got.Outcome != run.CaseOutcomePassed || len(r.db.viol) != 1 {
		t.Errorf("%+v", got)
	}
}

func TestInvariantRowsFormatSortedKeys(t *testing.T) {
	r := newRig(t)
	r.db.violations = map[string][]map[string]any{"q": {{"b": 1, "a": "x"}, {"a": "y"}}}
	r.ex.Checks = []spec.Invariant{{Name: "n", SQL: "q"}}
	got := r.ex.Execute(context.Background(), invCase("S1"))
	if got.Message != "Bất biến 'n' vi phạm: [{a=x, b=1}, {a=y}]" {
		t.Errorf("%q", got.Message)
	}
}

func TestInvariantQueryErrorFailsNotSwallowed(t *testing.T) {
	r := newRig(t)
	r.db.violErr = map[string]error{"q": errors.New(`relation "chua_tao" does not exist`)}
	r.ex.Checks = []spec.Invariant{{Name: "bang-sau", SQL: "q"}}
	got := r.ex.Execute(context.Background(), invCase("S1"))
	if got.Outcome != run.CaseOutcomeFailed || got.Message != `Bất biến 'bang-sau' lỗi: relation "chua_tao" does not exist` {
		t.Errorf("%+v", got)
	}
}

func TestInvariantStepOnlyFromThatStepOn(t *testing.T) {
	r := newRig(t)
	r.db.violErr = map[string]error{"q": errors.New("lỗi")}
	r.ex.Checks = []spec.Invariant{{Name: "bang-sau", SQL: "q", Step: "S2"}}
	r.ex.StepOrder = []string{"S1", "S2"}
	if got := r.ex.Execute(context.Background(), invCase("S1")); got.Outcome != run.CaseOutcomePassed {
		t.Errorf("S1: %+v", got)
	}
	if got := r.ex.Execute(context.Background(), invCase("S2")); got.Outcome != run.CaseOutcomeFailed ||
		!strings.HasPrefix(got.Message, "Bất biến 'bang-sau' lỗi: ") {
		t.Errorf("S2: %+v", got)
	}
}

func TestInvariantsSkippedWhenThenFails(t *testing.T) {
	r := newRig(t)
	r.ex.Checks = []spec.Invariant{{Name: "n", SQL: "q"}}
	c := invCase("S1")
	c.Then.Status = 500
	if got := r.ex.Execute(context.Background(), c); got.Outcome != run.CaseOutcomeFailed || len(r.db.viol) != 0 {
		t.Errorf("%+v", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func feat(t *testing.T) model.FeaturePaths {
	return model.FeaturePaths{RepoRoot: t.TempDir(), Key: "DEMO-1"}
}

func TestChecksSQLZeroBoundToLastDesignStep(t *testing.T) {
	p := feat(t)
	writeFile(t, p.Risk(), `level: high
requirements:
  - { id: sql-zero, name: z, sql: "SELECT 1 WHERE 1=0" }
`)
	writeFile(t, p.DesignYaml(), `steps: [{id: S1}, {id: S2}]
invariants:
  - { name: a, sql: "SELECT 1 WHERE 1=0" }
  - { name: b, sql: "SELECT 1 WHERE 1=0", step: S1 }
`)
	checks, order, err := Checks(p)
	if err != nil {
		t.Fatal(err)
	}
	var names, steps []string
	for _, c := range checks {
		names, steps = append(names, c.Name), append(steps, c.Step)
	}
	if !reflect.DeepEqual(names, []string{"a", "b", "z"}) || !reflect.DeepEqual(steps, []string{"", "S1", "S2"}) ||
		!reflect.DeepEqual(order, []string{"S1", "S2"}) {
		t.Errorf("names=%v steps=%v order=%v", names, steps, order)
	}
}

func TestChecksSQLZeroDefaultsNameAndAppliesToAllWithoutSteps(t *testing.T) {
	p := feat(t)
	writeFile(t, p.Risk(), "level: high\nrequirements:\n  - { id: sql-zero, sql: \"SELECT 1\" }\n  - { id: coverage, min: 90 }\n")
	checks, order, err := Checks(p)
	if err != nil || len(order) != 0 || len(checks) != 1 || checks[0].Name != "sql-zero" || checks[0].Step != "" || checks[0].SQL != "SELECT 1" {
		t.Errorf("%+v %v %v", checks, order, err)
	}
}

func TestChecksLiteCasesYamlInvariantsDropStep(t *testing.T) {
	p := feat(t)
	writeFile(t, p.Cases(), `cases: []
invariants:
  - { name: b1-100, sql: "SELECT id FROM transfer", step: S9 }
`)
	checks, order, err := Checks(p)
	if err != nil || len(order) != 0 || !reflect.DeepEqual(checks, []spec.Invariant{{Name: "b1-100", SQL: "SELECT id FROM transfer"}}) {
		t.Errorf("%+v %v %v", checks, order, err)
	}
}

func TestChecksNoFilesIsEmpty(t *testing.T) {
	checks, order, err := Checks(feat(t))
	if err != nil || len(checks) != 0 || len(order) != 0 {
		t.Errorf("%+v %v %v", checks, order, err)
	}
}

func TestChecksBrokenFileIsError(t *testing.T) {
	p := feat(t)
	writeFile(t, p.DesignYaml(), "steps: [unclosed\n")
	if _, _, err := Checks(p); err == nil {
		t.Error("expected error")
	}
	q := feat(t)
	writeFile(t, q.Risk(), "requirements: [unclosed\n")
	if _, _, err := Checks(q); err == nil {
		t.Error("expected error")
	}
}

func TestLaterStepChecksSkipEarlierCases(t *testing.T) {
	p := feat(t)
	writeFile(t, p.Risk(), "level: high\nrequirements:\n  - { id: sql-zero, name: z, sql: \"SELECT id FROM chua_tao\" }\n")
	writeFile(t, p.DesignYaml(), "steps: [{id: S1}, {id: S2}]\ninvariants:\n  - { name: sau, sql: \"SELECT id FROM chua_tao\", step: S2 }\n")
	checks, order, err := Checks(p)
	if err != nil {
		t.Fatal(err)
	}
	r := newRig(t)
	r.db.violErr = map[string]error{"SELECT id FROM chua_tao": errors.New("không có bảng")}
	r.ex.Checks, r.ex.StepOrder = checks, order
	if got := r.ex.Execute(context.Background(), invCase("S1")); got.Outcome != run.CaseOutcomePassed {
		t.Errorf("S1 must skip later-step checks: %+v", got)
	}
	if got := r.ex.Execute(context.Background(), invCase("S2")); got.Outcome != run.CaseOutcomeFailed {
		t.Errorf("S2: %+v", got)
	}
}

func TestInvariantViolationPrintsNullForNil(t *testing.T) {
	r := newRig(t)
	q := "SELECT id, note FROM t"
	r.db.violations = map[string][]map[string]any{q: {{"id": 2, "note": nil}}}
	r.ex.Checks = []spec.Invariant{{Name: "n", SQL: q}}
	got := r.ex.Execute(context.Background(), invCase("S1"))
	if got.Message != "Bất biến 'n' vi phạm: [{id=2, note=null}]" {
		t.Errorf("%+v", got)
	}
}
