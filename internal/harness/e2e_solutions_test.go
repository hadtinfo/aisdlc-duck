package harness

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"aisdlc/internal/harness/harnesstest"
	"aisdlc/internal/lang/golang"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
)

// solFeature mô tả cách e2e/run-e2e.sh chép lời giải của một tính năng: file tài liệu vào aisdlc/features/<KEY>/,
// migration nháp vào db/ của tính năng, rồi code từng bước (thư mục trong e2e/solutions) vào gốc repo.
type solFeature struct {
	key    model.FeatureKey
	docs   []string // file trong e2e/solutions/<KEY>/ chép vào thư mục tính năng
	drafts bool     // e2e/solutions/<KEY>/db/*.sql → aisdlc/features/<KEY>/db/
	steps  []solStep
}

type solStep struct {
	id  string
	dir string // thư mục code của bước, tương đối e2e/solutions
}

func solutionsDir() string { return filepath.Join(harnesstest.FixtureDir(), "..", "solutions") }

// Lời giải e2e chạy được trên dịch vụ mẫu: trước code của bước thì case của bước fail (như submit cases đòi), sau
// code thì pass; sau bước cuối build, unit test và mọi case đều đạt.
func TestE2ESolutions(t *testing.T) {
	harnesstest.RequireDocker(t)
	for _, f := range []solFeature{
		{key: "PAY-1", docs: []string{"brd.md", "design.md", "design.yaml", "cases.yaml"}, drafts: true,
			steps: []solStep{{"S1", "PAY-1/S1"}, {"S2", "PAY-1/S2"}}},
		{key: "FIX-2", docs: []string{"cases.yaml"}, steps: []solStep{{"S1", "FIX-2/S1"}}},
		{key: "FEE-3", docs: []string{"brd.md", "design.md", "design.yaml", "cases.yaml"},
			steps: []solStep{{"S1", "FIX-2/S1"}}}, // cùng sửa phí như FIX-2
	} {
		t.Run(string(f.key), func(t *testing.T) { runSolution(t, f) })
	}
}

func runSolution(t *testing.T, f solFeature) {
	sol := solutionsDir()
	root := harnesstest.CopyDir(t, harnesstest.FixtureDir())
	fdir := filepath.Join(root, "aisdlc/features", string(f.key))
	for _, d := range f.docs {
		copyFile(t, filepath.Join(sol, string(f.key), d), filepath.Join(fdir, d))
	}
	if f.drafts {
		copyTree(t, filepath.Join(sol, string(f.key), "db"), filepath.Join(fdir, "db"))
	}
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	infra := &recInfra{}
	h := &Harness{Root: root, Config: cfg, Adapter: golang.Adapter{}, Runtime: golang.Runtime{}, Infra: infra, Now: time.Now}
	ctx := context.Background()

	var upTo []string
	for i, st := range f.steps {
		upTo = append(upTo, st.id)
		// Trước code: schema đã thiết kế (migration chính + nháp) như submit cases; case của bước phải FAILED
		// (không ERROR), case của bước trước vẫn pass.
		out := outDir(t)
		res, err := h.Run(ctx, Options{Feature: f.key, Steps: upTo, Mode: ModeCasesWithDrafts, Out: out})
		if err != nil {
			t.Fatal(err)
		}
		failed := false
		for _, c := range res.Cases {
			switch {
			case c.Outcome == run.CaseOutcomeError:
				t.Errorf("%s trước code %s: %s ERROR %s\nlog:\n%s", f.key, st.id, c.ID, c.Message, readLog(t, out))
			case c.Step != st.id && c.Outcome != run.CaseOutcomePassed:
				t.Errorf("%s trước code %s: case %s của bước trước %s: %s", f.key, st.id, c.ID, c.Outcome, c.Message)
			case c.Step == st.id && c.Outcome == run.CaseOutcomeFailed:
				failed = true
			}
		}
		if !failed {
			t.Fatalf("%s: không case nào của %s fail trước khi code: %v", f.key, st.id, rows(res.Cases))
		}

		copyTree(t, filepath.Join(sol, st.dir), root)
		if i == len(f.steps)-1 {
			break
		}
		// step finish: case tới bước này, mode cases (chỉ migration chính)
		out = outDir(t)
		res, err = h.Run(ctx, Options{Feature: f.key, Steps: upTo, Mode: ModeCases, Out: out})
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("%s sau code %s: exit %d, %v\nlog:\n%s", f.key, st.id, res.ExitCode, rows(res.Cases), readLog(t, out))
		}
	}

	// Bước cuối như eval: build, unit test, mọi case
	out := outDir(t)
	vr, err := h.Verify(ctx, f.key, out)
	if err != nil {
		t.Fatal(err)
	}
	if !vr.Build.OK || vr.Unit.Exit != 0 || vr.HarnessErr != "" || vr.TimedOut {
		t.Fatalf("%s verify: %+v\nlog:\n%s", f.key, vr, readLog(t, out))
	}
	for _, c := range vr.Run.Cases {
		if c.Outcome != run.CaseOutcomePassed {
			t.Errorf("%s sau bước cuối: %s %s %s\nlog:\n%s", f.key, c.ID, c.Outcome, c.Message, readLog(t, out))
		}
	}
	if len(vr.Run.Cases) == 0 || !slices.ContainsFunc(vr.Run.Cases, func(c run.CaseResult) bool { return c.Step == upTo[0] }) {
		t.Errorf("%s: thiếu case: %v", f.key, rows(vr.Run.Cases))
	}
	infra.requireCleaned(t)
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// copyTree chép mọi file của src vào dst (ghi đè), như `cp -r src/. dst/`.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		copyFile(t, p, filepath.Join(dst, rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
