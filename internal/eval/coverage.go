package eval

import (
	"sort"

	"aisdlc/internal/lang"
)

const maxNamedUnmeasured = 5

// ChangedCoverage là độ phủ của các dòng mới/sửa.
type ChangedCoverage struct {
	Covered, Measurable int
	Unmeasured          []string // file nguồn có dòng đổi mà không có trong Coverage.Lines; tối đa 5 tên + "…"
	NA                  string   // "" hoặc lý do
}

// Percent trả phần trăm đã phủ; không có dòng đo được thì coi là 100.
func (c ChangedCoverage) Percent() float64 {
	if c.Measurable == 0 {
		return 100
	}
	return 100 * float64(c.Covered) / float64(c.Measurable)
}

// ComputeChangedCoverage giao dòng đổi với dòng đo được. Chỉ file nguồn (isSource) được tính.
// strict: file nguồn không có trong cov.Lines tính mọi dòng đổi là chưa chạy và được nêu tên;
// không strict thì bỏ qua file đó.
func ComputeChangedCoverage(changed map[string]map[int]bool, cov lang.Coverage, isSource func(string) bool, strict bool) ChangedCoverage {
	var c ChangedCoverage
	if cov.NA != "" {
		c.NA = cov.NA
		return c
	}
	files := make([]string, 0, len(changed))
	for f := range changed {
		if isSource(f) {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	var unmeasured []string
	for _, f := range files {
		measured, ok := cov.Lines[f]
		if !ok {
			if strict {
				c.Measurable += len(changed[f])
				unmeasured = append(unmeasured, f)
			}
			continue
		}
		for l := range changed[f] {
			if hit, ok := measured[l]; ok {
				c.Measurable++
				if hit {
					c.Covered++
				}
			}
		}
	}
	if len(unmeasured) > maxNamedUnmeasured {
		c.Unmeasured = append(unmeasured[:maxNamedUnmeasured:maxNamedUnmeasured], "…")
	} else {
		c.Unmeasured = unmeasured
	}
	if c.Measurable == 0 {
		c.NA = "không có dòng mới/sửa nào đo được"
	}
	return c
}
