package gitx

import (
	"context"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var ctx = context.Background()

// hermetic bỏ qua cấu hình git toàn cục và hệ thống của máy chạy test.
func hermetic(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("HOME", t.TempDir())
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

type fixture struct {
	t    *testing.T
	dir  string
	repo *Repo
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	hermetic(t)
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "user.name", "Test User")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	return &fixture{t: t, dir: dir, repo: New(dir)}
}

func (f *fixture) git(args ...string) string { return gitIn(f.t, f.dir, args...) }

func (f *fixture) write(name, content string) {
	f.t.Helper()
	p := filepath.Join(f.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) remove(name string) {
	f.t.Helper()
	if err := os.Remove(filepath.Join(f.dir, name)); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) commitAll() {
	f.t.Helper()
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "c")
}

func (f *fixture) snap() string {
	f.t.Helper()
	s, err := f.repo.SnapshotTree(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) diff(from, to string) []FileChange {
	f.t.Helper()
	d, err := f.repo.Diff(ctx, from, to)
	if err != nil {
		f.t.Fatal(err)
	}
	return d
}

func (f *fixture) changed(from, to string) map[string]map[int]bool {
	f.t.Helper()
	m, err := f.repo.ChangedLines(ctx, from, to)
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func lines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	return b.String()
}

func keys[V any](m map[string]V) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

func sortedInts(s map[int]bool) []int {
	var r []int
	for x := range s {
		r = append(r, x)
	}
	sort.Ints(r)
	return r
}

func eq(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestFileAtTreeReadsCommittedContentOrEmpty(t *testing.T) {
	f := newFixture(t)
	f.write("aisdlc/config.yaml", "ciRequireFeature: true\n")
	tree := f.snap()
	f.write("aisdlc/config.yaml", "ciRequireFeature: false\n")

	c, ok, err := f.repo.FileAt(ctx, tree, "aisdlc/config.yaml")
	if err != nil || !ok || c != "ciRequireFeature: true\n" {
		t.Fatalf("got %q %v %v", c, ok, err)
	}
	if _, ok, err := f.repo.FileAt(ctx, tree, "missing.yaml"); ok || err != nil {
		t.Fatalf("missing: %v %v", ok, err)
	}
	if _, ok, err := f.repo.FileAt(ctx, EmptyTree, "aisdlc/config.yaml"); ok || err != nil {
		t.Fatalf("empty tree: %v %v", ok, err)
	}
}

func TestSnapshotExcludingDirIgnoresChangesUnderIt(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "hello\n")
	base, err := f.repo.SnapshotTreeExcluding(ctx, "feat/X")
	if err != nil {
		t.Fatal(err)
	}
	f.write("feat/X/report.json", "{}\n")
	again, _ := f.repo.SnapshotTreeExcluding(ctx, "feat/X")
	if again != base {
		t.Fatal("excluded dir changed snapshot")
	}
	if f.snap() == base {
		t.Fatal("full snapshot should differ")
	}
	f.write("a.txt", "changed\n")
	again, _ = f.repo.SnapshotTreeExcluding(ctx, "feat/X")
	if again == base {
		t.Fatal("a.txt change should differ")
	}
}

func TestSnapshotIncludesUntrackedFiles(t *testing.T) {
	f := newFixture(t)
	base := f.snap()
	f.write("a.txt", "hello\n")
	ch := f.diff(base, f.snap())
	if len(ch) != 1 {
		t.Fatalf("got %v", ch)
	}
	c := ch[0]
	if c.Path != "a.txt" || c.Type != Added || c.Added != 1 || c.OldPath != "" {
		t.Fatalf("got %+v", c)
	}
}

func TestSnapshotDoesNotTouchUserIndex(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "hello\n")
	f.snap()
	if got := strings.TrimSpace(f.git("status", "--porcelain")); got != "?? a.txt" {
		t.Fatalf("got %q", got)
	}
}

func TestDetectsDeleteAndRename(t *testing.T) {
	f := newFixture(t)
	f.write("x.txt", lines(10))
	f.write("y.txt", strings.ReplaceAll(lines(10), "line", "other"))
	f.commitAll()
	before := f.snap()
	f.remove("x.txt")
	if err := os.Rename(filepath.Join(f.dir, "y.txt"), filepath.Join(f.dir, "z.txt")); err != nil {
		t.Fatal(err)
	}
	var del, ren bool
	for _, c := range f.diff(before, f.snap()) {
		if c.Path == "x.txt" && c.Type == Deleted && c.Deleted == 10 {
			del = true
		}
		if c.Path == "z.txt" && c.OldPath == "y.txt" && c.Type == Renamed {
			ren = true
		}
	}
	if !del || !ren {
		t.Fatalf("del=%v ren=%v", del, ren)
	}
}

func TestBinaryFileHasZeroLinesAndFlag(t *testing.T) {
	f := newFixture(t)
	base := f.snap()
	b := make([]byte, 1024)
	rand.New(rand.NewSource(42)).Read(b)
	b[0] = 0
	if err := os.WriteFile(filepath.Join(f.dir, "b.bin"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.diff(base, f.snap()) {
		if c.Path == "b.bin" {
			if c.Type != Added || !c.Binary || c.Lines() != 0 {
				t.Fatalf("got %+v", c)
			}
			return
		}
	}
	t.Fatal("b.bin not found")
}

func TestChangedLinesReportsNewSideLineNumbers(t *testing.T) {
	f := newFixture(t)
	f.write("f.txt", lines(5))
	f.commitAll()
	before := f.snap()
	f.write("f.txt", strings.Replace(lines(5), "line 3", "changed 3", 1))
	m := f.changed(before, f.snap())
	eq(t, keys(m), []string{"f.txt"})
	eq(t, sortedInts(m["f.txt"]), []int{3})
}

func TestIgnoredFilesExcluded(t *testing.T) {
	f := newFixture(t)
	f.write(".gitignore", "target/\n")
	f.commitAll()
	before := f.snap()
	f.write("target/x", "junk\n")
	f.write("src.txt", "s\n")
	var paths []string
	for _, c := range f.diff(before, f.snap()) {
		paths = append(paths, c.Path)
	}
	eq(t, paths, []string{"src.txt"})
}

func TestSnapshotWorksWithoutCommits(t *testing.T) {
	hermetic(t)
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := New(dir)
	s, err := r.SnapshotTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.Diff(ctx, EmptyTree, s)
	if err != nil || len(d) != 1 {
		t.Fatalf("got %v %v", d, err)
	}
}

func TestMergeBaseTreeAndUserName(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "1\n")
	f.commitAll()
	branchTree := f.snap()
	f.git("branch", "base")
	f.write("b.txt", "2\n")
	f.commitAll()
	got, err := f.repo.MergeBaseTree(ctx, "base")
	if err != nil || got != branchTree {
		t.Fatalf("got %q %v want %q", got, err, branchTree)
	}
	n, err := f.repo.UserName(ctx)
	if err != nil || n != "Test User" {
		t.Fatalf("got %q %v", n, err)
	}
}

func TestGitFailureReturnsErrorWithStderr(t *testing.T) {
	f := newFixture(t)
	_, err := f.repo.MergeBaseTree(ctx, "no-such-ref")
	if err == nil || !strings.Contains(err.Error(), "no-such-ref") {
		t.Fatalf("got %v", err)
	}
	ge, ok := err.(*Error)
	if !ok || ge.Stderr == "" || !strings.Contains(ge.Msg, "thất bại") {
		t.Fatalf("got %#v", err)
	}
}

func TestChangedLinesKeysMatchDiffPathsForSpecialNames(t *testing.T) {
	f := newFixture(t)
	base := f.snap()
	f.write("a b.txt", "x\n")
	f.write("tài liệu.txt", "y\nz\n")
	f.write("q\"t.txt", "w\n")
	to := f.snap()
	m := f.changed(base, to)
	want := []string{"a b.txt", "q\"t.txt", "tài liệu.txt"}
	eq(t, keys(m), want)
	eq(t, sortedInts(m["tài liệu.txt"]), []int{1, 2})
	var paths []string
	for _, c := range f.diff(base, to) {
		paths = append(paths, c.Path)
	}
	sort.Strings(paths)
	eq(t, paths, want)
}

func TestContentLineStartingWithPlusPlusIsNotAHeader(t *testing.T) {
	f := newFixture(t)
	f.write("g.txt", "1\n2\n3\n")
	f.commitAll()
	before := f.snap()
	f.write("n.txt", "a\n++ x\nb\n")
	f.write("g.txt", "1\n2\n3\n4\n")
	m := f.changed(before, f.snap())
	eq(t, keys(m), []string{"g.txt", "n.txt"})
	eq(t, sortedInts(m["n.txt"]), []int{1, 2, 3})
	eq(t, sortedInts(m["g.txt"]), []int{4})
}

func TestChangedLinesOmitsDeletedFilesAndIncludesUntracked(t *testing.T) {
	f := newFixture(t)
	f.write("d.txt", "1\n2\n")
	f.commitAll()
	before := f.snap()
	f.remove("d.txt")
	f.write("u.txt", "new\n")
	m := f.changed(before, f.snap())
	eq(t, keys(m), []string{"u.txt"})
	eq(t, sortedInts(m["u.txt"]), []int{1})
}

func TestPatchShowsUnifiedDiffBetweenTrees(t *testing.T) {
	f := newFixture(t)
	f.write("p.txt", "old\n")
	before := f.snap()
	f.write("p.txt", "new\n")
	to := f.snap()
	p, err := f.repo.Patch(ctx, before, to)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"diff --git a/p.txt b/p.txt", "-old", "+new"} {
		if !strings.Contains(p, s) {
			t.Fatalf("missing %q in %q", s, p)
		}
	}
	f.write("q.txt", "other\n")
	p, err = f.repo.PatchPaths(ctx, before, f.snap(), []string{"p.txt"})
	if err != nil || strings.Contains(p, "q.txt") || !strings.Contains(p, "p.txt") {
		t.Fatalf("filtered patch: %q %v", p, err)
	}
}

func TestListFilesHandlesSpacesAndUnicode(t *testing.T) {
	f := newFixture(t)
	f.write("a b/Tệp tin.txt", "x")
	f.write("z.txt", "y")
	f.git("add", "a b/Tệp tin.txt")
	got, err := f.repo.ListFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, []string{"a b/Tệp tin.txt", "z.txt"})
}

