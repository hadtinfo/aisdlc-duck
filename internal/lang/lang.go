// Package lang định nghĩa adapter ngôn ngữ; bản cài đặt nằm ở các gói con.
package lang

import "context"

// DependencyChanges là kết quả so dependency giữa base và working tree.
type DependencyChanges struct {
	Added []string // đã sắp xếp; "module/path" hoặc "replace a => b"
	NA    string   // khác "" thì không so được, đây là lý do (tiếng Việt)
}

// Adapter gom phần phụ thuộc ngôn ngữ của repo đích.
type Adapter interface {
	Name() string
	IsSource(repoPath string) bool
	SampleFileName() string // tên file mẫu cho ScopeFlags
	MigrationVersion(fileName string) (int64, bool)
	MigrationOrder(paths []string) []string // bản sao đã sắp xếp
	DependencyChanges(ctx context.Context, base, now FileSource) DependencyChanges
}
