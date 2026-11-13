package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitTreeAndRef(t *testing.T) {
	f := newFixture(t)
	f.write("a.go", "1")
	tree := f.snap()
	sha, err := f.repo.CommitTree(ctx, tree, "checkpoint S1")
	if err != nil {
		t.Fatal(err)
	}
	const ref = "refs/aisdlc/checkpoints/K/S1"
	if _, ok, err := f.repo.ResolveRef(ctx, ref); err != nil || ok {
		t.Fatalf("ref chưa có: ok=%v err=%v", ok, err)
	}
	if err := f.repo.UpdateRef(ctx, ref, sha); err != nil {
		t.Fatal(err)
	}
	got, ok, err := f.repo.ResolveRef(ctx, ref)
	if err != nil || !ok || got != sha {
		t.Fatalf("ResolveRef = %q %v %v, muốn %q", got, ok, err, sha)
	}
	f.git("gc", "-q", "--prune=now")
	if !f.repo.ObjectExists(ctx, tree) {
		t.Fatal("tree bị gc dù có ref")
	}
	if f.repo.ObjectExists(ctx, strings.Repeat("0", 40)) {
		t.Fatal("object giả không được tồn tại")
	}
}

func TestRestorePaths(t *testing.T) {
	f := newFixture(t)
	f.write("a.go", "1")
	f.write("c.go", "keep")
	if err := os.WriteFile(filepath.Join(f.dir, "x.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.commitAll()
	t0 := f.snap()
	head := strings.TrimSpace(f.git("rev-parse", "HEAD"))

	f.write("a.go", "2")
	f.write("b.go", "new")
	f.remove("c.go")
	f.remove("x.sh")
	now := f.snap()
	changes := f.diff(t0, now)
	if err := f.repo.RestorePaths(ctx, t0, changes); err != nil {
		t.Fatal(err)
	}
	read := func(n string) string {
		b, err := os.ReadFile(filepath.Join(f.dir, n))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	eq(t, read("a.go"), "1")
	eq(t, read("c.go"), "keep")
	if _, err := os.Stat(filepath.Join(f.dir, "b.go")); !os.IsNotExist(err) {
		t.Fatal("b.go phải bị xoá")
	}
	st, err := os.Stat(filepath.Join(f.dir, "x.sh"))
	if err != nil || st.Mode()&0o100 == 0 {
		t.Fatalf("x.sh phải giữ mode thực thi: %v %v", st, err)
	}
	eq(t, strings.TrimSpace(f.git("rev-parse", "HEAD")), head)
	eq(t, f.snap(), t0)
}

func TestRestorePathsRename(t *testing.T) {
	f := newFixture(t)
	f.write("old.go", strings.Repeat("line\n", 20))
	f.commitAll()
	t0 := f.snap()
	f.git("mv", "old.go", "new.go")
	changes := f.diff(t0, f.snap())
	if err := f.repo.RestorePaths(ctx, t0, changes); err != nil {
		t.Fatal(err)
	}
	eq(t, f.snap(), t0)
}
