package review

import (
	"testing"

	"aisdlc/internal/spec"
	"aisdlc/internal/yamlx"
)

var defaultValueFields = []string{"amount", "limit", "balance", "fee", "rate", "max", "min"}

func casesOf(t *testing.T, yaml string) spec.CasesSpec {
	t.Helper()
	var c spec.CasesSpec
	if err := yamlx.Parse([]byte(yaml), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestValueNotInBrd(t *testing.T) {
	c := casesOf(t, `
cases:
  - id: C3
    step: S1
    kind: happy
    title: t
    when: { method: POST, path: /x, body: { amount: 20000000, limit: 12 } }
    then: { status: 200, body: { remaining: 30000000 } }
  - id: C4
    step: S1
    kind: happy
    title: t
    when: { method: POST, path: /x, body: { amount: 50000000, fee: 5 } }
    then: { status: 200 }
`)
	fs := ValuesNotInBrd(c, "Hạn mức 50.000.000 VND/ngày", defaultValueFields)
	if len(fs) != 1 {
		t.Fatalf("flags %v", fs)
	}
	eq(t, "code", fs[0].Code, "CASE_VALUE_NOT_IN_BRD")
	eq(t, "level", fs[0].Level, WARN)
	eq(t, "refs", fs[0].Refs, []string{"C3"})
}

func TestRuleCoverage(t *testing.T) {
	c := casesOf(t, `
cases:
  - { id: C1, step: S1, kind: happy, title: t, rules: [BR-1], then: { status: 200 } }
`)
	brd := ParseBRD("## Quy tắc nghiệp vụ\n- BR-1: a\n- BR-2: b\n- không id\n")
	fs := append(RuleIDs(brd), RuleCoverage(brd, c)...)
	eq(t, "codes", codes(fs), []string{"BRD_RULE_NO_ID", "CASES_RULE_UNCOVERED"})
	eq(t, "refs", find(fs, "CASES_RULE_UNCOVERED").Refs, []string{"BR-2"})
}

func TestUnknownRuleReference(t *testing.T) {
	c := casesOf(t, `
cases:
  - { id: C1, step: S1, kind: happy, title: t, rules: [BR-1, BR-9], then: { status: 200 } }
`)
	brd := ParseBRD("## Quy tắc nghiệp vụ\n- BR-1: a\n")
	fs := RuleCoverage(brd, c)
	eq(t, "codes", codes(fs), []string{"CASES_RULE_UNKNOWN"})
	eq(t, "refs", fs[0].Refs, []string{"BR-9"})
}

func TestCaseValueRefsInNaturalOrder(t *testing.T) {
	c := casesOf(t, `
cases:
  - { id: C10, step: S1, kind: happy, title: t, when: { method: POST, path: /x, body: { amount: 77777 } }, then: { status: 200 } }
  - { id: C2, step: S1, kind: happy, title: t, when: { method: POST, path: /x, body: { amount: 88888 } }, then: { status: 200 } }
`)
	fs := ValuesNotInBrd(c, "không có số", defaultValueFields)
	eq(t, "refs", fs[0].Refs, []string{"C2", "C10"})
}

func TestPlainDigitsLikeBigDecimal(t *testing.T) {
	for in, want := range map[string]string{"20000000": "20000000", "1.50": "15", "1e7": "10000000", "0.050": "005", "1.0E7": "10000000"} {
		if got := plainDigits(in); got != want {
			t.Errorf("plainDigits(%q) = %q, want %q", in, got, want)
		}
	}
}
