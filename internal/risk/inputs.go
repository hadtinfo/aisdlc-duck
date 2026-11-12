package risk

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"aisdlc/internal/model"
)

// InputFiles là các file subagent aisdlc-risk-classifier đọc để xếp loại, sắp theo đường dẫn. Luồng đầy đủ: brd.md,
// design.md, design.yaml và các file trong db/ (migration nháp, chọn như hash cổng DESIGN: model.DbDraftFiles);
// luồng nhẹ: feature.yaml (tiêu đề) và cases.yaml.
// File chưa có thì bỏ qua. Rel tính từ root, dùng dấu "/".
func InputFiles(root string, paths model.FeaturePaths, flow model.Flow) ([]InputFile, error) {
	var abs []string
	if flow == model.FlowLite {
		abs = []string{paths.Meta(), paths.Cases()}
	} else {
		abs = []string{paths.Brd(), paths.DesignMd(), paths.DesignYaml()}
		abs = append(abs, model.DbDraftFiles(paths.DbDrafts())...)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var out []InputFile
	for _, p := range abs {
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("Không đọc được %s: %v", p, err)
		}
		pa, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(rootAbs, pa)
		if err != nil {
			return nil, err
		}
		out = append(out, InputFile{Rel: strings.ReplaceAll(filepath.ToSlash(rel), "\\", "/"), Content: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}
