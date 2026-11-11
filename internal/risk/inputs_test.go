package risk

import (
	"os"
	"path/filepath"
	"testing"

	"aisdlc/internal/model"
)

// db/ chọn file như hash cổng DESIGN: chỉ cấp trên cùng, symlink tới file thường được tính, thư mục con thì không.
func TestInputFilesDbTopLevelLikeDesignGate(t *testing.T) {
	root := t.TempDir()
	paths := model.FeaturePaths{RepoRoot: root, Key: model.FeatureKey("ABC-1")}
	db := paths.DbDrafts()
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(db, "V1__a.sql"), "a")
	write(filepath.Join(db, "sub", "V2__nested.sql"), "nested")
	write(filepath.Join(root, "outside.sql"), "linked")
	if err := os.Symlink(filepath.Join(root, "outside.sql"), filepath.Join(db, "V3__link.sql")); err != nil {
		t.Fatal(err)
	}
	files, err := InputFiles(root, paths, model.FlowFull)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Rel)
	}
	rel := func(name string) string {
		r, _ := filepath.Rel(root, filepath.Join(db, name))
		return filepath.ToSlash(r)
	}
	want := []string{rel("V1__a.sql"), rel("V3__link.sql")}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("InputFiles = %v, muốn %v", got, want)
	}
	gateFiles := model.DbDraftFiles(db)
	if len(gateFiles) != 2 {
		t.Fatalf("DbDraftFiles = %v", gateFiles)
	}
}
