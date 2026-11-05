package risk

import (
	"reflect"
	"testing"
)

func in(text string, tables, endpoints []string, agent string) RiskInputs {
	return RiskInputs{Text: text, Tables: tables, Endpoints: endpoints, AgentLevel: agent}
}

func policyWith(t *testing.T, rules, classification string) RiskPolicy {
	t.Helper()
	return mustParse(t, "schema: 1\nlevels: [low, medium, high]\ndefault: low\nrules:\n"+rules+"\nclassification: "+classification+"\nrequirements: {}\n")
}

func TestKeywordMatchesUnicodeCaseInsensitive(t *testing.T) {
	r := Classify(latest(t, "banking"), in("Ghi nhận BÚT TOÁN", nil, nil, ""))
	if r.Level != "high" {
		t.Errorf("level = %s", r.Level)
	}
	found := false
	for _, m := range r.Matched {
		found = found || (m.Rule == "R1" && m.On == "“BÚT TOÁN”")
	}
	if !found {
		t.Errorf("matched = %+v", r.Matched)
	}
}

func TestTableGlob(t *testing.T) {
	r := Classify(latest(t, "banking"), in("", []string{"ledger_entry"}, nil, ""))
	found := false
	for _, m := range r.Matched {
		found = found || m.On == "bảng ledger_entry"
	}
	if r.Level != "high" || !found {
		t.Errorf("%+v", r)
	}
}

func TestTableGlobStarDoesNotCrossDot(t *testing.T) {
	p := policyWith(t, `  - { id: T1, kind: table, match: "ledger_*", level: high }`, "{ agent: raise }")
	if m := Classify(p, in("", []string{"ledger_entry.amount"}, nil, "")).Matched; len(m) != 0 {
		t.Errorf("matched = %v", m)
	}
	if l := Classify(p, in("", []string{"ledger_entry", "ledger_entry.amount"}, nil, "")).Level; l != "high" {
		t.Errorf("level = %s", l)
	}
	q := policyWith(t, `  - { id: T1, kind: table, match: "ledger*", level: high }`, "{ agent: raise }")
	if m := Classify(q, in("", []string{"ledger.x"}, nil, "")).Matched; len(m) != 0 {
		t.Errorf("matched = %v", m)
	}
	c := policyWith(t, `  - { id: T1, kind: table, match: "*.*", level: high }`, "{ agent: raise }")
	if m := Classify(c, in("", []string{"a.b"}, nil, "")).Matched; len(m) != 1 || m[0].On != "bảng a.b" {
		t.Errorf("matched = %v", m)
	}
}

func TestEndpointGlobWithPathVariable(t *testing.T) {
	p := policyWith(t, `  - { id: E1, kind: endpoint, match: "POST /payments/**", level: high }`, "{ agent: raise }")
	hit := Classify(p, in("", nil, []string{"POST /payments/{id}/confirm"}, ""))
	if hit.Level != "high" || hit.Matched[0].On != "POST /payments/{id}/confirm" {
		t.Errorf("hit = %+v", hit)
	}
	miss := Classify(p, in("", nil, []string{"GET /payments/1"}, ""))
	if miss.Level != "low" || len(miss.Matched) != 0 {
		t.Errorf("miss = %+v", miss)
	}
}

func TestPathRuleUsesScopes(t *testing.T) {
	i := RiskInputs{Paths: []string{"src/main/java/**/accounting/*.java"}}
	r := Classify(latest(t, "banking"), i)
	found := false
	for _, m := range r.Matched {
		found = found || (m.Rule == "R3" && m.On == "src/main/java/**/accounting/*.java")
	}
	if r.Level != "high" || !found {
		t.Errorf("%+v", r)
	}
}

func TestLabelRuleExact(t *testing.T) {
	r := Classify(latest(t, "banking"), RiskInputs{Labels: []string{"money-movement"}})
	if r.Level != "high" || r.Matched[0].On != "agent gắn nhãn money-movement" {
		t.Errorf("%+v", r)
	}
}

func TestFirstMatchOnlyPerRule(t *testing.T) {
	p := policyWith(t, `  - { id: T1, kind: table, match: "a_*", level: medium }`, "{ agent: raise }")
	if m := Classify(p, in("", []string{"a_1", "a_2"}, nil, "")).Matched; len(m) != 1 {
		t.Errorf("matched = %v", m)
	}
}

func TestDisabledRuleIgnored(t *testing.T) {
	p := policyWith(t, `  - { id: K1, kind: keyword, match: "abc", level: high, enabled: false }`, "{ agent: raise }")
	r := Classify(p, in("abc", nil, nil, ""))
	if r.Level != "low" || len(r.Matched) != 0 {
		t.Errorf("%+v", r)
	}
}

func TestAgentRaises(t *testing.T) {
	r := Classify(latest(t, "banking"), in("không liên quan", nil, nil, "high"))
	if r.Level != "high" || r.RuleLevel != "low" || r.AgentLevel != "high" {
		t.Errorf("%+v", r)
	}
}

func TestAgentCannotLower(t *testing.T) {
	if r := Classify(latest(t, "banking"), in("bút toán", nil, nil, "low")); r.Level != "high" {
		t.Errorf("%+v", r)
	}
}

func TestAgentOff(t *testing.T) {
	p := policyWith(t, `  - { id: K1, kind: keyword, match: "abc", level: medium }`, `{ agent: "off" }`)
	if r := Classify(p, in("", nil, nil, "high")); r.Level != "low" {
		t.Errorf("%+v", r)
	}
}

func TestUnknownAgentLevelIgnored(t *testing.T) {
	if r := Classify(latest(t, "banking"), in("", nil, nil, "critical")); r.Level != "low" {
		t.Errorf("%+v", r)
	}
}

func TestClassify_matchedNeverNilShape(t *testing.T) {
	r := Classify(latest(t, "banking"), in("", nil, nil, ""))
	if r.Matched == nil || !reflect.DeepEqual(r.Matched, []Match{}) {
		t.Errorf("matched = %#v", r.Matched)
	}
}
