package spec

import (
	"reflect"
	"testing"
)

func TestNormalizeRuleID(t *testing.T) {
	for in, want := range map[string]string{
		"br-1": "BR-1", " BR-01 ": "BR-1", "**BR-10**": "BR-10", "BR-000": "BR-0", "R-1": "R-1",
	} {
		if got := NormalizeRuleID(in); got != want {
			t.Errorf("NormalizeRuleID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCaseSpecNormalizesAndDedupesRules(t *testing.T) {
	c := parseCases(t, "cases:\n  - {id: C1, step: S1, title: t, rules: [br-01, BR-1, ' ', BR-2]}\n")
	if got := c.Cases[0].Rules; !reflect.DeepEqual(got, []string{"BR-1", "BR-2"}) {
		t.Errorf("rules = %v", got)
	}
}
