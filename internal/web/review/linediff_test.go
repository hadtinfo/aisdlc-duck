package review

import (
	"strconv"
	"strings"
	"testing"

	"aisdlc/internal/i18n"
)

// Port LineDiffTest (8).

func numbered(n int, prefix string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString(prefix + strconv.Itoa(i) + "\n")
	}
	return b.String()
}

func sp(s string) *string { return &s }

func TestIdenticalIsEmpty(t *testing.T) {
	if p := Unified("a.md", sp("x\ny\n"), sp("x\ny\n"), i18n.VI); p != "" {
		t.Fatalf("%q", p)
	}
}

func TestOneChangeInMiddleGivesOneHunkWithThreeContextLines(t *testing.T) {
	a := numbered(10, "l")
	b := strings.Replace(a, "l5\n", "L5\n", 1)
	p := Unified("a.md", &a, &b, i18n.VI)
	if !strings.HasPrefix(p, "--- a/a.md\n+++ b/a.md\n@@ -2,7 +2,7 @@\n") || !strings.Contains(p, "\n-l5\n+L5\n") {
		t.Fatalf("%q", p)
	}
	if n := len(strings.Split(p, "\n@@ -")); n != 2 {
		t.Fatalf("hunks %d: %q", n-1, p)
	}
	if c := CountLines(p); c != (Counts{1, 1}) {
		t.Fatal(c)
	}
}

func TestAddedFileUsesDevNull(t *testing.T) {
	p := Unified("n.txt", nil, sp("a\nb\n"), i18n.VI)
	if p != "--- /dev/null\n+++ b/n.txt\n@@ -0,0 +1,2 @@\n+a\n+b\n" {
		t.Fatalf("%q", p)
	}
	if c := CountLines(p); c != (Counts{2, 0}) {
		t.Fatal(c)
	}
}

func TestRemovedFile(t *testing.T) {
	p := Unified("n.txt", sp("a\nb\n"), nil, i18n.VI)
	if p != "--- a/n.txt\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-a\n-b\n" {
		t.Fatalf("%q", p)
	}
	if c := CountLines(p); c != (Counts{0, 2}) {
		t.Fatal(c)
	}
}

func TestChangesSixLinesApartMergeButSevenSplit(t *testing.T) {
	a := numbered(30, "l")
	m := strings.Replace(strings.Replace(a, "l5\n", "X\n", 1), "l12\n", "Y\n", 1)
	if n := len(strings.Split(Unified("f", &a, &m, i18n.VI), "@@ -")); n != 2 {
		t.Fatalf("gộp: %d", n)
	}
	s := strings.Replace(strings.Replace(a, "l5\n", "X\n", 1), "l13\n", "Y\n", 1)
	split := Unified("f", &a, &s, i18n.VI)
	if n := len(strings.Split(split, "@@ -")); n != 3 {
		t.Fatalf("tách: %d %q", n, split)
	}
	if c := CountLines(split); c != (Counts{2, 2}) {
		t.Fatal(c)
	}
}

func TestLastLineWithoutNewlineIsKept(t *testing.T) {
	if p := Unified("f", sp("a\nb"), sp("a\nc"), i18n.VI); !strings.Contains(p, "-b\n+c\n") {
		t.Fatalf("%q", p)
	}
}

func TestHeaderLinesAreNotCounted(t *testing.T) {
	if c := CountLines("--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n+c\n"); c != (Counts{2, 1}) {
		t.Fatal(c)
	}
}

func TestTooLargeIsNotCompared(t *testing.T) {
	if p := Unified("big", sp(numbered(2001, "a")), sp(numbered(2001, "b")), i18n.VI); p != "--- a/big\n+++ b/big\n@@ file quá lớn để so @@\n" {
		t.Fatalf("%q", p)
	}
}
