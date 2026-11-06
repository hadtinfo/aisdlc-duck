package ingest_test

import (
	"context"
	"testing"
	"time"

	"aisdlc/internal/web/ingest"
)

func (f *fixture) purge(t *testing.T) int64 {
	t.Helper()
	n, err := (&ingest.Service{Pool: f.e.Pool, Now: func() time.Time { return *f.e.Now }}).PurgeOld(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}
