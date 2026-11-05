package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"aisdlc/internal/fsx"
)

func TestSnapshotExcludesAtomicTempFiles(t *testing.T) {
	f := newFixture(t)
	f.write("aisdlc/features/A/.gates.json.tmp-123", "x")
	f.write("aisdlc/features/A/gates.json", "{}")
	tree := f.snap()
	files, err := f.repo.ListTree(ctx, tree)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, p := range files {
		if strings.Contains(p, ".tmp-") {
			t.Fatalf("snapshot chứa file tạm: %s", p)
		}
		if p == "aisdlc/features/A/gates.json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("thiếu gates.json: %v", files)
	}
}

func TestSnapshotRaceWithAtomicWriters(t *testing.T) {
	f := newFixture(t)
	p := filepath.Join(f.dir, "aisdlc", "features", "B", "steps.json")
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = fsx.WriteAtomic(p, []byte(fmt.Sprintf("[%d]", i)), 0o644)
		}
	}()
	defer func() { close(stop); wg.Wait() }()
	for i := 0; i < 100; i++ {
		if _, err := f.repo.SnapshotTree(ctx); err != nil {
			t.Fatalf("lần %d: %v", i, err)
		}
	}
	_ = os.Remove(p)
}
