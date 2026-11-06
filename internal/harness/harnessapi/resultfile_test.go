package harnessapi

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"aisdlc/internal/lang"
	"aisdlc/internal/run"
)

// VerifyResult đi qua harness-result.json không mất gì (coverage theo dòng, unit test fail, cờ quá thời gian).
func TestResultFileRoundTrip(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	want := Result{Verify: &VerifyResult{
		Build:      lang.BuildResult{OK: true, Binary: "/b/svc", Output: "ok"},
		Unit:       lang.UnitResult{Exit: 1, Failed: []string{"ex.TestA"}, Profile: "/p", Output: "FAIL"},
		Run:        RunResult{ExitCode: 1, Cases: []run.CaseResult{{ID: "C1", Step: "S1", Outcome: run.CaseOutcomeFailed, Message: "m"}}},
		HarnessErr: "lỗi",
		Coverage:   lang.Coverage{Lines: map[string]map[int]bool{"a/b.go": {3: true, 7: false}}},
		TimedOut:   true,
	}}
	if err := WriteResult(out, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadResult(out)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, %v", got.Verify, err)
	}
}

func TestReadResultMissingCorruptOrEmptyIsError(t *testing.T) {
	out := t.TempDir()
	if _, err := ReadResult(out); err == nil {
		t.Error("thiếu file mà không lỗi")
	}
	for _, s := range []string{"{", "{}"} {
		if err := os.WriteFile(filepath.Join(out, ResultFile), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadResult(out); err == nil {
			t.Errorf("%q mà không lỗi", s)
		}
	}
}
