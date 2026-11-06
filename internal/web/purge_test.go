package web

import (
	"context"
	"testing"
	"time"
)

func TestNextPurge(t *testing.T) {
	d := func(h, m int) time.Time { return time.Date(2026, 11, 5, h, m, 0, 0, time.Local) }
	cases := []struct{ now, want time.Time }{
		{d(1, 0), d(3, 0)},
		{d(3, 0), time.Date(2026, 11, 6, 3, 0, 0, 0, time.Local)},
		{d(23, 59), time.Date(2026, 11, 6, 3, 0, 0, 0, time.Local)},
	}
	for _, c := range cases {
		if got := NextPurge(c.now.UTC()); !got.Equal(c.want) {
			t.Fatalf("%v: %v != %v", c.now, got, c.want)
		}
	}
}

func TestSchedulePurgeRunsDailyAndStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := time.Date(2026, 11, 5, 2, 0, 0, 0, time.Local)
	var waits []time.Duration
	runs := make(chan struct{}, 10)
	done := make(chan struct{})
	after := func(d time.Duration) <-chan time.Time {
		waits = append(waits, d)
		clock = clock.Add(d)
		c := make(chan time.Time, 1)
		c <- clock
		return c
	}
	purge := func(context.Context) (int64, error) {
		runs <- struct{}{}
		if len(waits) == 3 {
			cancel()
		}
		return 0, nil
	}
	go func() { SchedulePurge(ctx, func() time.Time { return clock }, after, purge); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SchedulePurge không dừng khi ctx hủy")
	}
	if len(runs) != 3 || waits[0] != time.Hour || waits[1] != 24*time.Hour {
		t.Fatal(len(runs), waits)
	}
}
