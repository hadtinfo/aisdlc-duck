package web

import (
	"context"
	"log"
	"time"
)

// PurgeHour là giờ địa phương chạy dọn sự kiện cũ (Java: @Scheduled(cron = "0 0 3 * * *"), múi giờ máy chủ).
const PurgeHour = 3

// NextPurge trả lần 03:00 địa phương kế tiếp sau now (luôn sau now, kể cả khi now đúng 03:00).
func NextPurge(now time.Time) time.Time {
	l := now.In(time.Local)
	next := time.Date(l.Year(), l.Month(), l.Day(), PurgeHour, 0, 0, 0, time.Local)
	if !next.After(now) {
		next = time.Date(l.Year(), l.Month(), l.Day()+1, PurgeHour, 0, 0, 0, time.Local)
	}
	return next
}

// SchedulePurge chạy purge hằng ngày lúc 03:00 địa phương cho đến khi ctx hủy. now và after tiêm được để test.
func SchedulePurge(ctx context.Context, now func() time.Time, after func(time.Duration) <-chan time.Time,
	purge func(context.Context) (int64, error)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-after(NextPurge(now()).Sub(now())):
		}
		if ctx.Err() != nil {
			return
		}
		if n, err := purge(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("web: dọn sự kiện cũ lỗi: %v", err)
		} else if n > 0 {
			log.Printf("web: đã xóa %d sự kiện cũ", n)
		}
	}
}
