package harness

import (
	"sync"

	"aisdlc/internal/lang"
)

// live là các tiến trình dịch vụ harness đã khởi động mà chưa dừng, để Ctrl-C lần hai diệt ngay được.
var live = struct {
	sync.Mutex
	procs map[lang.Process]struct{}
}{procs: map[lang.Process]struct{}{}}

// track ghi nhận p đang chạy; hàm trả về bỏ ghi nhận (gọi sau khi p đã dừng).
func track(p lang.Process) func() {
	live.Lock()
	live.procs[p] = struct{}{}
	live.Unlock()
	return func() {
		live.Lock()
		delete(live.procs, p)
		live.Unlock()
	}
}

// KillServices SIGKILL ngay (đồng bộ, không chờ) nhóm tiến trình của mọi dịch vụ harness đang chạy trong tiến trình
// này. Dùng khi dev bấm Ctrl-C lần hai: không chờ dọn êm nữa, nhưng không để sót dịch vụ.
func KillServices() {
	live.Lock()
	procs := make([]lang.Process, 0, len(live.procs))
	for p := range live.procs {
		procs = append(procs, p)
	}
	live.Unlock()
	for _, p := range procs {
		p.Kill()
	}
}
