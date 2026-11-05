package golang

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var migrationRe = regexp.MustCompile(`^(\d+)_.+\.(sql|go)$`)

// MigrationVersion lấy version goose từ tên file; quá int64 thì MaxInt64.
func (Adapter) MigrationVersion(fileName string) (int64, bool) {
	name := fileName[strings.LastIndex(fileName, "/")+1:]
	m := migrationRe.FindStringSubmatch(name)
	if m == nil || strings.HasSuffix(name, "_test.go") {
		return 0, false
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return math.MaxInt64, true
	}
	return v, true
}

// MigrationOrder: có version trước (tăng dần), còn lại theo tên.
func (a Adapter) MigrationOrder(paths []string) []string {
	out := append([]string(nil), paths...)
	sort.SliceStable(out, func(i, j int) bool {
		vi, oi := a.MigrationVersion(out[i])
		vj, oj := a.MigrationVersion(out[j])
		if oi != oj {
			return oi
		}
		if oi && vi != vj {
			return vi < vj
		}
		return out[i] < out[j]
	})
	return out
}
