package eval

import (
	"path"
	"sort"

	"aisdlc/internal/i18n"
	"aisdlc/internal/lang"
)

const maxNamedUnmeasured = 5

// ChangedCoverage là độ phủ của các dòng mới/sửa.
type ChangedCoverage struct {
	Covered, Measurable int
	Unmeasured          []string // file nguồn có dòng đổi mà không có trong Coverage.Lines; tối đa 5 tên + "…"
	UnmeasuredCount     int      // tổng số file chưa đo (để báo "(+N)" như Java)
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
// strict: file nguồn có dòng đổi, không có trong cov.Lines và nằm ở package (thư mục) không có file nào được đo thì
// mọi dòng đổi tính là chưa chạy và được nêu tên (như module không có báo cáo JaCoCo của Java). File vắng mặt mà
// package của nó có đo (chỉ có kiểu/hằng, không có block coverage) thì bỏ qua. Không strict thì bỏ qua mọi file
// không đo.
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
	measuredDirs := map[string]bool{}
	for f := range cov.Lines {
		measuredDirs[path.Dir(f)] = true
	}
	var unmeasured []string
	for _, f := range files {
		measured, ok := cov.Lines[f]
		if !ok {
			if strict && len(changed[f]) > 0 && !measuredDirs[path.Dir(f)] {
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
	c.UnmeasuredCount = len(unmeasured)
	if len(unmeasured) > maxNamedUnmeasured {
		c.Unmeasured = append(unmeasured[:maxNamedUnmeasured:maxNamedUnmeasured], "…")
	} else {
		c.Unmeasured = unmeasured
	}
	if c.Measurable == 0 {
		c.NA = i18n.T("không có dòng mới/sửa nào đo được")
	}
	return c
}
