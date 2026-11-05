package eval_test

import (
	"reflect"
	"testing"

	"aisdlc/internal/eval"
	"aisdlc/internal/lang"
	"aisdlc/internal/lang/golang"
)

var isSrc = golang.Adapter{}.IsSource

func cov() lang.Coverage {
	return lang.Coverage{Lines: map[string]map[int]bool{"limit/limit.go": {5: true, 6: true, 7: false, 9: true}}}
}

func TestChangedCoverageParsesLinesPerFile(t *testing.T) {
	c := eval.ComputeChangedCoverage(map[string]map[int]bool{"limit/limit.go": {5: true, 6: true, 7: true, 9: true}}, cov(), isSrc, false)
	if c.Covered != 3 || c.Measurable != 4 || c.NA != "" {
		t.Fatalf("%+v", c)
	}
}

func TestChangedCoverageMapsChangedLines(t *testing.T) {
	// 8 không đo được nên không tính: 2/3
	c := eval.ComputeChangedCoverage(map[string]map[int]bool{"limit/limit.go": {5: true, 6: true, 7: true, 8: true}}, cov(), isSrc, false)
	if c.Covered != 2 || c.Measurable != 3 {
		t.Fatalf("%+v", c)
	}
	if p := c.Percent(); int(p+0.5) != 67 {
		t.Errorf("percent %v", p)
	}
	if (eval.ChangedCoverage{}).Percent() != 100 {
		t.Error("Measurable==0 phải là 100")
	}
}

func TestChangedCoverageOnlySourceFilesCount(t *testing.T) {
	changed := map[string]map[int]bool{
		"limit/limit.go":      {5: true, 7: true, 9: true},
		"limit/limit_test.go": {1: true, 2: true, 3: true},
		"vendor/x/x.go":       {1: true},
		"a/testdata/t.go":     {1: true},
		"README.md":           {1: true},
	}
	c := eval.ComputeChangedCoverage(changed, cov(), isSrc, true)
	if c.Covered != 2 || c.Measurable != 3 || len(c.Unmeasured) != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestChangedCoverageStrictUnmeasuredFilesNamedUpToFive(t *testing.T) {
	changed := map[string]map[int]bool{"limit/limit.go": {5: true, 6: true, 7: true, 9: true}}
	for _, n := range []string{"u1", "u2", "u3", "u4", "u5", "u6"} {
		changed["m/"+n+".go"] = map[int]bool{1: true, 2: true}
	}
	c := eval.ComputeChangedCoverage(changed, cov(), isSrc, true)
	if c.Covered != 3 || c.Measurable != 16 {
		t.Fatalf("%+v", c)
	}
	want := []string{"m/u1.go", "m/u2.go", "m/u3.go", "m/u4.go", "m/u5.go", "…"}
	if !reflect.DeepEqual(c.Unmeasured, want) {
		t.Errorf("unmeasured %v", c.Unmeasured)
	}
}

func TestChangedCoverageSoftIgnoresUnmeasured(t *testing.T) {
	changed := map[string]map[int]bool{"limit/limit.go": {5: true, 6: true, 7: true, 9: true}, "m/u1.go": {1: true, 2: true}}
	c := eval.ComputeChangedCoverage(changed, cov(), isSrc, false)
	if c.Covered != 3 || c.Measurable != 4 || len(c.Unmeasured) != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestChangedCoverageNoMeasurableIsNA(t *testing.T) {
	c := eval.ComputeChangedCoverage(map[string]map[int]bool{"limit/limit.go": {8: true}}, cov(), isSrc, false)
	if c.NA != "không có dòng mới/sửa nào đo được" || c.Percent() != 100 {
		t.Fatalf("%+v", c)
	}
	c = eval.ComputeChangedCoverage(map[string]map[int]bool{"limit/limit.go": {5: true}}, lang.Coverage{NA: "lý do"}, isSrc, true)
	if c.NA != "lý do" {
		t.Fatalf("%+v", c)
	}
}

func TestChangedCoverageStrictIgnoresBlocklessFileInMeasuredPackage(t *testing.T) {
	// limit/types.go chỉ có kiểu/hằng nên không có block coverage, nhưng package limit có đo: bỏ qua (như Java bỏ
	// interface/package-info trong module có báo cáo). m/empty.go không có dòng đổi: bỏ qua.
	changed := map[string]map[int]bool{
		"limit/limit.go": {5: true, 6: true, 7: true, 9: true},
		"limit/types.go": {1: true, 2: true, 3: true},
		"m/empty.go":     {},
	}
	c := eval.ComputeChangedCoverage(changed, cov(), isSrc, true)
	if c.Covered != 3 || c.Measurable != 4 || len(c.Unmeasured) != 0 || c.UnmeasuredCount != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestChangedCoverageStrictCountsAllUnmeasured(t *testing.T) {
	changed := map[string]map[int]bool{}
	for _, n := range []string{"u1", "u2", "u3", "u4", "u5", "u6", "u7"} {
		changed["m/"+n+".go"] = map[int]bool{1: true}
	}
	c := eval.ComputeChangedCoverage(changed, cov(), isSrc, true)
	if c.UnmeasuredCount != 7 || len(c.Unmeasured) != 6 {
		t.Fatalf("%+v", c)
	}
}
