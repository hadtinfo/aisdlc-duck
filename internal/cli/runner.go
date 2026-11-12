package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"aisdlc/internal/eval"
	"aisdlc/internal/harness/harnessapi"
	"aisdlc/internal/i18n"
	"aisdlc/internal/lang"
	"aisdlc/internal/lang/langs"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
)

// HarnessLog là tên file log của harness trong OutDir (maven.log của bản Java).
const HarnessLog = harnessapi.HarnessLog

// harnessPanic mở đầu thông điệp khi harness hỏng giữa chừng (panic) trước khi ghi kết quả.
const harnessPanic = harnessapi.PanicPrefix

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

// newHarnessRunner là runner thật: binary aisdlc-harness (tiến trình con) theo language của config. std là ctx của
// lệnh (Ctrl-C hủy nó).
func newHarnessRunner(std context.Context, root string, cfg model.RepoConfig) CaseRunner {
	return &harnessCaseRunner{std: std, root: root, err: checkLanguage(cfg), find: findHarness}
}

// checkLanguage: language không hỗ trợ thì lỗi ngay ở CLI như khi harness còn chạy trong tiến trình.
func checkLanguage(cfg model.RepoConfig) error {
	if _, err := langs.For(cfg.Language); err != nil {
		return err
	}
	_, err := langs.RuntimeFor(cfg.Language)
	return err
}

type harnessCaseRunner struct {
	std  context.Context
	root string
	err  error                  // language không hỗ trợ
	find func() (string, error) // tìm binary aisdlc-harness
}

// RunCases chạy `aisdlc-harness run` vào OutDir. Harness hỏng giữa chừng (panic, chết không ghi kết quả) thì không
// bao giờ dùng kết quả cũ: results.json/schema.json cũ đã bị xóa trước khi chạy, case không có kết quả là ERROR
// NoResults, exit 2. Không tìm thấy binary thì mọi case ERROR với lý do (fail closed).
func (r *harnessCaseRunner) RunCases(key model.FeatureKey, steps []string, mode string) (run.CaseRunResult, error) {
	m, err := harnessapi.ParseMode(mode)
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
	bin, err := r.find()
	if err != nil {
		appendLog(out, err.Error())
		return run.CaseRunResult{ExitCode: 2, Cases: errorCases(r.selected(key, steps, m), err.Error())}, nil
	}
	canceled := run.CaseRunResult{ExitCode: harnessapi.ExitTimeout,
		Cases: errorCases(r.selected(key, steps, m), harnessapi.CanceledMessage)}
	if r.std.Err() != nil {
		return canceled, nil // đã hủy trước khi chạy: như harness gặp ctx đã hủy
	}
	res, err := execHarness(r.std, bin, out, "run", "--root", r.root, "--feature", key.String(),
		"--steps", strings.Join(steps, ","), "--mode", m, "--out", out)
	var verr *harnessVersionError
	switch {
	case errors.As(err, &verr):
		return run.CaseRunResult{ExitCode: 2, Cases: errorCases(r.selected(key, steps, m), verr.msg)}, nil
	case err != nil && r.std.Err() != nil:
		return canceled, nil
	case err != nil:
		appendLog(out, harnessPanic+err.Error())
		return r.crashed(key, steps, m, out), nil
	case res.Error != "":
		return run.CaseRunResult{}, errors.New(res.Error)
	case res.Panic != "" || res.Run == nil:
		return r.crashed(key, steps, m, out), nil
	}
	return run.CaseRunResult{ExitCode: res.Run.ExitCode, Cases: res.Run.Cases}, nil
}

// crashed: kết quả khi harness hỏng — đọc results.json harness để lại như MavenRunner (không có thì NoResults).
func (r *harnessCaseRunner) crashed(key model.FeatureKey, steps []string, mode, out string) run.CaseRunResult {
	return run.CaseRunResult{ExitCode: 2, Cases: harnessapi.ReadResults(out, r.selected(key, steps, mode))}
}

// selected là các case được chọn (migration-check không chạy case); cases.yaml hỏng thì rỗng.
func (r *harnessCaseRunner) selected(key model.FeatureKey, steps []string, mode string) []spec.CaseSpec {
	if mode == harnessapi.ModeMigrationCheck {
		return nil
	}
	paths := model.FeaturePaths{RepoRoot: r.root, Key: key}
	cs, err := spec.ReadCases(paths.Cases())
	if err != nil {
		return nil
	}
	return harnessapi.Select(cs.Cases, steps)
}

