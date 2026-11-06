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

	"aisdlc/internal/harness/harnessapi"
)

// HarnessEnv là biến môi trường trỏ tới binary aisdlc-harness (bỏ qua cách tìm mặc định).
const HarnessEnv = "AISDLC_HARNESS"

const (
	harnessBin     = "aisdlc-harness"
	harnessMissing = "không tìm thấy aisdlc-harness: "
	// harnessStopWait: sau Ctrl-C, chờ harness dọn dịch vụ và container (harness tự giới hạn dọn 30 giây) trước khi
	// coi như tín hiệu thứ hai.
	harnessStopWait = 45 * time.Second
	// harnessKillWait: sau tín hiệu thứ hai, chờ harness SIGKILL dịch vụ và thoát trước khi SIGKILL nhóm của nó.
	harnessKillWait = 3 * time.Second
	stderrTailLines = 20
)

// findHarness tìm binary aisdlc-harness: $AISDLC_HARNESS; cạnh binary aisdlc (aisdlc-harness, hoặc
// aisdlc-harness-<os>-<arch> theo bố cục plugin/lib); rồi PATH.
func findHarness() (string, error) {
	if p := os.Getenv(HarnessEnv); p != "" {
		if executable(p) {
			return p, nil
		}
		return "", errors.New(harnessMissing + HarnessEnv + "=" + p + " không phải file chạy được")
	}
	names := []string{harnessBin, harnessBin + "-" + runtime.GOOS + "-" + runtime.GOARCH}
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
		if real, err := filepath.EvalSymlinks(exe); err == nil && filepath.Dir(real) != dirs[0] {
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
	if p, err := exec.LookPath(harnessBin); err == nil {
		return p, nil
	}
	return "", errors.New(harnessMissing + "không có cạnh aisdlc (" + strings.Join(dirs, ", ") + "), không có trên PATH và " +
		HarnessEnv + " chưa đặt. Cài lại plugin hoặc `make build`")
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
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		return harnessapi.Result{}, fmt.Errorf("không chạy được %s: %w", bin, err)
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

	r, rerr := harnessapi.ReadResult(out)
	if rerr == nil {
		return r, nil
	}
	why := "thoát bình thường"
	if err != nil {
		why = err.Error()
	}
	msg := fmt.Sprintf("aisdlc-harness không ghi kết quả (%s)", why)
	if t := lastLines(output.String(), stderrTailLines); t != "" {
		msg += ":\n" + t
	}
	return harnessapi.Result{}, errors.New(msg)
}

func lastLines(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
