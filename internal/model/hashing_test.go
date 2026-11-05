package model

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContentsHashEqualsFileHash(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.md"), []byte("# A\n"), 0o644)
	os.MkdirAll(filepath.Join(tmp, "db"), 0o755)
	os.WriteFile(filepath.Join(tmp, "db/b.sql"), []byte("create table t();\n"), 0o644)
	files := []string{filepath.Join(tmp, "a.md"), filepath.Join(tmp, "db/b.sql"), filepath.Join(tmp, "missing.txt")}
	entries := []Content{{"a.md", []byte("# A\n")}, {"db/b.sql", []byte("create table t();\n")}, {"missing.txt", nil}}
	got := SHA256Contents(entries)
	want, err := SHA256Files(tmp, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 64 || got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestContentsHashDependsOnOrder(t *testing.T) {
	a := Content{"a", []byte{1}}
	b := Content{"b", []byte{2}}
	if SHA256Contents([]Content{a, b}) == SHA256Contents([]Content{b, a}) {
		t.Fatal("phải phụ thuộc thứ tự")
	}
}

func TestHashKnownVector(t *testing.T) {
	got := SHA256Contents([]Content{{"a.txt", []byte("x")}, {"b.txt", nil}})
	if got != "64437e6cefe8cc304c508249c0275f9f5e9421f3b52a95c7dcff23ac6978b9f6" {
		t.Fatalf("got %s", got)
	}
}
