package risk

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const fullPolicy = `schema: 1
preset: { name: banking, version: "0.3" }
levels: [low, medium, high]
default: low
rules:
  - id: R1
    kind: keyword
    match: "hạch toán|bút toán|định khoản"
    level: high
    note: Mọi thay đổi chạm sổ cái
    enabled: true
  - id: R2
    kind: keyword
    match: "phí|fee"
    level: high
  - id: R3
    kind: path
    match: "**/accounting/**"
    level: high
  - id: R4
    kind: table
    match: ["ledger_*", "fee_*", "account_balance"]
    level: high
classification:
  agent: raise
  devLower: reason
requirements:
  medium:
    - { id: rule-pos-neg }
    - { id: boundary }
    - { id: coverage, min: 75 }
  high:
    - { id: rule-pos-neg }
    - { id: concurrent, threads: 8 }
    - { id: coverage, min: 90 }
    - { id: sql-zero, name: "Tổng nợ bằng tổng có", sql: "SELECT txn_id FROM t GROUP BY txn_id" }
`

func withRules(rules string) string {
	return "schema: 1\nlevels: [low, high]\ndefault: low\nrules:\n" + rules
}

func mustParse(t *testing.T, y string) RiskPolicy {
	t.Helper()
	p, err := ParsePolicy(y)
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	return p
}

func policyErrors(t *testing.T, y string) []string {
	t.Helper()
	_, err := ParsePolicy(y)
	var pe *PolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PolicyError, got %v", err)
	}
	return pe.Errors
}

