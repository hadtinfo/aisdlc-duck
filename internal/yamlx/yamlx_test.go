package yamlx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIgnoresUnknownKeys(t *testing.T) {
	var out struct {
		A int `yaml:"a"`
	}
	if err := Parse([]byte("a: 1\nzzz: 2"), &out); err != nil {
		t.Fatal(err)
	}
	if out.A != 1 {
		t.Fatalf("A = %d", out.A)
	}
}

func TestReadSyntaxErrorHasFileAndLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cases.yaml")
	if err := os.WriteFile(p, []byte("a: 1\nb: 2\nc: x: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	err := Read(p, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"cases.yaml", "dòng 3", "Không đọc được"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q thiếu %q", err, want)
		}
	}
}

func TestParseSyntaxErrorPrefix(t *testing.T) {
	var out map[string]any
	err := Parse([]byte("a: [unclosed"), &out)
	if err == nil || !strings.HasPrefix(err.Error(), "YAML không hợp lệ: ") {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteCreatesParentAndNoDocMarker(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x", "y.yaml")
	if err := Write(p, map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(string(b), "---") {
		t.Fatalf("có ---: %q", b)
	}
}

func TestReadUnclosedFlowHasFileAndLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "design.yaml")
	if err := os.WriteFile(p, []byte("a: 1\nb:\n  - [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	err := Read(p, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"design.yaml", "dòng "} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q thiếu %q", err, want)
		}
	}
}
