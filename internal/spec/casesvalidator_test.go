package spec

import (
	"reflect"
	"strings"
	"testing"

	"aisdlc/internal/model"
)

func kc(id, step string, kind CaseKind, stubs ...string) CaseSpec {
	return CaseSpec{ID: id, Step: step, Kind: kind, Title: "t", Given: Given{Stubs: stubs}}
}

func ccodes(s CasesSpec, steps []StepSpec, flow model.Flow) []string {
	return codesOf(ValidateCases(s, steps, flow))
}

const casesHead = "cases:\n  - id: C1\n    step: S1\n    kind: error\n    title: t\n"

func oneStep() []StepSpec {
	return []StepSpec{{ID: "S1", Title: "t", Scope: []string{"a/**"}, Budget: &Budget{1, 1}}}
}

func TestCasesTemplateIsValid(t *testing.T) {
	c, err := ReadCases(tmplFile(t, "cases.full.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if f := ValidateCases(c, spec2(t).Steps, model.FlowFull); len(f) != 0 {
		t.Fatalf("%v", f)
	}
}

func TestCasesDupID(t *testing.T) {
	s := CasesSpec{Cases: []CaseSpec{kc("C1", "S1", KindError), kc("C1", "S2", KindHappy)}}
	if got := ccodes(s, spec2(t).Steps, model.FlowFull); !reflect.DeepEqual(got, []string{"CASES_DUP_ID"}) {
		t.Fatalf("%v", got)
	}
}

func TestCasesUnknownStep(t *testing.T) {
	s := CasesSpec{Cases: []CaseSpec{kc("C1", "S1", KindError), kc("C2", "S2", KindHappy), kc("C3", "S9", KindHappy)}}
	if got := ccodes(s, spec2(t).Steps, model.FlowFull); !reflect.DeepEqual(got, []string{"CASES_UNKNOWN_STEP"}) {
		t.Fatalf("%v", got)
	}
}

func TestCasesStepUncovered(t *testing.T) {
	s := CasesSpec{Cases: []CaseSpec{kc("C1", "S1", KindError)}}
	if got := ccodes(s, spec2(t).Steps, model.FlowFull); !reflect.DeepEqual(got, []string{"CASES_STEP_UNCOVERED"}) {
		t.Fatalf("%v", got)
	}
}

func TestCasesNoErrorCase(t *testing.T) {
	s := CasesSpec{Cases: []CaseSpec{kc("C1", "S1", KindHappy), kc("C2", "S2", KindBoundary)}}
	if got := ccodes(s, spec2(t).Steps, model.FlowFull); !reflect.DeepEqual(got, []string{"CASES_NO_ERROR_CASE"}) {
		t.Fatalf("%v", got)
	}
}

func TestCasesUnknownStub(t *testing.T) {
	s := CasesSpec{Cases: []CaseSpec{kc("C1", "S1", KindError, "nope"), kc("C2", "S2", KindHappy)}}
	if got := ccodes(s, spec2(t).Steps, model.FlowFull); !reflect.DeepEqual(got, []string{"CASES_UNKNOWN_STUB"}) {
		t.Fatalf("%v", got)
	}
}

func TestCasesLiteNeedsScopeAndBudget(t *testing.T) {
	noScope := CasesSpec{Cases: []CaseSpec{kc("C1", "S1", KindError)}, Budget: &Budget{1, 1}}
	if got := ccodes(noScope, EffectiveSteps(model.FlowLite, nil, noScope), model.FlowLite); !reflect.DeepEqual(got, []string{"CASES_LITE_NO_SCOPE"}) {
		t.Fatalf("%v", got)
	}
	noBudget := CasesSpec{Cases: []CaseSpec{kc("C1", "S1", KindError)}, Scope: []string{"a/**"}}
	if got := ccodes(noBudget, EffectiveSteps(model.FlowLite, nil, noBudget), model.FlowLite); !reflect.DeepEqual(got, []string{"CASES_LITE_NO_SCOPE"}) {
		t.Fatalf("%v", got)
	}
}

func TestCasesLiteSingleStepFromCasesScope(t *testing.T) {
	lite, err := ReadCases(tmplFile(t, "cases.lite.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	steps := EffectiveSteps(model.FlowLite, nil, lite)
	if len(steps) != 1 || steps[0].ID != "S1" || steps[0].Title != "Thay đổi" ||
		!reflect.DeepEqual(steps[0].Scope, lite.Scope) || steps[0].Budget != lite.Budget {
		t.Fatalf("%+v", steps)
	}
	if f := ValidateCases(lite, steps, model.FlowLite); len(f) != 0 {
		t.Fatalf("%v", f)
	}
}

func TestCasesFullUsesDesignSteps(t *testing.T) {
	d := spec2(t)
	full, err := ReadCases(tmplFile(t, "cases.full.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(EffectiveSteps(model.FlowFull, &d, full), d.Steps) {
		t.Fatal("khác")
	}
}

func TestCasesInvariantsOnlyInLite(t *testing.T) {
	s := parseCases(t, casesHead+"    when: {method: GET, path: /x}\n    then: {status: 200}\n"+
		"invariants:\n  - {name: a, sql: \"SELECT 1 WHERE 1=0\"}\n")
	if !contains(ccodes(s, oneStep(), model.FlowFull), "CASES_INVARIANTS_FULL") {
		t.Fatal("full phải báo")
	}
	if contains(ccodes(s, oneStep(), model.FlowLite), "CASES_INVARIANTS_FULL") {
		t.Fatal("lite không báo")
	}
}

func TestCasesParallelNeedsStatus(t *testing.T) {
	s := parseCases(t, casesHead+"    when: {method: POST, path: /x, parallel: 4}\n    then: {}\n")
	if !contains(ccodes(s, oneStep(), model.FlowFull), "CASE_PARALLEL_STATUS") {
		t.Fatal("thiếu")
	}
	ok := parseCases(t, casesHead+"    when: {method: POST, path: /x, parallel: 4}\n    then: {status: 200}\n")
	if contains(ccodes(ok, oneStep(), model.FlowFull), "CASE_PARALLEL_STATUS") {
		t.Fatal("thừa")
	}
}

func TestCasesStatusCountsMustSum(t *testing.T) {
	bad := parseCases(t, casesHead+"    when: {method: POST, path: /x, parallel: 4}\n    then: {statusCounts: {200: 1, 409: 2}}\n")
	if !contains(ccodes(bad, oneStep(), model.FlowFull), "CASE_STATUS_COUNTS") {
		t.Fatal("thiếu")
	}
	ok := parseCases(t, casesHead+"    when: {method: POST, path: /x, parallel: 4}\n    then: {statusCounts: {200: 1, 409: 3}}\n")
	g := ccodes(ok, oneStep(), model.FlowFull)
	if contains(g, "CASE_STATUS_COUNTS") || contains(g, "CASE_PARALLEL_STATUS") {
		t.Fatalf("%v", g)
	}
}

func stubCases(t *testing.T, stub string) []Finding {
	t.Helper()
	s := parseCases(t, "stubs:\n"+stub+casesHead)
	return ValidateCases(s, oneStep(), model.FlowFull)
}

func TestStubUnsupportedKey(t *testing.T) {
	f := stubCases(t, "  - id: st\n    request: {method: GET, url: /a, cookies: {a: b}}\n    response: {status: 200, fault: RESET}\n")
	if len(f) != 2 {
		t.Fatalf("%v", f)
	}
	for _, x := range f {
		if x.Code != "CASES_STUB_UNSUPPORTED" {
			t.Fatalf("%v", x)
		}
	}
	if !strings.HasPrefix(f[0].Message, `Stub st: khóa "request.cookies" không được hỗ trợ (chỉ hỗ trợ `) ||
		!strings.HasPrefix(f[1].Message, `Stub st: khóa "response.fault" không được hỗ trợ (chỉ hỗ trợ `) ||
		!strings.HasSuffix(f[1].Message, ").") {
		t.Fatalf("%v", f)
	}
}

func TestStubSupportedSubsetPasses(t *testing.T) {
	f := stubCases(t, `  - id: st
    request:
      method: POST
      url: /a
      urlPath: /a
      urlPathPattern: /a/.*
      queryParameters: {q: {equalTo: "1"}, r: {matches: "x.*"}}
      headers: {H: {equalTo: v}}
      bodyPatterns:
        - {equalToJson: "{}"}
        - {contains: abc}
    response:
      status: 200
      headers: {A: b}
      body: x
      jsonBody: {a: 1}
      fixedDelayMilliseconds: 10
`)
	if len(f) != 0 {
		t.Fatalf("%v", f)
	}
}

func TestStubNestedUnsupported(t *testing.T) {
	f := stubCases(t, "  - id: st\n    request:\n      queryParameters: {q: {absent: true}}\n      bodyPatterns: [{matchesJsonPath: x}]\n")
	if len(f) != 2 || !strings.Contains(f[1].Message, `"request.queryParameters.q.absent"`) ||
		!strings.Contains(f[0].Message, `"request.bodyPatterns[0].matchesJsonPath"`) {
		t.Fatalf("%v", f)
	}
}

func TestStubWrongShapeMessage(t *testing.T) {
	cases := []struct{ stub, want string }{
		{"  - id: st\n    request: {queryParameters: [a]}\n",
			`Stub st: "request.queryParameters" sai cấu trúc (map tên → matcher, mỗi matcher đúng một khóa trong equalTo, matches).`},
		{"  - id: st\n    request: {bodyPatterns: {contains: a}}\n",
			`Stub st: "request.bodyPatterns" sai cấu trúc (danh sách, mỗi phần tử đúng một khóa trong equalToJson, contains).`},
		{"  - id: st\n    request: {headers: {H: {}}}\n",
			`Stub st: "request.headers.H" sai cấu trúc (đúng một khóa trong equalTo, matches).`},
		{"  - id: st\n    request: {headers: {H: x}}\n",
			`Stub st: "request.headers.H" sai cấu trúc (đúng một khóa trong equalTo, matches).`},
		// hai khóa đều được hỗ trợ: sai cấu trúc, không phải khóa lạ
		{"  - id: st\n    request: {queryParameters: {q: {equalTo: a, matches: b}}}\n",
			`Stub st: "request.queryParameters.q" sai cấu trúc (đúng một khóa trong equalTo, matches).`},
	}
	for _, c := range cases {
		f := stubCases(t, c.stub)
		if len(f) != 1 || f[0].Code != "CASES_STUB_UNSUPPORTED" || f[0].Message != c.want {
			t.Errorf("%q: got %v want %s", c.stub, f, c.want)
		}
	}
	// khóa lạ thật sự giữ câu cũ
	f := stubCases(t, "  - id: st\n    request: {cookies: x}\n")
	if len(f) != 1 || !strings.HasPrefix(f[0].Message, `Stub st: khóa "request.cookies" không được hỗ trợ (chỉ hỗ trợ `) {
		t.Errorf("%v", f)
	}
}
