package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aisdlc/internal/eval"
	"aisdlc/internal/fsx"
	"aisdlc/internal/harness"
	"aisdlc/internal/lang/langs"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
)

// HarnessLog là tên file log của harness trong OutDir (maven.log của bản Java).
const HarnessLog = "harness.log"

// harnessPanic mở đầu thông điệp khi harness hỏng giữa chừng (panic) trước khi ghi kết quả.
const harnessPanic = "harness lỗi: "

// CaseRunner chạy bộ case của một tính năng (MavenRunner của Java). mode: "cases", "cases-with-drafts",
// "migration-check". Lỗi hạ tầng hay dịch vụ trả thành case ERROR; error chỉ khi mode sai (*Abort) hoặc không
// ghi được OutDir.
type CaseRunner interface {
	RunCases(key model.FeatureKey, steps []string, mode string) (run.CaseRunResult, error)
	// OutDir là thư mục kết quả của lần chạy (.aisdlc/run/<KEY>): harness ghi results.json, schema.json ở đây.
	OutDir(key model.FeatureKey) string
	// OutputTail là n dòng cuối log của lần chạy gần nhất ("" nếu chưa có).
	OutputTail(key model.FeatureKey, n int) string
}

// newHarnessRunner là runner thật: harness Go trong tiến trình, theo language của config. std là ctx của lệnh
// (Ctrl-C hủy nó); now là đồng hồ của Context.
func newHarnessRunner(std context.Context, root string, cfg model.RepoConfig, now func() time.Time) CaseRunner {
	h, err := newHarness(root, cfg, now)
	return &harnessCaseRunner{std: std, root: root, h: h, err: err}
}

// newHarness dựng harness.Harness với adapter và runtime của language (hạ tầng mặc định: testcontainers).
func newHarness(root string, cfg model.RepoConfig, now func() time.Time) (*harness.Harness, error) {
	a, err := langs.For(cfg.Language)
	if err != nil {
		return nil, err
	}
	rt, err := langs.RuntimeFor(cfg.Language)
	if err != nil {
		return nil, err
	}
	return &harness.Harness{Root: root, Config: cfg, Adapter: a, Runtime: rt, Now: now}, nil
}

type harnessCaseRunner struct {
	std  context.Context
	root string
	h    *harness.Harness
	err  error // language không hỗ trợ
}

// RunCases chạy harness.Run vào OutDir. Harness hỏng giữa chừng (panic) thì không bao giờ dùng kết quả cũ:
// results.json/schema.json cũ đã bị xóa trước khi chạy, case không có kết quả là ERROR NoResults, exit 2.
func (r *harnessCaseRunner) RunCases(key model.FeatureKey, steps []string, mode string) (res run.CaseRunResult, err error) {
	m, err := harness.ParseMode(mode)
	if err != nil {
		return run.CaseRunResult{}, abort(2, err.Error())
	}
	if r.err != nil {
		return run.CaseRunResult{}, r.err
	}
	out := r.OutDir(key)
	if err := clearResults(out); err != nil {
		return run.CaseRunResult{}, err
	}
	defer func() {
		if p := recover(); p != nil {
			res, err = r.crashed(key, steps, m, out, p), nil
		}
	}()
	rr, err := r.h.Run(r.std, harness.Options{Feature: key, Steps: steps, Mode: m, Out: out})
	if err != nil {
		return run.CaseRunResult{}, err
	}
	return run.CaseRunResult{ExitCode: rr.ExitCode, Cases: rr.Cases}, nil
}

// crashed: kết quả khi harness panic — đọc results.json harness để lại như MavenRunner (không có thì NoResults).
func (r *harnessCaseRunner) crashed(key model.FeatureKey, steps []string, mode, out string, p any) run.CaseRunResult {
	appendLog(out, fmt.Sprintf("%s%v", harnessPanic, p))
	var selected []spec.CaseSpec
	if mode != harness.ModeMigrationCheck {
		paths := model.FeaturePaths{RepoRoot: r.root, Key: key}
		if cs, err := spec.ReadCases(paths.Cases()); err == nil {
			selected = harness.Select(cs.Cases, steps)
		}
	}
	return run.CaseRunResult{ExitCode: 2, Cases: harness.ReadResults(out, selected)}
}

func (r *harnessCaseRunner) OutDir(key model.FeatureKey) string {
	return filepath.Join(r.root, ".aisdlc", "run", key.String())
}

func (r *harnessCaseRunner) OutputTail(key model.FeatureKey, n int) string {
	return tailFile(filepath.Join(r.OutDir(key), HarnessLog), n)
}

// harnessVerifier là eval.Verifier thật (harness.Verify). Panic giữa chừng thành HarnessErr (build không đạt) và
// results.json/schema.json cũ không còn để eval đọc nhầm.
type harnessVerifier struct {
	h   *harness.Harness
	err error // language không hỗ trợ
}

func newHarnessVerifier(root string, cfg model.RepoConfig, now func() time.Time) eval.Verifier {
	h, err := newHarness(root, cfg, now)
	return harnessVerifier{h: h, err: err}
}

func (v harnessVerifier) Verify(ctx context.Context, key model.FeatureKey, out string) (vr harness.VerifyResult, err error) {
	if v.err != nil {
		return harness.VerifyResult{}, v.err
	}
	if err := clearResults(out); err != nil {
		return harness.VerifyResult{}, err
	}
	defer func() {
		if p := recover(); p != nil {
			msg := fmt.Sprintf("%s%v", harnessPanic, p)
			appendLog(out, msg)
			vr, err = harness.VerifyResult{HarnessErr: msg, Run: harness.RunResult{ExitCode: 2}}, nil
		}
	}()
	return v.h.Verify(ctx, key, out)
}

// clearResults xóa results.json và schema.json cũ trong out trước khi harness chạy (harness cũng xóa cả out khi
// mở; đây là chốt chặn nếu harness hỏng trước đó).
func clearResults(out string) error {
	for _, n := range []string{"results.json", "schema.json"} {
		if err := os.Remove(filepath.Join(out, n)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// appendLog thêm một dòng vào out/harness.log; lỗi bị bỏ qua (log chỉ để dev xem).
func appendLog(out, line string) {
	p := filepath.Join(out, HarnessLog)
	b, _ := os.ReadFile(p)
	_ = fsx.WriteAtomic(p, append(b, []byte(line+"\n")...), 0o644)
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