func anyContains(errs []string, subs ...string) bool {
	for _, e := range errs {
		ok := true
		for _, s := range subs {
			if !strings.Contains(e, s) {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func TestVersionDefaultsToZeroWhenFileOmitsIt(t *testing.T) {
	if v := mustParse(t, fullPolicy).Version; v != 0 {
		t.Errorf("version = %d", v)
	}
	if v := mustParse(t, "version: 4\n"+fullPolicy).Version; v != 4 {
		t.Errorf("version = %d", v)
	}
}

func TestParse_fullExample(t *testing.T) {
	p := mustParse(t, fullPolicy)
	if len(p.Levels) != 3 || p.DefaultLevel != "low" {
		t.Errorf("levels/default: %v %q", p.Levels, p.DefaultLevel)
	}
	if !reflect.DeepEqual(p.Rules[3].Match, []string{"ledger_*", "fee_*", "account_balance"}) {
		t.Errorf("match = %v", p.Rules[3].Match)
	}
	found := false
	for _, r := range p.RequirementsFor("high") {
		if r.ID == "concurrent" && r.IntParam("threads", 0) == 8 {
			found = true
		}
	}
	if !found {
		t.Error("concurrent threads 8 không có")
	}
	if p.Classification.Agent != AgentRaise {
		t.Errorf("agent = %q", p.Classification.Agent)
	}
	if !p.Rules[0].Enabled || !p.Rules[1].Enabled {
		t.Error("enabled mặc định phải true")
	}
	if p.Rank("high") != 2 || p.Rank("zzz") != -1 {
		t.Error("rank sai")
	}
	if len(p.RequirementsFor("nope")) != 0 {
		t.Error("requirementsFor mức lạ phải rỗng")
	}
}

func TestParse_matchStringBecomesList(t *testing.T) {
	p := mustParse(t, withRules("  - { id: A, kind: keyword, match: \"a|b\", level: high }\n"))
	if !reflect.DeepEqual(p.Rules[0].Match, []string{"a|b"}) {
		t.Errorf("match = %v", p.Rules[0].Match)
	}
}

func TestParse_reportsEveryError(t *testing.T) {
	errs := policyErrors(t, withRules(`  - { id: R1, kind: keyword, match: "((", level: high }
  - { id: R2, kind: path, match: [], level: high }
  - { id: R3, kind: path, match: "x", level: urgent }
  - { id: R4, kind: path, match: "x", level: high }
  - { id: R4, kind: path, match: "y", level: high }
requirements:
  high:
    - { id: foo }
    - { id: coverage }
`))
	if len(errs) != 6 {
		t.Fatalf("want 6 errors, got %d: %v", len(errs), errs)
	}
	for _, c := range [][]string{{"R1", "regex"}, {"R2", "match"}, {"R3", "urgent"}, {"R4", "trùng"}, {"foo"}, {"coverage", "min"}} {
		if !anyContains(errs, c...) {
			t.Errorf("thiếu lỗi %v trong %v", c, errs)
		}
	}
}

func TestParse_sqlZeroMustBeSelect(t *testing.T) {
	base := "schema: 1\nlevels: [low, high]\ndefault: low\nrequirements:\n  high:\n    - { id: sql-zero, name: n, sql: \"%s\" }\n"
	if errs := policyErrors(t, strings.Replace(base, "%s", "DELETE FROM x", 1)); !anyContains(errs, "SELECT") {
		t.Errorf("errs = %v", errs)
	}
	p := mustParse(t, strings.Replace(base, "%s", "with t as (select 1) select * from t", 1))
	if len(p.RequirementsFor("high")) != 1 {
		t.Error("with ... select phải hợp lệ")
	}
	if errs := policyErrors(t, strings.Replace(base, "%s", "select 1; select 2", 1)); !anyContains(errs, "SELECT") {
		t.Errorf("nhiều câu phải bị từ chối: %v", errs)
	}
}

func TestParse_levelsBounds(t *testing.T) {
	policyErrors(t, "schema: 1\nlevels: [a]\ndefault: a\n")
	policyErrors(t, "schema: 1\nlevels: [a,b,c,d,e,f]\ndefault: a\n")
	if p := mustParse(t, "schema: 1\nlevels: [a,b]\ndefault: a\n"); len(p.Levels) != 2 {
		t.Error("2 mức hợp lệ")
	}
}

func TestRoundTrip_yamlJsonHashStable(t *testing.T) {
	p := mustParse(t, fullPolicy)
	y, err := p.ToYAML()
	if err != nil {
		t.Fatal(err)
	}
	q := mustParse(t, y)
	if q.Hash() != p.Hash() {
		t.Error("hash đổi sau vòng YAML")
	}
	if !reflect.DeepEqual(q, p) {
		t.Errorf("YAML round trip khác:\n%+v\n%+v", q, p)
	}
	r, err := PolicyFromJSON(p.ToJSON())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r, p) {
		t.Errorf("JSON round trip khác:\n%+v\n%+v", r, p)
	}
	if !strings.HasPrefix(p.Hash(), "sha256:") {
		t.Error("hash phải bắt đầu sha256:")
	}
}

func TestToJSON_jacksonShape(t *testing.T) {
	p := mustParse(t, "schema: 1\nlevels: [low, high]\ndefault: low\nrules:\n  - { id: A, kind: Keyword, match: \"a\\\"b\", level: high }\n")
	want := `{"classification":{"agent":"raise","devLower":"reason"},"default":"low","levels":["low","high"],"preset":null,` +
		`"requirements":{},"rules":[{"enabled":true,"id":"A","kind":"keyword","level":"high","match":["a\"b"]}],"schema":1,"version":0}`
	if got := p.ToJSON(); got != want {
		t.Errorf("json:\n%s\n%s", got, want)
	}
}

func TestParse_unknownKeysReported(t *testing.T) {
	errs := policyErrors(t, `schema: 1
levles: [x]
levels: [low, high]
default: low
preset: { name: a, versoin: "1" }
classification: { agnet: raise }
rules:
  - { id: R1, kind: path, match: x, level: high, notes: oops }
requirments: {}
`)
	if len(errs) != 5 {
		t.Fatalf("want 5 errors: %v", errs)
	}
	for _, c := range [][]string{{"levles"}, {"requirments"}, {"versoin"}, {"agnet"}, {"R1", "notes"}} {
		if !anyContains(errs, c...) {
			t.Errorf("thiếu lỗi %v trong %v", c, errs)
		}
	}
}

func TestParse_nullEntryReported(t *testing.T) {
	if errs := policyErrors(t, withRules("  - \n")); !anyContains(errs, "rules[0]") {
		t.Errorf("errs = %v", errs)
	}
	if errs := policyErrors(t, "schema: 1\nlevels: [low, high]\ndefault: low\nrequirements:\n  high:\n    - \n"); !anyContains(errs, "requirements.high[0]") {
		t.Errorf("errs = %v", errs)
	}
}

func TestParse_unknownKindReported(t *testing.T) {
	if errs := policyErrors(t, withRules("  - { id: R1, kind: bogus, match: x, level: high }\n")); !anyContains(errs, "bogus", "R1") {
		t.Errorf("errs = %v", errs)
	}
}

func TestParse_blankMatchEntryReported(t *testing.T) {
	if errs := policyErrors(t, withRules("  - { id: R9, kind: path, match: [\"a\", \" \"], level: high }\n")); !anyContains(errs, "R9") {
		t.Errorf("errs = %v", errs)
	}
}

func TestParse_nullMatchElementIsDropped(t *testing.T) {
	p := mustParse(t, "schema: 1\nlevels: [low, high]\ndefault: low\nrules:\n  - { id: R1, kind: keyword, match: [abc, null], level: high }\n")
	if !reflect.DeepEqual(p.Rules[0].Match, []string{"abc"}) {
		t.Errorf("match = %v", p.Rules[0].Match)
	}
}

func TestParse_emptyOrWrongShape(t *testing.T) {
	if errs := policyErrors(t, ""); !anyContains(errs, "Chính sách rỗng hoặc sai dạng") {
		t.Errorf("errs = %v", errs)
	}
	if errs := policyErrors(t, "- a\n- b\n"); !anyContains(errs, "Chính sách rỗng hoặc sai dạng") {
		t.Errorf("errs = %v", errs)
	}
	if _, err := PolicyFromJSON("{not json"); err == nil || !strings.Contains(err.Error(), "JSON chính sách không hợp lệ") {
		t.Errorf("err = %v", err)
	}
}

func TestPolicyError_message(t *testing.T) {
	e := &PolicyError{Errors: []string{"a", "b"}}
	if e.Error() != "Chính sách rủi ro không hợp lệ: a; b" {
		t.Errorf("msg = %q", e.Error())
	}
}

const flowBase = "schema: 1\nlevels: [low, medium, high]\ndefault: low\n"

func TestParse_flow(t *testing.T) {
	p := mustParse(t, flowBase+"flow:\n  low: { brd: auto, design: auto, cases: auto, step: auto }\n  medium: { brd: manual, step: auto }\n")
	cases := []struct{ level, unit, want string }{
		{"low", "brd", "auto"}, {"low", "step", "auto"}, {"medium", "step", "auto"},
		{"medium", "brd", "manual"}, {"medium", "design", "manual"},
	}
	for _, c := range cases {
		if got := p.FlowFor(c.level, c.unit); got != c.want {
			t.Errorf("FlowFor(%s,%s) = %s", c.level, c.unit, got)
		}
	}
}

func TestParse_flowReviewKey_error(t *testing.T) {
	if errs := policyErrors(t, flowBase+"flow:\n  low: { review: auto }\n"); !anyContains(errs, "review") {
		t.Errorf("errs = %v", errs)
	}
}

func TestParse_flowBadValue_error(t *testing.T) {
	if errs := policyErrors(t, flowBase+"flow:\n  low: { brd: auto2 }\n"); !anyContains(errs, "auto2") {
		t.Errorf("errs = %v", errs)
	}
}

func TestParse_flowUnknownLevel_error(t *testing.T) {
	if errs := policyErrors(t, flowBase+"flow:\n  urgent: { brd: auto }\n"); !anyContains(errs, "urgent") {
		t.Errorf("errs = %v", errs)
	}
}

func TestFlowDefaultsManual(t *testing.T) {
	p := mustParse(t, flowBase)
	if len(p.Flow) != 0 {
		t.Error("flow phải rỗng")
	}
	for _, u := range FlowUnits {
		if p.FlowFor("low", u) != "manual" {
			t.Errorf("%s phải manual", u)
		}
	}
	if p.FlowFor("nope", "brd") != "manual" {
		t.Error("mức lạ phải manual")
	}
}

func TestFlowIsPartOfHash_andAbsentFlowKeepsOldJson(t *testing.T) {
	a := mustParse(t, flowBase)
	b := mustParse(t, flowBase+"flow:\n  low: { brd: auto }\n")
	if a.Hash() == b.Hash() {
		t.Error("flow phải nằm trong hash")
	}
	if strings.Contains(a.ToJSON(), "flow") {
		t.Error("không có flow thì JSON không có khóa flow")
	}
	r, err := PolicyFromJSON(b.ToJSON())
	if err != nil || r.FlowFor("low", "brd") != "auto" {
		t.Errorf("fromJson: %v %v", r, err)
	}
}

func TestEmptyLevelFlowHashesLikeAbsent(t *testing.T) {
	if mustParse(t, flowBase+"flow:\n  low: {}\n").Hash() != mustParse(t, flowBase).Hash() {
		t.Error("flow rỗng phải băm như không có")
	}
}

const schema2Base = `schema: 2
version: 1
levels: [low, high]
default: low
rules:
  - { id: R1, level: high, describe: "Chạm sổ cái" }
requirements:
  high:
    - { id: idempotent }
`

func TestParseSchema2(t *testing.T) {
	p, err := ParsePolicy(schema2Base)
	if err != nil {
		t.Fatal(err)
	}
	if p.Schema != 2 || p.Rules[0].Describe != "Chạm sổ cái" || !p.Rules[0].Enabled {
		t.Errorf("p = %+v", p)
	}
	if err := p.Usable(); err != nil {
		t.Errorf("Usable = %v", err)
	}
}

func TestSchema2RejectsMatchKeys(t *testing.T) {
	y := strings.Replace(schema2Base, `describe: "Chạm sổ cái" }`, `describe: x, kind: label, match: a }`, 1)
	_, err := ParsePolicy(y)
	if err == nil || !strings.Contains(err.Error(), "R1: khóa lạ 'kind'") || !strings.Contains(err.Error(), "R1: khóa lạ 'match'") {
		t.Errorf("err = %v", err)
	}
}

func TestSchema2DescribeRequired(t *testing.T) {
	for _, d := range []string{`, describe: ""`, `, describe: "  "`, ``} {
		y := strings.Replace(schema2Base, `, describe: "Chạm sổ cái"`, d, 1)
		_, err := ParsePolicy(y)
		if err == nil || !strings.Contains(err.Error(), "R1: describe không được rỗng") {
			t.Errorf("%q: err = %v", d, err)
		}
	}
}

func TestSchema2RejectsAgentClassification(t *testing.T) {
	y := schema2Base + "classification: { agent: raise, devLower: reason }\n"
	_, err := ParsePolicy(y)
	if err == nil || !strings.Contains(err.Error(), "classification: khóa lạ 'agent'") {
		t.Errorf("err = %v", err)
	}
	if _, err := ParsePolicy(schema2Base + "classification: { devLower: never }\n"); err != nil {
		t.Errorf("devLower phải hợp lệ: %v", err)
	}
}

func TestSchema2FlowBrdWarning(t *testing.T) {
	y := schema2Base + "flow:\n  high: { brd: manual, design: auto }\n"
	p, err := ParsePolicy(y)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := p.Flow["high"]["brd"]; has || p.Flow["high"]["design"] != "auto" {
		t.Errorf("flow = %v", p.Flow)
	}
	if len(p.Warnings) != 1 || p.Warnings[0] != "flow.high.brd bị bỏ qua: cổng BRD luôn duyệt tay" {
		t.Errorf("warnings = %v", p.Warnings)
	}
}

func TestSchema2JSONRoundTrip(t *testing.T) {
	p, _ := ParsePolicy(schema2Base)
	js := p.ToJSON()
	if strings.Contains(js, `"kind"`) || strings.Contains(js, `"match"`) || !strings.Contains(js, `"describe":"Chạm sổ cái"`) {
		t.Errorf("json = %s", js)
	}
	q, err := PolicyFromJSON(js)
	if err != nil || q.Hash() != p.Hash() {
		t.Errorf("round trip: %v", err)
	}
	y, err := p.ToYAML()
	if err != nil || strings.Contains(y, "kind:") || strings.Contains(y, "match:") || !strings.Contains(y, "describe:") {
		t.Errorf("yaml = %s (%v)", y, err)
	}
}

func TestUsableRejectsSchema1(t *testing.T) {
	p, err := LoadPreset("generic", "2")
	if err != nil {
		t.Fatal(err)
	}
	err = p.Usable()
	var pe *PolicyError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "`aisdlc policy migrate`") {
		t.Errorf("err = %v", err)
	}
}

func TestSchema1HashUnchanged(t *testing.T) {
	p, err := LoadPreset("generic", "2")
	if err != nil {
		t.Fatal(err)
	}
	const golden = "sha256:40dcc55640ceddbd59b8885d815b33a683407b46744014862af35f5e5244beea" // ghi từ main trước khi đổi
	if p.Hash() != golden {
		t.Errorf("hash = %s", p.Hash())
	}
}
