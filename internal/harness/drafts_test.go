package harness

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"aisdlc/internal/lang/golang"
)

func put(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("-- "+rel), 0o644); err != nil {
		t.Fatal(err)
	}
}

func prep(t *testing.T, tmp string) bool {
	t.Helper()
	ok, err := PrepareDrafts(filepath.Join(tmp, "drafts"), filepath.Join(tmp, "main"), filepath.Join(tmp, "out/drafts"), golang.Adapter{})
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestDraftsKeepOnlyVersionsNotInMain(t *testing.T) {
	tmp := t.TempDir()
	put(t, tmp, "main/00001_init.sql")
	put(t, tmp, "main/sub/00002_daily_limit.sql") // quét cả thư mục con
	put(t, tmp, "drafts/00002_daily_limit.sql")   // bước code đã chép vào src
	put(t, tmp, "drafts/2_other_name.sql")        // cùng version 2 dù khác tên
	put(t, tmp, "drafts/00003_add_note.sql")
	put(t, tmp, "drafts/ghi-chu.txt")
	if !prep(t, tmp) {
		t.Fatal("expected drafts")
	}
	if got := names(t, filepath.Join(tmp, "out/drafts")); !reflect.DeepEqual(got, []string{"00003_add_note.sql"}) {
		t.Errorf("got %v", got)
	}
}

func TestDraftsNoRemainingMeansNotOK(t *testing.T) {
	tmp := t.TempDir()
	put(t, tmp, "main/00002_daily_limit.sql")
	put(t, tmp, "drafts/00002_daily_limit.sql")
	if prep(t, tmp) {
		t.Error("ok must be false")
	}
}

func TestDraftsMissingDirectoriesAreFine(t *testing.T) {
	tmp := t.TempDir()
	if prep(t, tmp) {
		t.Error("ok must be false")
	}
	put(t, tmp, "drafts/00005_x.sql")
	if !prep(t, tmp) { // không có thư mục migration chính
		t.Error("ok must be true")
	}
}

func TestDraftsStaleCopiesRemoved(t *testing.T) {
	tmp := t.TempDir()
	put(t, tmp, "drafts/00005_x.sql")
	put(t, tmp, "out/drafts/00009_old.sql")
	if !prep(t, tmp) {
		t.Fatal("expected drafts")
	}
	if got := names(t, filepath.Join(tmp, "out/drafts")); !reflect.DeepEqual(got, []string{"00005_x.sql"}) {
		t.Errorf("got %v", got)
	}
}
