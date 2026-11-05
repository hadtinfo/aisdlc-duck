package golang_test

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"aisdlc/internal/gitx"
	"aisdlc/internal/lang"
	"aisdlc/internal/lang/golang"
	"aisdlc/internal/lang/langs"
)

func TestIsSource(t *testing.T) {
	a := golang.Adapter{}
	for p, want := range map[string]bool{
		"cmd/server/main.go":       true,
		"internal/fee/fee_test.go": false,
		"vendor/x/a.go":            false,
		"pkg/testdata/a.go":        false,
		"README.md":                false,
	} {
		if got := a.IsSource(p); got != want {
			t.Errorf("IsSource(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestMigrationOrder(t *testing.T) {
	in := []string{"db/migrations/00010_b.sql", "db/migrations/00002_a.sql", "db/migrations/notes.sql", "db/migrations/20261105120000_c.sql", "db/migrations/00002_0.sql"}
	want := []string{"db/migrations/00002_0.sql", "db/migrations/00002_a.sql", "db/migrations/00010_b.sql", "db/migrations/20261105120000_c.sql", "db/migrations/notes.sql"}
	got := golang.Adapter{}.MigrationOrder(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if in[0] != "db/migrations/00010_b.sql" {
		t.Error("input mutated")
	}
}

func TestMigrationVersionHuge(t *testing.T) {
	v, ok := golang.Adapter{}.MigrationVersion("99999999999999999999_x.sql")
	if v != math.MaxInt64 || !ok {
		t.Errorf("got %d %v", v, ok)
	}
	if _, ok := (golang.Adapter{}).MigrationVersion("notes.sql"); ok {
		t.Error("notes.sql has no version")
	}
}

type mapSource map[string]string

func (m mapSource) List(context.Context) ([]string, error) {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out, nil
}
func (m mapSource) Read(_ context.Context, p string) ([]byte, error) {
	return []byte(m[p]), nil
}

func TestDependencyAdded(t *testing.T) {
	base := mapSource{"go.mod": "module x\n\ngo 1.23\n\nrequire github.com/go-chi/chi/v5 v5.0.0\n"}
	now := mapSource{"go.mod": "module x\n\ngo 1.23\n\nrequire (\n\tgithub.com/go-chi/chi/v5 v5.0.0\n\tgithub.com/jackc/pgx/v5 v5.5.0\n)\n\nreplace example.com/a => ../a\n"}
	d := golang.Adapter{}.DependencyChanges(context.Background(), base, now)
	want := []string{"github.com/jackc/pgx/v5", "replace example.com/a => ../a"}
	if d.NA != "" || !reflect.DeepEqual(d.Added, want) {
		t.Errorf("got %+v", d)
	}
}

func TestDependencyNoGoMod(t *testing.T) {
	d := golang.Adapter{}.DependencyChanges(context.Background(), mapSource{}, mapSource{"a.go": "package a"})
	if d.NA != "Repo không có go.mod, bỏ qua kiểm tra dependency." {
		t.Errorf("got %+v", d)
	}
}

func TestDependencyBrokenGoMod(t *testing.T) {
	d := golang.Adapter{}.DependencyChanges(context.Background(), mapSource{}, mapSource{"go.mod": "module"})
	if !strings.HasPrefix(d.NA, "Không đọc được go.mod hoặc base tree: ") || strings.Contains(d.NA, "\n") {
		t.Errorf("got %q", d.NA)
	}
}

func TestTreeSourceReadsBaseTree(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "HOME="+home)
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = env
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.name", "t")
	run("config", "user.email", "t@t")
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "c")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("HOME", home)
	src := lang.TreeSource(gitx.New(dir), "HEAD")
	files, err := src.List(context.Background())
	if err != nil || !reflect.DeepEqual(files, []string{"go.mod"}) {
		t.Fatalf("%v %v", files, err)
	}
	b, err := src.Read(context.Background(), "go.mod")
	if err != nil || string(b) != "module x\n" {
		t.Fatalf("%q %v", b, err)
	}
	ds := lang.DirSource(dir, []string{"go.mod"})
	if b, err := ds.Read(context.Background(), "go.mod"); err != nil || string(b) != "module x\n" {
		t.Fatalf("dir %q %v", b, err)
	}
}

func TestForUnknownLanguage(t *testing.T) {
	_, err := langs.For("java")
	if err == nil || err.Error() != "Ngôn ngữ chưa hỗ trợ: java (hiện có: go)" {
		t.Errorf("got %v", err)
	}
	a, err := langs.For("go")
	if err != nil || a.Name() != "go" || a.SampleFileName() != "aisdlcx.go" {
		t.Errorf("got %v %v", a, err)
	}
}

func TestDependencyUnknownDirectiveIsNA(t *testing.T) {
	base := mapSource{"go.mod": "module x\n\ngo 1.23\n"}
	now := mapSource{"go.mod": "module x\n\ngo 1.23\n\nfrobnicate x\n\nrequire example.com/a v1.0.0\n"}
	d := golang.Adapter{}.DependencyChanges(context.Background(), base, now)
	if !strings.HasPrefix(d.NA, "Không đọc được go.mod hoặc base tree: ") || strings.Contains(d.NA, "\n") || len(d.Added) != 0 {
		t.Errorf("got %+v", d)
	}
	// lỗi ở phía base cũng là N/A
	d = golang.Adapter{}.DependencyChanges(context.Background(), now, base)
	if !strings.HasPrefix(d.NA, "Không đọc được go.mod hoặc base tree: ") {
		t.Errorf("base side: got %+v", d)
	}
}

func TestDependencyIgnoreDirectiveStillReportsReplace(t *testing.T) {
	base := mapSource{"go.mod": "module x\n\ngo 1.25\n"}
	now := mapSource{"go.mod": "module x\n\ngo 1.25\n\nignore ./foo\n\nreplace example.com/a => ../a\n"}
	d := golang.Adapter{}.DependencyChanges(context.Background(), base, now)
	if d.NA != "" || !reflect.DeepEqual(d.Added, []string{"replace example.com/a => ../a"}) {
		t.Errorf("got %+v", d)
	}
}

func TestDependencyBaseWithoutGoModReportsAllAsAdded(t *testing.T) {
	now := mapSource{"go.mod": "module x\n\ngo 1.23\n\nrequire (\n\texample.com/a v1.0.0\n\texample.com/b v1.0.0\n)\n"}
	d := golang.Adapter{}.DependencyChanges(context.Background(), mapSource{"main.go": "package main"}, now)
	if d.NA != "" || !reflect.DeepEqual(d.Added, []string{"example.com/a", "example.com/b"}) {
		t.Errorf("got %+v", d)
	}
}

func TestMigrationVersionIgnoresTestFiles(t *testing.T) {
	a := golang.Adapter{}
	if _, ok := a.MigrationVersion("db/migrations/00003_x_test.go"); ok {
		t.Error("_test.go is not a migration")
	}
	if v, ok := a.MigrationVersion("db/migrations/00003_x.go"); !ok || v != 3 {
		t.Errorf("got %d %v", v, ok)
	}
}

func TestLanguageLookupTrimsAndLowercases(t *testing.T) {
	a, err := langs.For(" Go ")
	if err != nil || a.Name() != "go" {
		t.Errorf("got %v %v", a, err)
	}
}

func TestFileSourceMissingPathIsNotExist(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("HOME", home)
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.name", "t")
	run("config", "user.email", "t@t")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "c")
	ctx := context.Background()
	if _, err := lang.TreeSource(gitx.New(dir), "HEAD").Read(ctx, "nope.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("tree: %v", err)
	}
	if _, err := lang.DirSource(dir, nil).Read(ctx, "nope.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("dir: %v", err)
	}
}
