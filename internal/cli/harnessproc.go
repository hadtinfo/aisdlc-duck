package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"aisdlc/internal/buildinfo"
	"aisdlc/internal/harness/harnessapi"
	"aisdlc/internal/i18n"
)

const (
	harnessBin = "aisdlc-harness"
	// harnessStopWait: sau Ctrl-C, chờ harness dọn dịch vụ và container (harness tự giới hạn dọn 30 giây) trước khi
	// coi như tín hiệu thứ hai.
	harnessStopWait = 45 * time.Second
	// harnessKillWait: sau tín hiệu thứ hai, chờ harness SIGKILL dịch vụ và thoát trước khi SIGKILL nhóm của nó.
	harnessKillWait = 3 * time.Second
)

// cliVersion là phiên bản build của aisdlc (thay được trong test).
var cliVersion = buildinfo.Version

// findHarness tìm binary aisdlc-harness của tiến trình đang chạy (xem locateHarness).
func findHarness() (string, error) { return locateHarness(os.Executable, exec.LookPath) }

// locateHarness: cạnh binary aisdlc (và thư mục đích nếu nó là symlink): aisdlc-harness hoặc
// aisdlc-harness-<os>-<arch> theo bố cục plugin/lib; chỉ khi không có cạnh aisdlc mới tìm PATH. Không có biến môi
// trường nào đổi được binary (agent không trỏ được tới harness giả); test tiêm qua exe/lookPath.
func locateHarness(exe func() (string, error), lookPath func(string) (string, error)) (string, error) {
	names := []string{harnessBin, harnessBin + "-" + runtime.GOOS + "-" + runtime.GOARCH}
	var dirs []string
	if e, err := exe(); err == nil {
		dirs = append(dirs, filepath.Dir(e))
		if real, err := filepath.EvalSymlinks(e); err == nil && filepath.Dir(real) != dirs[0] {
			dirs = append(dirs, filepath.Dir(real))
		}
	}
	for _, d := range dirs {
		for _, n := range names {
			if p := filepath.Join(d, n); executable(p) {
				return p, nil
			}
		}
	}
	if p, err := lookPath(harnessBin); err == nil {
		return p, nil
	}
	return "", errors.New(i18n.Tf("không tìm thấy aisdlc-harness: không có cạnh aisdlc (%s) và không có trên PATH. Cài lại plugin (plugin/install.sh) hoặc `make build`", strings.Join(dirs, ", ")))
}

func executable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0
}

// liveHarness là các tiến trình aisdlc-harness đang chạy, để Ctrl-C lần hai diệt được dịch vụ của chúng.
var liveHarness = struct {
	sync.Mutex
	procs map[*os.Process]chan struct{} // → đóng khi tiến trình đã thoát
}{procs: map[*os.Process]chan struct{}{}}

// KillHarness dừng ngay (đồng bộ) mọi aisdlc-harness đang chạy: gửi thêm SIGINT (với harness đó là tín hiệu thứ hai:
// SIGKILL dịch vụ rồi thoát), chờ ngắn, còn sống thì SIGKILL nhóm tiến trình của nó.
func KillHarness() {
	liveHarness.Lock()
	procs := make(map[*os.Process]chan struct{}, len(liveHarness.procs))
	for p, done := range liveHarness.procs {
		procs[p] = done
	}
	liveHarness.Unlock()
	var wg sync.WaitGroup
	for p, done := range procs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			killChild(p, done)
		}()
	}
	wg.Wait()
}

func killChild(p *os.Process, done <-chan struct{}) {
	_ = p.Signal(os.Interrupt)
	select {
	case <-done:
		return
	case <-time.After(harnessKillWait):
	}
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
	_ = p.Kill()
}

// execHarness chạy aisdlc-harness với args rồi đọc out/harness-result.json. Tiến trình con ở nhóm riêng (Ctrl-C ở
// terminal chỉ tới aisdlc; aisdlc chuyển tiếp): ctx hủy thì gửi SIGINT (harness dừng dịch vụ, dọn container), quá
// harnessStopWait thì dừng như tín hiệu thứ hai. error khi không chạy được hoặc không có kết quả (kèm stderr).
func execHarness(ctx context.Context, bin, out string, args ...string) (harnessapi.Result, error) {
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- bin là aisdlc-harness do CLI tự định vị, không phải đầu vào người dùng
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "AISDLC_LOCALE="+string(i18n.Default()))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		return harnessapi.Result{}, fmt.Errorf(i18n.T("không chạy được %s: %w"), bin, err)
	}
	exited := make(chan struct{})
	liveHarness.Lock()
	liveHarness.procs[cmd.Process] = exited
	liveHarness.Unlock()
	waitErr := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		close(exited)
		waitErr <- err
	}()
	var err error
	select {
	case err = <-waitErr:
	case <-ctx.Done():
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case err = <-waitErr:
		case <-time.After(harnessStopWait):
			killChild(cmd.Process, exited)
			err = <-waitErr
		}
	}
	liveHarness.Lock()
	delete(liveHarness.procs, cmd.Process)
	liveHarness.Unlock()

	logHarness(out, bin, output.String())
	r, rerr := harnessapi.ReadResult(out)
	if rerr == nil {
		if cli := cliVersion(); !buildinfo.Compatible(cli, r.Version) {
			_ = clearResults(out)
			msg := versionMismatch(cli, r.Version)
			appendLog(out, msg)
			return harnessapi.Result{}, &harnessVersionError{msg}
		}
		return r, nil
	}
	why := i18n.T("thoát bình thường")
	if err != nil {
		why = err.Error()
	}
	msg := fmt.Sprintf(i18n.T("aisdlc-harness không ghi kết quả (%s)"), why)
	if l := harnessErrorLine(output.String()); l != "" {
		msg += ": " + l
	}
	return harnessapi.Result{}, errors.New(msg)
}

// harnessVersionError: aisdlc-harness khác bản build với aisdlc; kết quả của nó không được dùng.
type harnessVersionError struct{ msg string }

func (e *harnessVersionError) Error() string { return e.msg }

func versionMismatch(cli, harness string) string {
	if harness == "" {
		harness = i18n.T("không rõ")
	}
	return i18n.Tf("aisdlc-harness khác bản build với aisdlc (aisdlc %s, aisdlc-harness %s). Cài lại plugin (plugin/install.sh hoặc make plugin-dist) để hai binary cùng bản", cli, harness)
}

// logHarness ghi vào harness.log binary đã dùng và toàn bộ stdout/stderr của nó.
func logHarness(out, bin, output string) {
	line := "aisdlc-harness: " + bin
	if o := strings.TrimRight(output, "\n"); o != "" {
		line += i18n.Tf("\n--- stdout/stderr của aisdlc-harness ---\n%s\n--- hết ---", o)
	}
	appendLog(out, line)
}

// harnessErrorLine là một dòng tóm lý do harness chết: dòng `panic:`/`fatal error:` đầu tiên, không có thì dòng cuối
// không rỗng của output.
func harnessErrorLine(output string) string {
	last := ""
	for _, l := range strings.Split(output, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "panic:") || strings.HasPrefix(l, "fatal error:") {
			return l
		}
		if l != "" {
			last = l
		}
	}
	return last
}
