package lang

import (
	"context"
	"time"

	"aisdlc/internal/model"
)

// BuildResult là kết quả build dịch vụ đích.
type BuildResult struct {
	OK     bool
	Binary string // đường dẫn tuyệt đối; "" khi !OK
	Output string // output của trình biên dịch (stdout+stderr)
}

// UnitResult là kết quả chạy unit test của repo đích.
type UnitResult struct {
	Exit    int
	Failed  []string // "<import path>.<TestName>"; package fail không có test nào fail: "<import path>"; đã sắp xếp
	Profile string   // file coverprofile; "" nếu không có
	Output  string   // output text (đã bỏ JSON), để ghi log
}

// Coverage là độ phủ dòng đo được (Task 2 cài đặt phần tính).
type Coverage struct {
	Lines map[string]map[int]bool // repo path (slash) → dòng đo được → đã chạy
	NA    string                  // khác "" thì không đo được, đây là lý do (tiếng Việt)
}

// Process là dịch vụ đích đang chạy.
type Process interface {
	Done() <-chan struct{}                      // đóng khi tiến trình thoát
	ExitErr() error                             // sau Done
	Stop(timeout time.Duration) (graceful bool) // SIGTERM nhóm, chờ timeout, quá thì SIGKILL nhóm
	Kill()                                      // SIGKILL nhóm ngay, không chờ (Ctrl-C lần hai)
}

// Runtime gom phần build, test và chạy tiến trình phụ thuộc ngôn ngữ.
// error trả về chỉ dành cho lỗi hạ tầng (ví dụ không có go trên PATH):
// build hỏng thì OK=false, test fail thì Exit != 0, error vẫn là nil.
type Runtime interface {
	Build(ctx context.Context, repoRoot string, svc model.ServiceConfig, outDir string) (BuildResult, error)
	UnitTests(ctx context.Context, repoRoot string, ut model.UnitTestConfig, outDir string) (UnitResult, error)
	Start(ctx context.Context, binary string, env map[string]string, coverDir, logPath string) (Process, error)
	Coverage(ctx context.Context, repoRoot, outDir string, unitProfile, coverDir string) (Coverage, error)
}
