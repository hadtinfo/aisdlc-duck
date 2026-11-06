package golang_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/lang"
	"aisdlc/internal/lang/golang"
)

const sampleProfile = "mode: atomic\nexample.com/m/p/a.go:3.10,5.2 2 1\nexample.com/m/p/a.go:7.1,7.20 1 0\nother.org/x/b.go:1.1,2.2 1 1\n"

func TestParseProfileBlocksToLines(t *testing.T) {
	got, err := golang.ParseProfile(strings.NewReader(sampleProfile), "example.com/m")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[int]bool{"p/a.go": {3: true, 4: true, 5: true, 7: false}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestMergeProfilesOrsCounts(t *testing.T) {
	a, _ := golang.ParseProfile(strings.NewReader("mode: set\nexample.com/m/a.go:1.1,2.2 1 0\n"), "example.com/m")
	b, _ := golang.ParseProfile(strings.NewReader("mode: set\nexample.com/m/a.go:1.1,2.2 1 1\nexample.com/m/a.go:9.1,9.5 1 0\n"), "example.com/m")
	got := golang.MergeLines(a, b)
	want := map[string]map[int]bool{"a.go": {1: true, 2: true, 9: false}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

const covMain = `package main

import (
	"os"
	"os/signal"
	"syscall"
)

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM)
	<-c
	hit()
}

func hit() {
	println("hit")
}

func never() {
	println("never")
}
`

func TestCoverageFromCoverDirAndUnit(t *testing.T) {
	repo := writeModule(t, map[string]string{"cmd/server/main.go": covMain, "p/p.go": "package p\n\nfunc F() int {\n\treturn 1\n}\n"})
	r, err := golang.Runtime{}.Build(context.Background(), repo, svc, t.TempDir())
	if err != nil || !r.OK {
		t.Fatalf("%v %+v", err, r)
	}
	coverDir := t.TempDir()
	p, err := golang.Runtime{}.Start(context.Background(), r.Binary, nil, coverDir, filepath.Join(t.TempDir(), "l.log"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if !p.Stop(10 * time.Second) {
		t.Fatal("mong đợi thoát êm")
	}
	unit := filepath.Join(t.TempDir(), "unit.cov")
	if err := os.WriteFile(unit, []byte("mode: set\nexample.com/m/p/p.go:3.1,5.2 1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cov, err := golang.Runtime{}.Coverage(context.Background(), repo, t.TempDir(), unit, coverDir)
	if err != nil || cov.NA != "" {
		t.Fatalf("%v %+v", err, cov)
	}
	m := cov.Lines["cmd/server/main.go"]
	if !m[17] || m[21] {
		t.Errorf("hit() dòng 17 phải true, never() dòng 21 phải false: %v", m)
	}
	if !cov.Lines["p/p.go"][4] {
		t.Errorf("unit: %v", cov.Lines)
	}
	var _ lang.Coverage = cov
}

func TestCoverageEmptyCoverDirIsNA(t *testing.T) {
	repo := writeModule(t, nil2())
	cov, err := golang.Runtime{}.Coverage(context.Background(), repo, t.TempDir(), "", t.TempDir())
	if err != nil || cov.NA != "không có dữ liệu coverage (unit test và case đều không ghi)" {
		t.Fatalf("%v %+v", err, cov)
	}
}

func nil2() map[string]string { return map[string]string{} }