func errorCases(selected []spec.CaseSpec, msg string) []run.CaseResult {
	out := make([]run.CaseResult, 0, len(selected))
	for _, c := range selected {
		out = append(out, run.CaseResult{ID: c.ID, Step: c.Step, Outcome: run.CaseOutcomeError, Message: msg})
	}
	return out
}

func (r *harnessCaseRunner) OutDir(key model.FeatureKey) string {
	return filepath.Join(r.root, ".aisdlc", "run", key.String())
}

func (r *harnessCaseRunner) OutputTail(key model.FeatureKey, n int) string {
	return tailFile(filepath.Join(r.OutDir(key), HarnessLog), n)
}

// harnessNoVerify: harness-result.json không có kết quả verify (cũng không panic hay lỗi).
func harnessNoVerify() string { return i18n.T("aisdlc-harness không trả kết quả verify") }

// harnessVerifier là eval.Verifier thật (`aisdlc-harness verify`). Harness hỏng giữa chừng hoặc không tìm thấy binary
// thành HarnessErr (không phải build hỏng: Build.OK=true để BUILD_AND_TESTS chỉ báo lỗi harness), lý do ghi vào
// harness.log; results.json/schema.json cũ không còn để eval đọc nhầm.
type harnessVerifier struct {
	root string
	err  error // language không hỗ trợ
	find func() (string, error)
}

func newHarnessVerifier(root string, cfg model.RepoConfig) eval.Verifier {
	return harnessVerifier{root: root, err: checkLanguage(cfg), find: findHarness}
}

func (v harnessVerifier) Verify(ctx context.Context, key model.FeatureKey, out string) (harnessapi.VerifyResult, error) {
	if v.err != nil {
		return harnessapi.VerifyResult{}, v.err
	}
	if err := clearResults(out); err != nil {
		return harnessapi.VerifyResult{}, err
	}
	bin, err := v.find()
	if err != nil {
		appendLog(out, err.Error())
		return failedVerify(err.Error(), err.Error()), nil
	}
	if ctx.Err() != nil {
		return canceledVerify(), nil
	}
	res, err := execHarness(ctx, bin, out, "verify", "--root", v.root, "--feature", key.String(), "--out", out)
	var verr *harnessVersionError
	switch {
	case errors.As(err, &verr):
		return failedVerify(verr.msg, verr.msg), nil
	case err != nil && ctx.Err() != nil:
		return canceledVerify(), nil
	case err != nil:
		appendLog(out, harnessPanic+err.Error())
		return failedVerify(err.Error(), harnessPanic+err.Error()), nil
	case res.Error != "":
		return harnessapi.VerifyResult{}, errors.New(res.Error)
	case res.Panic != "":
		return failedVerify(res.Panic, harnessPanic+res.Panic), nil
	case res.Verify == nil:
		appendLog(out, harnessNoVerify())
		return failedVerify(harnessNoVerify(), harnessPanic+harnessNoVerify()), nil
	}
	return *res.Verify, nil
}

// failedVerify: harness không chạy xong (panic, không tìm thấy binary): lỗi harness, exit 2, coverage N/A.
func failedVerify(harnessErr, na string) harnessapi.VerifyResult {
	return harnessapi.VerifyResult{Build: lang.BuildResult{OK: true}, HarnessErr: harnessErr,
		Run: harnessapi.RunResult{ExitCode: 2}, Coverage: lang.Coverage{NA: na}}
}

// canceledVerify: bị hủy trước khi harness ghi kết quả.
func canceledVerify() harnessapi.VerifyResult {
	return harnessapi.VerifyResult{Build: lang.BuildResult{OK: true}, Run: harnessapi.RunResult{ExitCode: harnessapi.ExitTimeout},
		Coverage: lang.Coverage{NA: harnessapi.CanceledMessage}, TimedOut: true}
}

// clearResults xóa results.json, schema.json và harness-result.json cũ trong out trước khi harness chạy (harness cũng
// xóa cả out khi mở; đây là chốt chặn nếu harness hỏng trước đó).
func clearResults(out string) error {
	for _, n := range []string{"results.json", "schema.json", harnessapi.ResultFile} {
		if err := os.Remove(filepath.Join(out, n)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// appendLog thêm một dòng vào out/harness.log; lỗi bị bỏ qua (log chỉ để dev xem).
func appendLog(out, line string) { harnessapi.AppendLog(out, line) }

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
