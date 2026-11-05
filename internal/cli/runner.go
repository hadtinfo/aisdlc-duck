package cli

import (
	"os"
	"path/filepath"
	"strings"

	"aisdlc/internal/model"
	"aisdlc/internal/run"
)

// HarnessLog là tên file log của harness trong OutDir (maven.log của bản Java).
const HarnessLog = "harness.log"

// CaseRunner chạy bộ case của một tính năng (MavenRunner của Java). mode: "cases", "cases-with-drafts",
// "migration-check". Bản thật (harness Go) có ở giai đoạn 3.
type CaseRunner interface {
	RunCases(key model.FeatureKey, steps []string, mode string) (run.CaseRunResult, error)
	// OutDir là thư mục kết quả của lần chạy (.aisdlc/run/<KEY>): harness ghi results.json, schema.json ở đây.
	OutDir(key model.FeatureKey) string
	// OutputTail là n dòng cuối log của lần chạy gần nhất ("" nếu chưa có).
	OutputTail(key model.FeatureKey, n int) string
}

// defaultRunner chưa có harness: RunCases luôn dừng lệnh.
func defaultRunner(root string) CaseRunner { return pendingRunner{root: root} }

type pendingRunner struct{ root string }

func (r pendingRunner) RunCases(model.FeatureKey, []string, string) (run.CaseRunResult, error) {
	return run.CaseRunResult{}, abort(2, "Chạy case cần harness Go (giai đoạn 3).")
}

func (r pendingRunner) OutDir(key model.FeatureKey) string {
	return filepath.Join(r.root, ".aisdlc", "run", key.String())
}

func (r pendingRunner) OutputTail(key model.FeatureKey, n int) string {
	return tailFile(filepath.Join(r.OutDir(key), HarnessLog), n)
}

// tailFile là n dòng cuối của file, nối bằng "\n"; "" nếu không đọc được.
func tailFile(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
