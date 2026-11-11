package risk

import (
	"strings"
	"testing"
)

const diffBase = `schema: 1
version: 3
levels: [low, medium, high]
default: low
rules:
  - { id: A1, kind: label, match: money-movement, level: high }
  - { id: A2, kind: label, match: schema-change, level: medium }
classification: { agent: raise, devLower: reason }
requirements:
  medium:
    - { id: rule-pos-neg }
    - { id: coverage, min: 75 }
  high:
    - { id: idempotent }
    - { id: concurrent, threads: 8 }
    - { id: coverage, min: 90 }
`

func onlyKind(t *testing.T, changes []PolicyChange, kind string) PolicyChange {
	t.Helper()
	var of []PolicyChange
	for _, c := range changes {
		if c.Kind == kind {
			of = append(of, c)
		}
	}
	if len(of) != 1 {
		t.Fatalf("%s: %+v", kind, changes)
	}
	return of[0]
}

func diffOf(t *testing.T, a, b string) []PolicyChange {
	t.Helper()
	return DiffPolicies(mustParse(t, a), mustParse(t, b))
}

func TestIdenticalPoliciesNoChanges(t *testing.T) {
	if d := diffOf(t, diffBase, diffBase); len(d) != 0 {
		t.Errorf("d = %+v", d)
	}
}

func TestLooseningRequirementRemoved(t *testing.T) {
	d := diffOf(t, diffBase, strings.Replace(diffBase, "    - { id: idempotent }\n", "", 1))
	c := onlyKind(t, d, "REMOVE_REQ")
	if c.Key != "high/idempotent" || !c.Loosening || len(d) != 1 {
		t.Errorf("d = %+v", d)
	}
}

func TestLooseningParamDecreased(t *testing.T) {
	d := diffOf(t, diffBase, strings.NewReplacer("threads: 8", "threads: 4", "min: 90", "min: 80").Replace(diffBase))
	if len(d) != 2 {
		t.Fatalf("d = %+v", d)
	}
	keys := map[string]PolicyChange{}
	for _, c := range d {
		if c.Kind != "CHANGE_PARAM" || !c.Loosening {
			t.Errorf("c = %+v", c)
		}
		keys[c.Key] = c
	}
	tc, ok := keys["high/concurrent.threads"]
	if !ok || keys["high/coverage.min"].Key == "" || tc.Before != "8" || tc.After != "4" {
		t.Errorf("keys = %+v", keys)
	}
	up := diffOf(t, diffBase, strings.Replace(diffBase, "threads: 8", "threads: 16", 1))
	if len(up) != 1 || up[0].Loosening {
		t.Errorf("up = %+v", up)
	}
}

func TestNotLooseningRuleAdded(t *testing.T) {
	d := diffOf(t, diffBase, strings.Replace(diffBase, "rules:\n", "rules:\n  - { id: A3, kind: path, match: \"**/x/**\", level: high }\n", 1))
	c := onlyKind(t, d, "ADD_RULE")
	if c.Key != "A3" || c.Before != "" || c.Loosening {
		t.Errorf("c = %+v", c)
	}
	d = diffOf(t, diffBase, strings.Replace(diffBase, "    - { id: idempotent }\n", "    - { id: idempotent }\n    - { id: invariant }\n", 1))
	if onlyKind(t, d, "ADD_REQ").Loosening {
		t.Error("ADD_REQ không phải nới")
	}
}

func TestLooseningRuleLevelLowered(t *testing.T) {
	d := diffOf(t, diffBase, strings.Replace(diffBase, "match: money-movement, level: high", "match: money-movement, level: medium", 1))
	if c := onlyKind(t, d, "CHANGE_RULE"); c.Key != "A1" || !c.Loosening {
		t.Errorf("c = %+v", c)
	}
	up := diffOf(t, diffBase, strings.Replace(diffBase, "match: schema-change, level: medium", "match: schema-change, level: high", 1))
	if onlyKind(t, up, "CHANGE_RULE").Loosening {
		t.Error("nâng mức không phải nới")
	}
	off := diffOf(t, diffBase, strings.Replace(diffBase, "match: schema-change, level: medium }", "match: schema-change, level: medium, enabled: false }", 1))
	if !onlyKind(t, off, "CHANGE_RULE").Loosening {
		t.Error("tắt rule là nới")
	}
	rm := diffOf(t, diffBase, strings.Replace(diffBase, "  - { id: A2, kind: label, match: schema-change, level: medium }\n", "", 1))
	if !onlyKind(t, rm, "REMOVE_RULE").Loosening {
		t.Error("xóa rule là nới")
	}
}

func TestClassificationLoosening(t *testing.T) {
	d := diffOf(t, diffBase, strings.Replace(diffBase, "agent: raise, devLower: reason", "agent: \"off\", devLower: reason", 1))
	if !onlyKind(t, d, "CHANGE_CLASSIFICATION").Loosening {
		t.Error("agent off là nới")
	}
	never := strings.Replace(diffBase, "devLower: reason", "devLower: never", 1)
	if !onlyKind(t, diffOf(t, never, diffBase), "CHANGE_CLASSIFICATION").Loosening {
		t.Error("never -> reason là nới")
	}
	if onlyKind(t, diffOf(t, diffBase, never), "CHANGE_CLASSIFICATION").Loosening {
		t.Error("reason -> never không phải nới")
	}
}

func TestDiffManualToAutoIsLoosening(t *testing.T) {
	manual := diffBase + "flow:\n  low: { brd: manual }\n"
	auto := diffBase + "flow:\n  low: { brd: auto }\n"
	c := onlyKind(t, diffOf(t, manual, auto), "CHANGE_FLOW")
	if c.Key != "low/brd" || !c.Loosening {
		t.Errorf("c = %+v", c)
	}
	if !onlyKind(t, diffOf(t, diffBase, auto), "CHANGE_FLOW").Loosening {
		t.Error("thiếu flow -> auto là nới")
	}
	if onlyKind(t, diffOf(t, auto, manual), "CHANGE_FLOW").Loosening {
		t.Error("auto -> manual không phải nới")
	}
}

func TestDiffSchemaMigrated(t *testing.T) {
	from, err := LoadPreset("generic", "2")
	if err != nil {
		t.Fatal(err)
	}
	to, err := ParsePolicy(schema2Base)
	if err != nil {
		t.Fatal(err)
	}
	ch := DiffPolicies(from, to)
	if len(ch) == 0 || ch[0].Kind != "SCHEMA_MIGRATED" || ch[0].Before != "1" || ch[0].After != "2" {
		t.Errorf("changes = %+v", ch)
	}
}

func TestDiffDescribeChangeLoosens(t *testing.T) {
	a, _ := ParsePolicy(schema2Base)
	b, _ := ParsePolicy(strings.Replace(schema2Base, "Chạm sổ cái", "Chạm sổ cái và số dư", 1))
	c := onlyKind(t, DiffPolicies(a, b), "CHANGE_RULE")
	if !c.Loosening || !strings.Contains(c.After, "số dư") {
		t.Errorf("c = %+v", c)
	}
}
