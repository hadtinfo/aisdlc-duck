package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"aisdlc/internal/yamlx"
	"aisdlc/templates"
)

// tmplFile ghi template nhúng ra file tạm và trả đường dẫn.
func tmplFile(t *testing.T, name string) string {
	t.Helper()
	data, err := templates.FS.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func parseCases(t *testing.T, y string) CasesSpec {
	t.Helper()
	var c CasesSpec
	if err := yamlx.Parse([]byte(y), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestParsesTemplateFull(t *testing.T) {
	spec, err := ReadCases(tmplFile(t, "cases.full.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.ResetSQL) != 0 {
		t.Errorf("reset_sql = %v", spec.ResetSQL)
	}
	if got := spec.Cases[1].Given.SQL; !reflect.DeepEqual(got, []string{"DELETE FROM transfer_limit_usage"}) {
		t.Errorf("given.sql = %v", got)
	}
	if len(spec.Stubs) != 1 || spec.Stubs[0].ID != "core-balance-ok" {
		t.Errorf("stubs = %v", spec.Stubs)
	}
	c1 := spec.Cases[0]
	if c1.ID != "C1" || c1.Kind != KindError || c1.Then.Status != 400 || c1.When.Method != "POST" {
		t.Errorf("c1 = %+v", c1)
	}
	if len(spec.Cases[1].Then.DB) != 1 {
		t.Errorf("db = %v", spec.Cases[1].Then.DB)
	}
}

func TestParsesTemplateLite(t *testing.T) {
	spec, err := ReadCases(tmplFile(t, "cases.lite.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Scope) == 0 {
		t.Error("scope rỗng")
	}
	if spec.Budget == nil || *spec.Budget != (Budget{3, 120}) {
		t.Errorf("budget = %v", spec.Budget)
	}
}

func TestReadCasesSyntaxError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cases.yaml")
	if err := os.WriteFile(p, []byte("cases: [ {id: C1"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadCases(p)
	if err == nil || !strings.Contains(err.Error(), "cases.yaml") || !strings.Contains(err.Error(), "dòng") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownCaseKindFails(t *testing.T) {
	var c CasesSpec
	err := yamlx.Parse([]byte("cases:\n  - id: C1\n    kind: smoke\n"), &c)
	if err == nil || !strings.Contains(err.Error(), "smoke") {
		t.Fatalf("err = %v", err)
	}
}

func TestKindTrimAndCase(t *testing.T) {
	c := parseCases(t, "cases:\n  - {id: C1, kind: \" ERROR \"}\n")
	if c.Cases[0].Kind != KindError {
		t.Errorf("kind = %q", c.Cases[0].Kind)
	}
}

func TestStatusCountsIntKeys(t *testing.T) {
	c := parseCases(t, "cases:\n  - id: C1\n    then: {statusCounts: {200: 1, 409: 4}}\n")
	if got := c.Cases[0].Then.StatusCounts; !reflect.DeepEqual(got, map[int]int{200: 1, 409: 4}) {
		t.Errorf("got %v", got)
	}
}

func TestUntypedValuesStayJSONCompatible(t *testing.T) {
	c := parseCases(t, `
stubs:
  - id: s1
    request: {method: GET, url: /x, 7: seven}
    response:
      status: 200
      jsonBody: {when: 2026-11-05, 1: one}
cases:
  - id: C1
    when:
      method: POST
      path: /x
      body: {date: 2026-11-05, at: 2026-11-05T10:00:00Z, 5: five}
    then:
      status: 200
      body: {day: 2026-11-05}
      db:
        - query: select 1
          rows:
            - {d: 2026-11-05, n: 3}
      messages:
        - topic: t
          value: {ts: 2026-11-05T10:00:00Z}
`)
	cs := c.Cases[0]
	if got := cs.Then.DB[0].Rows[0]["d"]; got != "2026-11-05" {
		t.Errorf("rows date = %#v", got)
	}
	if got := cs.When.Body.(map[string]any)["date"]; got != "2026-11-05" {
		t.Errorf("when.body date = %#v", got)
	}
	if got := cs.When.Body.(map[string]any)["5"]; got != "five" {
		t.Errorf("when.body int key = %#v", got)
	}
	if cs.When.Repeat != 1 || cs.Then.Status != 200 {
		t.Errorf("when/then = %+v %+v", cs.When, cs.Then)
	}
	if got := c.Stubs[0].Response["jsonBody"].(map[string]any)["when"]; got != "2026-11-05" {
		t.Errorf("stub jsonBody = %#v", got)
	}
	if got := c.Stubs[0].Request["7"]; got != "seven" {
		t.Errorf("stub request int key = %#v", got)
	}
	if _, err := json.Marshal(c); err != nil {
		t.Errorf("marshal: %v", err)
	}
}
