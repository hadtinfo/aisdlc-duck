package harnessapi

import (
	"math"
	"strconv"

	"aisdlc/internal/i18n"
	"aisdlc/internal/lang"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
)

// ExitTimeout là ExitCode khi harness quá timeoutMinutes hoặc bị hủy (thông điệp case phân biệt hai trường hợp).
const ExitTimeout = 124

// Options là tham số một lần chạy case.
type Options struct {
	Feature model.FeatureKey
	Steps   []string
	Mode    string
	Out     string
}

// RunResult: ExitCode 0 khi mọi case PASSED; 1 khi có case không PASSED; 2 khi lỗi harness (hạ tầng, build,
// dịch vụ không khởi động, cases.yaml hỏng); ExitTimeout khi quá thời gian hoặc bị hủy.
type RunResult struct {
	ExitCode int
	Cases    []run.CaseResult
}

// VerifyResult là kết quả build, unit test, chạy case và coverage của một lần eval.
type VerifyResult struct {
	Build      lang.BuildResult
	Unit       lang.UnitResult
	Run        RunResult     // mode cases, mọi bước
	HarnessErr string        // "" hoặc lỗi hạ tầng/khởi động (tiếng Việt)
	Coverage   lang.Coverage // NA có lý do khi dịch vụ không tắt êm
	TimedOut   bool
}

// Canceled là thông điệp (theo locale) của case khi harness bị hủy (Ctrl-C/SIGTERM) chứ không phải quá thời gian.
func Canceled() string { return i18n.T("Harness bị hủy (Ctrl-C), đã dừng") }

// TimeoutMessage là thông điệp của case khi harness bị dừng vì quá timeoutMinutes.
func TimeoutMessage(minutes float64) string {
	return i18n.Tf("Harness chạy quá thời gian (%s phút), đã dừng", fmtMinutes(minutes))
}

// fmtMinutes in như MavenRunner.minutes: số nguyên thì không có phần thập phân.
func fmtMinutes(m float64) string {
	if m == math.Trunc(m) {
		return strconv.FormatInt(int64(m), 10)
	}
	return strconv.FormatFloat(m, 'f', -1, 64)
}
