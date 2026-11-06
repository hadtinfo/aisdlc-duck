package harness

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"aisdlc/internal/run"
	"aisdlc/internal/spec"
)

var sel = []spec.CaseSpec{{ID: "C1", Step: "S1"}, {ID: "C2", Step: "S1"}}

func TestResultsRoundTripAndFormat(t *testing.T) {
	out := t.TempDir()
	cases := []run.CaseResult{
		{ID: "C1", Step: "S1", Outcome: run.CaseOutcomePassed},
		{ID: "C9", Step: "S1", Outcome: run.CaseOutcomeFailed, Message: "mong đợi 201"},
	}
	if err := WriteResults(filepath.Join(out, "sub"), "DEMO-1", ModeCases, cases); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out, "sub/results.json"))
	want := "{\n  \"feature\": \"DEMO-1\",\n  \"mode\": \"cases\",\n  \"cases\": [\n    {\n      \"id\": \"C1\","
	if !strings.HasPrefix(string(b), want) {
		t.Errorf("format:\n%s", b)
	}
	got := ReadResults(filepath.Join(out, "sub"), []spec.CaseSpec{{ID: "C1", Step: "S1"}, {ID: "C9", Step: "S1"}})
	if !reflect.DeepEqual(got, cases) {
		t.Errorf("got %v", got)
	}
}

func TestResultsEmptyCasesWritesArray(t *testing.T) {
	out := t.TempDir()
	if err := WriteResults(out, "F-1", ModeMigrationCheck, nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out, "results.json"))
	if !strings.Contains(string(b), `"cases": []`) {
		t.Errorf("%s", b)
	}
}

func TestReadResultsMissingRowIsError(t *testing.T) {
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "results.json"),
		[]byte(`{"cases":[{"id":"C1","step":"S1","outcome":"PASSED","message":""}]}`), 0o644)
	got := ReadResults(out, sel)
	want := []run.CaseResult{
		{ID: "C1", Step: "S1", Outcome: run.CaseOutcomePassed},
		{ID: "C2", Step: "S1", Outcome: run.CaseOutcomeError, Message: NoRow},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestReadResultsEmptyMakesEveryCaseError(t *testing.T) {
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "results.json"), []byte(`{"cases":[]}`), 0o644)
	for _, c := range ReadResults(out, sel) {
		if c.Outcome != run.CaseOutcomeError {
			t.Errorf("%v", c)
		}
	}
}

func TestReadResultsMissingFileIsNoResults(t *testing.T) {
	got := ReadResults(t.TempDir(), sel)
	if len(got) != 2 || got[0].Message != NoResults || got[1].Outcome != run.CaseOutcomeError {
		t.Errorf("got %v", got)
	}
}

func TestReadResultsCorruptFileIsNoResults(t *testing.T) {
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "results.json"), []byte(`{"cases":[{"id":`), 0o644)
	got := ReadResults(out, sel)
	if len(got) != 2 || got[0].Message != NoResults {
		t.Errorf("got %v", got)
	}
}
