package webclient

import (
	"os"
	"os/exec"
	"syscall"
)

// StartDetached mở tiến trình exe args ở thư mục dir, tách hẳn khỏi tiến trình gọi (BackgroundFlush): session mới
// (Setsid), stdin/stdout/stderr về /dev/null nên không giữ ống của hook, không chờ nó xong. Tiến trình gọi thoát
// trước thì nó được init nhận nuôi; còn sống thì một goroutine thu dọn để không thành zombie.
func StartDetached(exe string, args []string, dir string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	// Stdin/Stdout/Stderr nil: exec nối cả ba vào os.DevNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// SpawnFlush mở `aisdlc web flush` nền ở gốc repo root, bằng chính binary đang chạy.
func SpawnFlush(root string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return StartDetached(exe, []string{"web", "flush"}, root)
}
