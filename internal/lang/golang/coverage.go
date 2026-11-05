package golang

import (
	"context"

	"aisdlc/internal/lang"
)

// Coverage tạm thời chưa cài (Task 2).
func (Runtime) Coverage(ctx context.Context, repoRoot, outDir string, unitProfile, coverDir string) (lang.Coverage, error) {
	return lang.Coverage{NA: "chưa cài"}, nil
}
