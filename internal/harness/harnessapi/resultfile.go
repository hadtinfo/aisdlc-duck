package harnessapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"aisdlc/internal/fsx"
)

// ResultFile là file kết quả máy đọc của binary aisdlc-harness trong thư mục out (CLI aisdlc đọc nó sau khi
// tiến trình con thoát; results.json, schema.json, harness.log và cover/ vẫn như cũ).
const ResultFile = "harness-result.json"

// HarnessLog là tên file log của harness trong thư mục out.
const HarnessLog = "harness.log"

// PanicPrefix mở đầu thông điệp khi harness hỏng giữa chừng (panic) trước khi ghi kết quả.
const PanicPrefix = "harness lỗi: "

// Mã thoát của aisdlc-harness. `run`: ExitCode của RunResult (0 mọi case PASSED, 1 có case không PASSED, 2 lỗi
// harness, ExitTimeout quá thời gian hoặc bị hủy); `verify`: 0 khi đã ghi kết quả. Ngoài ra:
const (
	ExitPanic       = 2   // harness panic; ResultFile có Panic, stack ở cuối harness.log
	ExitError       = 3   // tham số sai, config/language lỗi, không ghi được out; ResultFile có Error nếu ghi được
	ExitInterrupted = 130 // tín hiệu thứ hai: đã SIGKILL dịch vụ và thoát ngay, không có ResultFile
)

// Result là nội dung ResultFile: đúng một trong Run (lệnh run), Verify (lệnh verify), Error hoặc Panic.
type Result struct {
	Run    *RunResult    `json:"run,omitempty"`
	Verify *VerifyResult `json:"verify,omitempty"`
	Error  string        `json:"error,omitempty"` // lỗi trả về của Run/Verify (không ghi được out, language lạ…)
	Panic  string        `json:"panic,omitempty"` // giá trị panic
	// Version là phiên bản build của aisdlc-harness (buildinfo.Version); CLI từ chối kết quả khi khác bản với nó.
	Version string `json:"version,omitempty"`
}

// WriteResult ghi out/harness-result.json.
func WriteResult(out string, r Result) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	return fsx.WriteAtomic(filepath.Join(out, ResultFile), b, 0o644)
}

// ReadResult đọc out/harness-result.json; thiếu hoặc hỏng là lỗi.
func ReadResult(out string) (Result, error) {
	var r Result
	b, err := os.ReadFile(filepath.Join(out, ResultFile))
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("%s hỏng: %w", ResultFile, err)
	}
	if r.Run == nil && r.Verify == nil && r.Error == "" && r.Panic == "" {
		return r, fmt.Errorf("%s rỗng", ResultFile)
	}
	return r, nil
}

// PanicLog: stack của panic rồi dòng "harness lỗi: <giá trị>" ở cuối, để 20 dòng cuối log vẫn có lý do.
func PanicLog(p any, stack []byte) string {
	return fmt.Sprintf("stack:\n%s%s%v", stack, PanicPrefix, p)
}

// AppendLog thêm một dòng vào out/harness.log; lỗi bị bỏ qua (log chỉ để dev xem).
func AppendLog(out, line string) {
	p := filepath.Join(out, HarnessLog)
	b, _ := os.ReadFile(p)
	_ = fsx.WriteAtomic(p, append(b, []byte(line+"\n")...), 0o644)
}
