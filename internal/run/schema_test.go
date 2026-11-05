package run

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRoundTripAndDiff(t *testing.T) {
	tmp := t.TempDir()
	actual := NewSchemaSnapshot(map[string][]string{"Users": {"name:TEXT:NOT NULL", "id:BIGINT:NOT NULL"}})
	expected := NewSchemaSnapshot(map[string][]string{"users": {"id:BIGINT:NOT NULL", "age:INT:NULL"}})
	p := filepath.Join(tmp, "x", "s.json")
	if err := actual.Write(p); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSchemaSnapshot(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, actual) {
		t.Errorf("got %v want %v", got, actual)
	}
	if _, ok := actual.Tables["users"]; !ok {
		t.Error("thiếu khóa users")
	}
	want := []string{"+users.name:TEXT:NOT NULL", "-users.age:INT:NULL"}
	if d := actual.Diff(expected); !reflect.DeepEqual(d, want) {
		t.Errorf("diff = %v", d)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "{\n  \"tables\": {\n    \"users\": [\n      \"id:BIGINT:NOT NULL\",\n      \"name:TEXT:NOT NULL\"\n    ]\n  }\n}" {
		t.Errorf("định dạng: %s", b)
	}
}

func TestReadSchemaSnapshotMissing(t *testing.T) {
	if _, err := ReadSchemaSnapshot(filepath.Join(t.TempDir(), "no.json")); err == nil {
		t.Fatal("cần lỗi")
	}
}

func TestPassedCountsAndNotPassedIds(t *testing.T) {
	r := CaseRunResult{ExitCode: 1, Cases: []CaseResult{
		{ID: "A", Step: "S1", Outcome: CaseOutcomePassed},
		{ID: "B", Step: "S1", Outcome: CaseOutcomeError, Message: "x"},
	}}
	if r.Passed() != 1 {
		t.Errorf("passed = %d", r.Passed())
	}
	if ids := r.NotPassedIDs(); !reflect.DeepEqual(ids, []string{"B"}) {
		t.Errorf("ids = %v", ids)
	}
}
