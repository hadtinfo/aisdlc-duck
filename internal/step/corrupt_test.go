package step

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func checkCorrupt(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
	var se *json.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("không chuỗi được nguyên nhân: %v", err)
	}
}

func TestStepsJSONCorruptMessageVerbatim(t *testing.T) {
	e := newEnv(t)
	s := e.service()
	file := s.paths.Steps()
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.WriteFile(file, []byte("{not json"), 0o644)
	_, err := s.load()
	checkCorrupt(t, err, "steps.json hỏng: "+file)
}

func TestScopeExtraCorruptMessageVerbatim(t *testing.T) {
	file := filepath.Join(t.TempDir(), "scope-extra.json")
	os.WriteFile(file, []byte("{not json"), 0o644)
	_, err := (&ScopeExtras{file: file}).load()
	checkCorrupt(t, err, "scope-extra.json hỏng: "+file)
}
