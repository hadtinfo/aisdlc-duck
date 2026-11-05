package risk

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"aisdlc/internal/lang"
	"aisdlc/internal/lang/langs"
	"aisdlc/internal/model"
)

func testFeature(t *testing.T) model.FeaturePaths {
	t.Helper()
	return model.FeaturePaths{RepoRoot: t.TempDir(), Key: "PAY-1"}
}

func goAdapter(t *testing.T) lang.Adapter {
	t.Helper()
	a, err := langs.For("go")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func writeFile(t *testing.T, f, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFullCollectsEverySource(t *testing.T) {
	p := testFeature(t)
	writeFile(t, p.Brd(), "# BRD\n- BR-1: Miễn phí giao dịch\n")
	writeFile(t, p.DesignMd(), "# Design\n## API\n- `POST /fee-waivers` · tạo\n| 201 | ok | {} |\n")
	writeFile(t, p.DesignYaml(), `steps:
  - id: S1
    title: a
    scope: ["src/a/**"]
  - id: S2
    title: b
    scope: ["src/b/*.go", "src/a/**"]
impact:
  level: high
  labels: [money-movement]
  reason: vì sao
`)
	writeFile(t, filepath.Join(p.DbDrafts(), "00009_x.sql"),
		"CREATE TABLE fee_waiver_rule (id BIGINT PRIMARY KEY, Amount NUMERIC(10));")

	in, err := CollectFull(p, "Tiêu đề TÍNH NĂNG", goAdapter(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"Tiêu đề TÍNH NĂNG", "Miễn phí giao dịch", "# Design"} {
		if !strings.Contains(in.Text, s) {
			t.Errorf("text thiếu %q", s)
		}
	}
	if !reflect.DeepEqual(in.Paths, []string{"src/a/**", "src/b/*.go"}) {
		t.Errorf("paths = %v", in.Paths)
	}
	if !reflect.DeepEqual(in.Tables, []string{"fee_waiver_rule", "fee_waiver_rule.id", "fee_waiver_rule.amount"}) {
		t.Errorf("tables = %v", in.Tables)
	}
	if !reflect.DeepEqual(in.Endpoints, []string{"POST /fee-waivers"}) {
		t.Errorf("endpoints = %v", in.Endpoints)
	}
	if !reflect.DeepEqual(in.Labels, []string{"money-movement"}) || in.AgentLevel != "high" {
		t.Errorf("labels/agent = %v %q", in.Labels, in.AgentLevel)
	}
	if in.SampleFile != "aisdlcx.go" {
		t.Errorf("sampleFile = %q", in.SampleFile)
	}
}

func TestFullTablesFollowMigrationOrder(t *testing.T) {
	p := testFeature(t)
	writeFile(t, filepath.Join(p.DbDrafts(), "00010_b.sql"), "CREATE TABLE tb (x INT);")
	writeFile(t, filepath.Join(p.DbDrafts(), "00002_a.sql"), "CREATE TABLE ta (y INT);")
	in, err := CollectFull(p, "T", goAdapter(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.Tables, []string{"ta", "ta.y", "tb", "tb.x"}) {
		t.Errorf("tables = %v", in.Tables)
	}
}

func TestFullToleratesMissingFiles(t *testing.T) {
	in, err := CollectFull(testFeature(t), "T", goAdapter(t))
	if err != nil {
		t.Fatal(err)
	}
	if in.Text != "T" || len(in.Paths) != 0 || len(in.Tables) != 0 || in.AgentLevel != "" {
		t.Errorf("%+v", in)
	}
}

func TestFullBrokenDesignYamlIsError(t *testing.T) {
	p := testFeature(t)
	writeFile(t, p.DesignYaml(), "steps: [unclosed")
	if _, err := CollectFull(p, "T", goAdapter(t)); err == nil {
		t.Error("design.yaml hỏng phải báo lỗi")
	}
}

func TestLiteUsesCasesYaml(t *testing.T) {
	p := testFeature(t)
	writeFile(t, p.Cases(), `scope: ["src/x/**"]
impact: { level: medium, labels: [auth] }
cases:
  - id: C1
    kind: happy
    title: Đăng nhập thành công
`)
	in, err := CollectLite(p, "Tính năng", goAdapter(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(in.Text, "Tính năng") || !strings.Contains(in.Text, "Đăng nhập thành công") {
		t.Errorf("text = %q", in.Text)
	}
	if !reflect.DeepEqual(in.Paths, []string{"src/x/**"}) || len(in.Tables) != 0 || len(in.Endpoints) != 0 {
		t.Errorf("%+v", in)
	}
	if !reflect.DeepEqual(in.Labels, []string{"auth"}) || in.AgentLevel != "medium" {
		t.Errorf("%+v", in)
	}
}

func TestBrdOnlyHasText(t *testing.T) {
	in := CollectBrdOnly("T", "nội dung")
	if !strings.Contains(in.Text, "T") || !strings.Contains(in.Text, "nội dung") || len(in.Paths) != 0 || in.AgentLevel != "" {
		t.Errorf("%+v", in)
	}
}