func TestListFilesExcludesDeleted(t *testing.T) {
	f := newFixture(t)
	f.write("gone.txt", "x")
	f.write("keep.txt", "y")
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "two")
	f.remove("gone.txt")
	got, err := f.repo.ListFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, []string{"keep.txt"})
}

// Ngoài 17 test của Java: các accessor không có test Java.
func TestListTreeRemoteURLCurrentBranch(t *testing.T) {
	f := newFixture(t)
	f.write("a/x.txt", "1")
	f.write("b/y.txt", "2")
	f.commitAll()
	all, err := f.repo.ListTree(ctx, "HEAD")
	eq(t, all, []string{"a/x.txt", "b/y.txt"})
	if err != nil {
		t.Fatal(err)
	}
	under, _ := f.repo.ListTree(ctx, "HEAD", "a")
	eq(t, under, []string{"a/x.txt"})

	if _, ok, err := f.repo.RemoteURL(ctx, "origin"); ok || err != nil {
		t.Fatalf("remote: %v %v", ok, err)
	}
	f.git("remote", "add", "origin", "https://example.com/r.git")
	u, ok, _ := f.repo.RemoteURL(ctx, "origin")
	if !ok || u != "https://example.com/r.git" {
		t.Fatalf("got %q %v", u, ok)
	}

	b, ok, _ := f.repo.CurrentBranch(ctx)
	if !ok || b == "" {
		t.Fatalf("branch %q %v", b, ok)
	}
	f.git("checkout", "-q", "--detach")
	if _, ok, _ := f.repo.CurrentBranch(ctx); ok {
		t.Fatal("detached should be absent")
	}
}

func TestPatchPathsEmptyReturnsEmpty(t *testing.T) {
	f := newFixture(t)
	// ref không tồn tại: nếu có gọi git thì sẽ lỗi
	p, err := f.repo.PatchPaths(ctx, "no-such-ref", "no-such-ref", nil)
	if p != "" || err != nil {
		t.Fatalf("got %q %v", p, err)
	}
}

func TestPatchPathsLimitsOutputToListedPath(t *testing.T) {
	f := newFixture(t)
	before := f.snap()
	f.write("a.txt", "1\n")
	f.write("b.txt", "2\n")
	p, err := f.repo.PatchPaths(ctx, before, f.snap(), []string{"b.txt"})
	if err != nil || !strings.Contains(p, "b/b.txt") || strings.Contains(p, "a.txt") {
		t.Fatalf("got %q %v", p, err)
	}
}
