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

// SpawnFlush mở `aisdlc web flush` nền ở gốc repo root, bằng chính binary đang chạy. feature khác rỗng (tính năng
// của phiên gọi, có thể không phải .aisdlc/active khi nhiều phiên song song) thì thêm `--feature KEY` để tiến trình
// nền làm mới cả cổng và điều khiển của tính năng đó.
func SpawnFlush(root, feature string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return StartDetached(exe, FlushArgs(feature), root)
}

// FlushArgs là đối số của tiến trình gửi nền: `web flush [--feature KEY]`.
func FlushArgs(feature string) []string {
	if feature == "" {
		return []string{"web", "flush"}
	}
	return []string{"web", "flush", "--feature", feature}
}
