package gate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/model"
)

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newPaths(t *testing.T, key string) model.FeaturePaths {
	t.Helper()
	return model.FeaturePaths{RepoRoot: t.TempDir(), Key: model.FeatureKey(key)}
}

var allGates = []Gate{BRD, DESIGN, CASES, REVIEW}

var t0 = time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)

func rels(p model.FeaturePaths, files []string) []string {
	out := []string{}
	for _, f := range files {
		out = append(out, p.Rel(f))
	}
	return out
}

func TestArtifactContentsMatchArtifacts(t *testing.T) {
	p := newPaths(t, "PAY-1")
	write(t, p.Brd(), "# BRD\n")
	write(t, p.DesignMd(), "# D\n")
	write(t, p.DesignYaml(), "steps: []\n")
	write(t, filepath.Join(p.DbDrafts(), "V1__a.sql"), "a")
	write(t, p.Cases(), "cases: []\n")
	write(t, p.SchemaExpected(), "{}\n")
	write(t, p.ReviewAgent(), "{}")
	// file suite test (nếu có) không ảnh hưởng
	write(t, filepath.Join(p.RepoRoot, "src/test/java/x/AisdlcCasesTest.java"), "class AisdlcCasesTest {}\n")
	for _, flow := range []model.Flow{model.FlowFull, model.FlowLite} {
		for _, g := range allGates {
			contents, err := g.ArtifactContents(p, flow)
			if err != nil {
				t.Fatal(err)
			}
			var keys []string
			for _, c := range contents {
				keys = append(keys, c.Path)
			}
			if !reflect.DeepEqual(keys, rels(p, g.Artifacts(p, flow))) {
				t.Errorf("%s %s: %v", flow, g, keys)
			}
			want, _ := model.SHA256Files(p.RepoRoot, g.Artifacts(p, flow))
			if model.SHA256Contents(contents) != want {
				t.Errorf("%s %s: hash khác", flow, g)
			}
		}
	}
	// file còn thiếu (report.json) thì Data == nil
	c, _ := REVIEW.ArtifactContents(p, model.FlowFull)
	if c[0].Data != nil {
		t.Errorf("report.json phải thiếu")
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestCasesArtifactsIncludeRisk(t *testing.T) {
	p := newPaths(t, "PAY-2")
	write(t, p.Cases(), "cases: []\n")
	hash := func(f model.Flow) string {
		c, err := CASES.ArtifactContents(p, f)
		if err != nil {
			t.Fatal(err)
		}
		return model.SHA256Contents(c)
	}
	for _, flow := range []model.Flow{model.FlowFull, model.FlowLite} {
		before := hash(flow)
		if contains(CASES.Artifacts(p, flow), p.Risk()) {
			t.Fatal("chưa có risk.yaml")
		}
		c, _ := CASES.ArtifactContents(p, flow)
		for _, e := range c {
			if strings.HasSuffix(e.Path, "risk.yaml") {
				t.Fatal("không có mục risk")
			}
		}
		write(t, p.Risk(), "level: high\n")
		if !contains(CASES.Artifacts(p, flow), p.Risk()) {
			t.Fatal("phải có risk.yaml")
		}
		if hash(flow) == before {
			t.Fatal("hash phải đổi")
		}
		write(t, p.Risk(), "level: low\n")
		os.Remove(p.Risk())
		if hash(flow) != before {
			t.Fatal("hash phải về như cũ")
		}
	}
	for _, g := range []Gate{BRD, DESIGN, REVIEW} {
		write(t, p.Risk(), "level: high\n")
		if contains(g.Artifacts(p, model.FlowFull), p.Risk()) {
			t.Errorf("%s không gồm risk.yaml", g)
		}
		os.Remove(p.Risk())
	}
}

func TestArtifactsPerGate(t *testing.T) {
	p := newPaths(t, "PAY-123")
	write(t, filepath.Join(p.DbDrafts(), "V2__b.sql"), "b")
	write(t, filepath.Join(p.DbDrafts(), "V1__a.sql"), "a")
	// CASES không phụ thuộc file suite test
	write(t, filepath.Join(p.RepoRoot, "src/test/java/com/x/AisdlcCasesTest.java"), "class AisdlcCasesTest {}")
	eq := func(name string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v want %v", name, got, want)
		}
	}
	eq("design", DESIGN.Artifacts(p, model.FlowFull), []string{p.DesignMd(), p.DesignYaml(),
		filepath.Join(p.DbDrafts(), "V1__a.sql"), filepath.Join(p.DbDrafts(), "V2__b.sql"), p.SchemaExpected()})
	eq("cases full", CASES.Artifacts(p, model.FlowFull), []string{p.Cases()})
	eq("cases lite", CASES.Artifacts(p, model.FlowLite), []string{p.Cases(), p.SchemaExpected()})
	eq("review", REVIEW.Artifacts(p, model.FlowFull), []string{filepath.Join(p.EvalDir(), "report.json"), p.ReviewAgent()})
}

func TestCasesDoNotDependOnSuiteFiles(t *testing.T) {
	p := newPaths(t, "PAY-3")
	write(t, p.Cases(), "cases: []\n")
	for _, flow := range []model.Flow{model.FlowFull, model.FlowLite} {
		before := CASES.Artifacts(p, flow)
		h1, _ := model.SHA256Files(p.RepoRoot, before)
		write(t, filepath.Join(p.RepoRoot, "mod-a/src/test/java/p/AisdlcCasesTest.java"), "x")
		write(t, filepath.Join(p.RepoRoot, "mod-b/src/test/java/p/AisdlcCasesTest.java"), "y")
		after := CASES.Artifacts(p, flow)
		h2, _ := model.SHA256Files(p.RepoRoot, after)
		if !reflect.DeepEqual(before, after) || h1 != h2 {
			t.Errorf("%s: CASES không được phụ thuộc file suite: %v", flow, after)
		}
	}
}
