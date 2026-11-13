package rewind

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
)

func mkInput(t *testing.T, flow model.Flow, nSteps int, reached bool) Input {
	t.Helper()
	p := model.FeaturePaths{RepoRoot: t.TempDir(), Key: "F-1"}
	for _, f := range []string{"feature.yaml", "brd.md", "design.md", "design.yaml", "schema-expected.json", "cases.yaml",
		"risk.yaml", "risk-agent.json", "review-agent.json", "scope-extra.json", "gates.json", "steps.json", "feedback.md",
		"db/V1.sql", "eval/r.json"} {
		fp := filepath.Join(p.Dir(), f)
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fp, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	in := Input{Key: p.Key, Flow: flow, Paths: p, RiskFromDesign: flow == model.FlowFull,
		Reached: func(gate.Gate) bool { return reached }}
	for i := 1; i <= nSteps; i++ {
		id := "S" + string(rune('0'+i))
		in.Steps = append(in.Steps, spec.StepSpec{ID: id})
		in.Records = append(in.Records, step.StepRecord{ID: id, StartTree: "tree-" + id})
	}
	return in
}

func ids(cps []Checkpoint) string {
	var s []string
	for _, c := range cps {
		s = append(s, c.ID)
	}
	return strings.Join(s, ",")
}

func TestCheckpoints_FullFlow(t *testing.T) {
	in := mkInput(t, model.FlowFull, 3, true)
	if got := ids(Checkpoints(in)); got != "brd,design,cases,S1,S2,S3,review" {
		t.Fatal(got)
	}
	in.Records = in.Records[:2]
	if got := ids(Checkpoints(in)); got != "brd,design,cases,S1,S2,review" {
		t.Fatal(got)
	}
}

func TestCheckpoints_LiteFlow(t *testing.T) {
	in := mkInput(t, model.FlowLite, 1, true)
	if got := ids(Checkpoints(in)); got != "cases,S1,review" {
		t.Fatal(got)
	}
}

func rel(in Input, l []string) []string {
	out := []string{}
	for _, s := range l {
		out = append(out, in.Paths.Rel(s)[len(in.Paths.Rel(in.Paths.Dir()))+1:])
	}
	return out
}

func TestBuild_Table(t *testing.T) {
	all := []string{"brd.md", "design.md", "design.yaml", "db", "schema-expected.json", "cases.yaml", "risk.yaml",
		"risk-agent.json", "eval", "review-agent.json", "scope-extra.json"}
	tests := []struct {
		name   string
		flow   model.Flow
		cp     string
		clear  []gate.Gate
		drop   []string
		del    []string
		tree   string
		scopeS []string
	}{
		{"brd", model.FlowFull, "brd", []gate.Gate{gate.BRD, gate.DESIGN, gate.CASES, gate.REVIEW}, []string{"S1", "S2"}, all, "tree-S1", nil},
		{"design", model.FlowFull, "design", []gate.Gate{gate.DESIGN, gate.CASES, gate.REVIEW}, []string{"S1", "S2"}, all[1:], "tree-S1", nil},
		{"cases full", model.FlowFull, "cases", []gate.Gate{gate.CASES, gate.REVIEW}, []string{"S1", "S2"},
			[]string{"cases.yaml", "eval", "review-agent.json", "scope-extra.json"}, "tree-S1", nil},
		{"cases lite", model.FlowLite, "cases", []gate.Gate{gate.CASES, gate.REVIEW}, []string{"S1", "S2"},
			[]string{"cases.yaml", "risk.yaml", "risk-agent.json", "schema-expected.json", "eval", "review-agent.json", "scope-extra.json"}, "tree-S1", nil},
		{"S2", model.FlowFull, "S2", []gate.Gate{gate.REVIEW}, []string{"S2"},
			[]string{"eval", "review-agent.json"}, "tree-S2", []string{"S2"}},
		{"review", model.FlowFull, "review", []gate.Gate{gate.REVIEW}, nil,
			[]string{"eval", "review-agent.json"}, "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := mkInput(t, tc.flow, 2, true)
			cp, err := Parse(in, tc.cp)
			if err != nil {
				t.Fatal(err)
			}
			pl, err := Build(in, cp)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(pl.ClearGates, tc.clear) || !reflect.DeepEqual(pl.DropSteps, tc.drop) ||
				!reflect.DeepEqual(rel(in, pl.DeletePaths), tc.del) || pl.CodeTree != tc.tree ||
				!reflect.DeepEqual(pl.ScopeSteps, tc.scopeS) {
				t.Fatalf("%+v\ndel=%v", pl, rel(in, pl.DeletePaths))
			}
		})
	}
}

func TestBuild_NoStepsNoCodeChange(t *testing.T) {
	in := mkInput(t, model.FlowFull, 0, true)
	pl, _ := Build(in, Checkpoint{ID: "design", Gate: gate.DESIGN})
	if pl.CodeTree != "" {
		t.Fatal(pl.CodeTree)
	}
}

func TestParse_CaseInsensitiveAndUnknown(t *testing.T) {
	in := mkInput(t, model.FlowFull, 2, true)
	for _, a := range []string{"S2", "s2", "Design"} {
		if _, err := Parse(in, a); err != nil {
			t.Fatal(a, err)
		}
	}
	_, err := Parse(in, "S9")
	if err == nil || !strings.Contains(err.Error(), "brd, design, cases, S1, S2, review") {
		t.Fatal(err)
	}
}
