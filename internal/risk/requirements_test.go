package risk

import (
	"strings"
	"testing"

	"aisdlc/internal/review"
	"aisdlc/internal/spec"
	"aisdlc/internal/yamlx"
)

func riskOf(rs ...Requirement) RiskFile {
	return RiskFile{Level: "high", Policy: PolicyRef{Version: 1, Hash: "h"}, Requirements: rs}
}

func req(id string, kv ...any) Requirement {
	m := map[string]any{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return NewRequirement(id, m)
}

func kase(id string, kind spec.CaseKind, when *spec.When, rules ...string) spec.CaseSpec {
	return spec.CaseSpec{ID: id, Step: "S1", Kind: kind, Title: "t", When: when, Rules: rules}
}

func wh(method, path string, repeat, parallel int) *spec.When {
	return &spec.When{Method: method, Path: path, Repeat: repeat, Parallel: parallel}
}

func casesOf(cs ...spec.CaseSpec) spec.CasesSpec { return spec.CasesSpec{Cases: cs} }

func brdOf(rules ...review.Rule) *review.BrdDoc { return &review.BrdDoc{Rules: rules} }

func apiOf(lines ...string) *review.DesignDoc { return &review.DesignDoc{APILines: lines} }

func check(t *testing.T, r RiskFile, c spec.CasesSpec, d *spec.DesignSpec, b *review.BrdDoc, dd *review.DesignDoc) []RequirementStatus {
	t.Helper()
	return CheckRequirements(r, c, d, b, dd)
}

func one(t *testing.T, l []RequirementStatus) RequirementStatus {
	t.Helper()
	if len(l) != 1 {
		t.Fatalf("len = %d: %+v", len(l), l)
	}
	return l[0]
}

func TestRulePosNegOk(t *testing.T) {
	r := check(t, riskOf(req("rule-pos-neg")),
		casesOf(kase("C1", spec.KindHappy, nil, "BR-1", "BR-2"), kase("C2", spec.KindError, nil, "BR-1"), kase("C3", spec.KindBoundary, nil, "BR-2")),
		nil, brdOf(review.Rule{ID: "BR-1", Text: "a"}, review.Rule{ID: "BR-2", Text: "b"}), nil)
	s := one(t, r)
	if s.Status != RequirementOK || s.ID != "rule-pos-neg" || s.Name != "Case đúng và case sai cho mỗi rule BRD" {
		t.Errorf("s = %+v", s)
	}
}

func TestRulePosNegMissingNegativeForBR2(t *testing.T) {
	s := one(t, check(t, riskOf(req("rule-pos-neg")),
		casesOf(kase("C1", spec.KindHappy, nil, "BR-1", "BR-2"), kase("C2", spec.KindError, nil, "BR-1")),
		nil, brdOf(review.Rule{ID: "BR-1", Text: "a"}, review.Rule{ID: "BR-2", Text: "b"}), nil))
	if s.Status != RequirementMissing || !strings.Contains(s.Detail, "BR-2") || strings.Contains(s.Detail, "BR-1") {
		t.Errorf("s = %+v", s)
	}
	if s.Detail != "thiếu case sai hoặc biên cho BR-2" {
		t.Errorf("detail = %s", s.Detail)
	}
}

func TestRulePosNegLiteIsLater(t *testing.T) {
	s := one(t, check(t, riskOf(req("rule-pos-neg")), casesOf(), nil, nil, nil))
	if s.Status != RequirementLater || !strings.Contains(s.Detail, "không có BRD") {
		t.Errorf("s = %+v", s)
	}
}

func TestBoundaryOkAndMissing(t *testing.T) {
	b := brdOf(review.Rule{ID: "BR-1", Text: "a", Boundary: true}, review.Rule{ID: "BR-2", Text: "b"})
	ok := one(t, check(t, riskOf(req("boundary")), casesOf(kase("C1", spec.KindBoundary, nil, "BR-1")), nil, b, nil))
	if ok.Status != RequirementOK {
		t.Errorf("ok = %+v", ok)
	}
	miss := one(t, check(t, riskOf(req("boundary")), casesOf(kase("C1", spec.KindError, nil, "BR-1")), nil, b, nil))
	if miss.Status != RequirementMissing || !strings.Contains(miss.Detail, "BR-1") {
		t.Errorf("miss = %+v", miss)
	}
	min2 := one(t, check(t, riskOf(req("boundary", "min", 2)), casesOf(kase("C1", spec.KindBoundary, nil, "BR-1")), nil, b, nil))
	if min2.Status != RequirementMissing {
		t.Errorf("min2 = %+v", min2)
	}
}

func TestIdempotentPerMutatingEndpoint(t *testing.T) {
	s := one(t, check(t, riskOf(req("idempotent")),
		casesOf(kase("C1", spec.KindHappy, wh("POST", "/fee-waivers", 2, 1))), nil, nil,
		apiOf("POST /fee-waivers", "GET /fee-waivers")))
	if s.Status != RequirementOK {
		t.Errorf("s = %+v", s)
	}
}

func TestIdempotentMissingForSecondEndpoint(t *testing.T) {
	s := one(t, check(t, riskOf(req("idempotent")),
		casesOf(kase("C1", spec.KindHappy, wh("POST", "/fee-waivers", 2, 1)), kase("C2", spec.KindHappy, wh("DELETE", "/fee-waivers/7", 1, 1))),
		nil, nil, apiOf("POST /fee-waivers", "DELETE /fee-waivers/{id}")))
	if s.Status != RequirementMissing || !strings.Contains(s.Detail, "DELETE /fee-waivers/{id}") || strings.Contains(s.Detail, "POST") {
		t.Errorf("s = %+v", s)
	}
	ok := one(t, check(t, riskOf(req("idempotent")),
		casesOf(kase("C2", spec.KindHappy, wh("delete", "/fee-waivers/7?x=1", 3, 1))), nil, nil, apiOf("DELETE /fee-waivers/{id}")))
	if ok.Status != RequirementOK {
		t.Errorf("ok = %+v", ok)
	}
}

func TestIdempotentEndpointInBackticksWithTrailingPunctuation(t *testing.T) {
	ok := one(t, check(t, riskOf(req("idempotent")),
		casesOf(kase("C1", spec.KindHappy, wh("POST", "/api/v1/limits/check", 2, 1))), nil, nil,
		apiOf("- `POST /api/v1/limits/check`, body `{\"accountId\": \"A1\"}`.")))
	if ok.Status != RequirementOK {
		t.Errorf("ok = %+v", ok)
	}
	miss := one(t, check(t, riskOf(req("idempotent")),
		casesOf(kase("C1", spec.KindHappy, wh("POST", "/api/v1/limits/check", 1, 1))), nil, nil,
		apiOf("- `POST /api/v1/limits/check`, body `{}`.", "- PUT /x/{id}.")))
	d := miss.Detail
	if miss.Status != RequirementMissing || !strings.Contains(d, "POST /api/v1/limits/check, PUT /x/{id}") ||
		strings.Contains(d, "`") || strings.Contains(d, "{id}.") {
		t.Errorf("miss = %+v", miss)
	}
}

func TestIdempotentNoApiTableAnyRepeatCase(t *testing.T) {
	miss := one(t, check(t, riskOf(req("idempotent")), casesOf(kase("C1", spec.KindHappy, wh("POST", "/x", 1, 1))), nil, nil, nil))
	if miss.Status != RequirementMissing || miss.Detail != "chưa có case nào gửi trùng (when.repeat >= 2)" {
		t.Errorf("miss = %+v", miss)
	}
	ok := one(t, check(t, riskOf(req("idempotent")), casesOf(kase("C1", spec.KindHappy, wh("POST", "/x", 2, 1))), nil, nil, apiOf("GET /x")))
	if ok.Status != RequirementOK {
		t.Errorf("ok = %+v", ok)
	}
}

func TestConcurrentThreadsThreshold(t *testing.T) {
	cs := casesOf(kase("C1", spec.KindHappy, wh("POST", "/x", 1, 4)))
	r := one(t, check(t, riskOf(req("concurrent", "threads", 8)), cs, nil, nil, nil))
	if r.Status != RequirementMissing || r.Detail != "chưa có case nào có when.parallel >= 8" {
		t.Errorf("r = %+v", r)
	}
	if ok := one(t, check(t, riskOf(req("concurrent", "threads", 4)), cs, nil, nil, nil)); ok.Status != RequirementOK {
		t.Errorf("ok = %+v", ok)
	}
}

func TestInvariantOkMissingAndLite(t *testing.T) {
	d := &spec.DesignSpec{Invariants: []spec.Invariant{{Name: "n", SQL: "select 0"}}}
	if s := one(t, check(t, riskOf(req("invariant")), casesOf(), d, nil, nil)); s.Status != RequirementOK {
		t.Errorf("s = %+v", s)
	}
	if s := one(t, check(t, riskOf(req("invariant")), casesOf(), &spec.DesignSpec{}, nil, nil)); s.Status != RequirementMissing {
		t.Errorf("s = %+v", s)
	}
	lite := one(t, check(t, riskOf(req("invariant")), casesOf(), nil, nil, nil))
	if lite.Status != RequirementMissing || !strings.Contains(lite.Detail, "cases.yaml có 0/1 invariants") {
		t.Errorf("lite = %+v", lite)
	}
}

func TestInvariantLiteCountsCasesYamlInvariants(t *testing.T) {
	var withInv spec.CasesSpec
	err := yamlx.Parse([]byte("cases: []\ninvariants:\n  - { name: a, sql: \"SELECT 1 WHERE 1=0\" }\n  - { name: b, sql: \"SELECT 1 WHERE 1=0\", step: S9 }\n"), &withInv)
	if err != nil || len(withInv.Invariants) != 2 {
		t.Fatal(err, withInv)
	}
	if s := one(t, check(t, riskOf(req("invariant", "min", 2)), withInv, nil, nil, nil)); s.Status != RequirementOK {
		t.Errorf("s = %+v", s)
	}
	if s := one(t, check(t, riskOf(req("invariant", "min", 3)), withInv, nil, nil, nil)); !strings.Contains(s.Detail, "cases.yaml có 2/3 invariants") {
		t.Errorf("s = %+v", s)
	}
}

func TestCoverageAndSqlZeroAreLater(t *testing.T) {
	r := check(t, riskOf(req("coverage", "min", 90), req("sql-zero", "name", "n", "sql", "select 0")), casesOf(), nil, nil, nil)
	if len(r) != 2 || r[0].Status != RequirementLater || r[1].Status != RequirementLater {
		t.Fatalf("r = %+v", r)
	}
	if r[0].Detail != "kiểm ở Review" || r[1].Detail != "harness chạy sau mọi case" {
		t.Errorf("r = %+v", r)
	}
}

func TestNoRiskRequirementsEmpty(t *testing.T) {
	if r := check(t, riskOf(), casesOf(), nil, nil, nil); len(r) != 0 {
		t.Errorf("r = %+v", r)
	}
	if r := CheckRequirements(RiskFile{}, casesOf(), nil, nil, nil); len(r) != 0 {
		t.Errorf("r = %+v", r)
	}
}

func TestUnknownRequirementSkipped(t *testing.T) {
	if r := check(t, riskOf(req("nope")), casesOf(), nil, nil, nil); len(r) != 0 {
		t.Errorf("r = %+v", r)
	}
}

func TestFindingsOnlyForMissing(t *testing.T) {
	st := check(t, riskOf(req("invariant"), req("coverage", "min", 90)), casesOf(), nil, nil, nil)
	f := RequirementFindings(riskOf(req("invariant")), st)
	if len(f) != 1 || f[0].Code != "RISK_REQ" || f[0].Message != "Mức high yêu cầu Bất biến DB sau mọi case: cases.yaml có 0/1 invariants" {
		t.Errorf("f = %+v", f)
	}
}

func TestRequirementUnmarshalFlat(t *testing.T) {
	var r RiskFile
	if err := yamlx.Parse([]byte("requirements:\n  - { id: coverage, min: 90 }\n"), &r); err != nil {
		t.Fatal(err)
	}
	if r.Requirements[0].ID != "coverage" || r.Requirements[0].IntParam("min", 0) != 90 {
		t.Errorf("r = %+v", r)
	}
}
