package review

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"aisdlc/internal/lang/golang"
	"aisdlc/internal/model"
	"aisdlc/internal/spec"
)

var testSettings = model.ReviewSettings{
	ScopeMaxFiles: 30, BudgetMaxFiles: 10, BudgetMaxLines: 400,
	MigrationDirs: []string{"db/migrations"},
	ValueFields:   []string{"amount", "limit", "balance", "fee", "rate", "max", "min"},
}

var sample = golang.Adapter{}.SampleFileName()

func mk(id string, files, lines int, scope ...string) spec.StepSpec {
	return spec.StepSpec{ID: id, Title: "t", Scope: scope, Budget: &spec.Budget{Files: files, Lines: lines}}
}

func flagsFor(steps []spec.StepSpec, files []string) []ReviewFlag {
	return ScopeFlags(steps, files, testSettings, sample)
}

func find(fs []ReviewFlag, code string) *ReviewFlag {
	for i := range fs {
		if fs[i].Code == code {
			return &fs[i]
		}
	}
	return nil
}

func codes(fs []ReviewFlag) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, f.Code)
	}
	slices.Sort(out)
	return out
}

func TestPay1OverlapAndOldMigration(t *testing.T) {
	steps := []spec.StepSpec{
		mk("S1", 1, 40, "internal/**/limit/web/**"),
		mk("S2", 3, 80, "internal/**/limit/**", "db/migrations/**"),
	}
	files := []string{"internal/fixture/app.go", "db/migrations/00001_init.sql", "go.mod"}
	fs := flagsFor(steps, files)
	eq(t, "codes", codes(fs), []string{"SCOPE_MATCHES_OLD_MIGRATION", "SCOPE_OVERLAP"})
	o := find(fs, "SCOPE_OVERLAP")
	eq(t, "level", o.Level, HIGH)
	eq(t, "refs", o.Refs, []string{"S1", "S2", "internal/**/limit/web/**"})
	if !strings.Contains(o.Message, "limit/web") {
		t.Errorf("message %q", o.Message)
	}
	m := find(fs, "SCOPE_MATCHES_OLD_MIGRATION")
	eq(t, "level", m.Level, WARN)
	eq(t, "refs", m.Refs, []string{"S2", "db/migrations/00001_init.sql"})
}

func TestFix2LiteNoFlags(t *testing.T) {
	eq(t, "len", len(flagsFor([]spec.StepSpec{mk("S1", 1, 40, "internal/**/fee/**")}, nil)), 0)
}

func TestTooWide(t *testing.T) {
	if find(flagsFor([]spec.StepSpec{mk("S1", 1, 40, "**")}, nil), "SCOPE_TOO_WIDE") == nil {
		t.Error("** phải rộng")
	}
	if find(flagsFor([]spec.StepSpec{mk("S1", 1, 40, "src/**")}, nil), "SCOPE_TOO_WIDE") == nil {
		t.Error("src/** phải rộng")
	}
	var files []string
	for i := 0; i < 31; i++ {
		files = append(files, fmt.Sprintf("a/f%d.go", i))
	}
	if find(flagsFor([]spec.StepSpec{mk("S1", 1, 40, "a/**")}, files), "SCOPE_TOO_WIDE") == nil {
		t.Error("31 file phải rộng")
	}
	if find(flagsFor([]spec.StepSpec{mk("S1", 1, 40, "a/**")}, files[:30]), "SCOPE_TOO_WIDE") != nil {
		t.Error("30 file không rộng")
	}
}

func TestOddGlobsDoNotPanic(t *testing.T) {
	flagsFor([]spec.StepSpec{mk("S1", 1, 1, "{a,b}/**"), mk("S2", 1, 1, "*.go"), mk("S3", 1, 1, "[unclosed")},
		[]string{"a/x.go", "B.go"})
	bad := flagsFor([]spec.StepSpec{mk("S3", 1, 1, "[unclosed", "a/**")}, []string{"a/x"})
	eq(t, "len", len(bad), 1)
	eq(t, "code", bad[0].Code, "SCOPE_BAD_GLOB")
	eq(t, "level", bad[0].Level, NA)
	eq(t, "refs", bad[0].Refs, []string{"S3", "[unclosed"})
	fs := flagsFor([]spec.StepSpec{mk("S1", 1, 1)}, []string{"a"})
	eq(t, "len", len(fs), 1)
	eq(t, "level", fs[0].Level, NA)
	if !strings.Contains(fs[0].Message, "bước không có scope") {
		t.Errorf("message %q", fs[0].Message)
	}
}

func TestBudgetHigh(t *testing.T) {
	if find(flagsFor([]spec.StepSpec{mk("S1", 11, 40, "x/**")}, nil), "BUDGET_HIGH") == nil {
		t.Error("11 file")
	}
	if find(flagsFor([]spec.StepSpec{mk("S1", 5, 401, "x/**")}, nil), "BUDGET_HIGH") == nil {
		t.Error("401 dòng")
	}
	eq(t, "len", len(flagsFor([]spec.StepSpec{mk("S1", 10, 400, "x/**")}, nil)), 0)
}

func TestOverlapRefsPutStepsInNaturalOrder(t *testing.T) {
	steps := []spec.StepSpec{
		mk("S2", 1, 40, "internal/**/a/**"),
		mk("S10", 1, 40, "internal/**/a/**"),
	}
	o := find(flagsFor(steps, nil), "SCOPE_OVERLAP")
	i2, i10 := slices.Index(o.Refs, "S2"), slices.Index(o.Refs, "S10")
	if i2 < 0 || i2 >= i10 {
		t.Errorf("refs %v", o.Refs)
	}
}

func TestSampleUsesAdapterFileName(t *testing.T) {
	eq(t, "tail", SampleFor("a/**", "aisdlcx.go"), "a/aisdlcx/aisdlcx.go")
	eq(t, "mid", SampleFor("a/**/b/*.x?", "f"), "a/aisdlcx/b/aisdlcx.xx")
	eq(t, "valid", ValidGlob("[unclosed"), false)
	eq(t, "valid2", ValidGlob("a/**"), true)
}
