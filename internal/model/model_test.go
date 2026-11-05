package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestFeatureKeyRejectsLowercase(t *testing.T) {
	_, err := ParseFeatureKey("pay-1")
	if err == nil || err.Error() != "Feature key không hợp lệ: pay-1 (ví dụ: PAY-123)" {
		t.Fatalf("err = %v", err)
	}
	k, err := ParseFeatureKey("PAY-123")
	if err != nil || k != "PAY-123" {
		t.Fatalf("k=%q err=%v", k, err)
	}
}

func TestFeaturePathsLayout(t *testing.T) {
	tmp := t.TempDir()
	p := FeaturePaths{RepoRoot: tmp, Key: "PAY-123"}
	cases := map[string]string{
		p.Rel(p.Cases()):          "aisdlc/features/PAY-123/cases.yaml",
		p.Rel(p.DbDrafts()):       "aisdlc/features/PAY-123/db",
		p.Rel(p.Meta()):           "aisdlc/features/PAY-123/feature.yaml",
		p.Rel(p.Brd()):            "aisdlc/features/PAY-123/brd.md",
		p.Rel(p.DesignMd()):       "aisdlc/features/PAY-123/design.md",
		p.Rel(p.DesignYaml()):     "aisdlc/features/PAY-123/design.yaml",
		p.Rel(p.SchemaExpected()): "aisdlc/features/PAY-123/schema-expected.json",
		p.Rel(p.Gates()):          "aisdlc/features/PAY-123/gates.json",
		p.Rel(p.Steps()):          "aisdlc/features/PAY-123/steps.json",
		p.Rel(p.ScopeExtra()):     "aisdlc/features/PAY-123/scope-extra.json",
		p.Rel(p.Feedback()):       "aisdlc/features/PAY-123/feedback.md",
		p.Rel(p.EvalDir()):        "aisdlc/features/PAY-123/eval",
		p.Rel(p.ReviewAgent()):    "aisdlc/features/PAY-123/review-agent.json",
		p.Rel(p.Risk()):           "aisdlc/features/PAY-123/risk.yaml",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
	if !filepath.IsAbs(p.Dir()) {
		t.Errorf("Dir không tuyệt đối: %s", p.Dir())
	}
}

func TestHashChangesWhenContentChanges(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "a.txt")
	os.WriteFile(f, []byte("one"), 0o644)
	h1, _ := SHA256Files(tmp, []string{f})
	os.WriteFile(f, []byte("two"), 0o644)
	h2, _ := SHA256Files(tmp, []string{f})
	if len(h1) != 64 || h1 == h2 {
		t.Fatalf("h1=%s h2=%s", h1, h2)
	}
}

func TestHashOfMissingFileIsStable(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "missing.txt")
	a, err := SHA256Files(tmp, []string{f})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := SHA256Files(tmp, []string{f})
	if a != b {
		t.Fatal("không ổn định")
	}
}

func TestFeatureMetaRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	m := FeatureMeta{Key: "PAY-123", Title: "Chuyển tiền", Flow: FlowLite, BaseTree: "abc123",
		CreatedAt: time.Date(2026, 11, 2, 10, 15, 30, 0, time.UTC), Owner: "an@corp.vn"}
	f := filepath.Join(tmp, "x", "feature.yaml")
	if err := m.Write(f); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f)
	s := string(b)
	if !strings.Contains(s, "key: PAY-123") || !strings.Contains(s, "flow: LITE") ||
		!strings.Contains(s, "2026-11-02T10:15:30Z") {
		t.Fatalf("yaml:\n%s", s)
	}
	got, err := ReadFeatureMeta(f)
	if err != nil {
		t.Fatal(err)
	}
	if got != m || !got.CreatedAt.Equal(m.CreatedAt) {
		t.Fatalf("got %+v want %+v", got, m)
	}
}

func TestReadsLegacyYamlWithoutOwner(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "feature.yaml")
	os.WriteFile(f, []byte("key: \"PAY-1\"\ntitle: \"Cũ\"\nflow: \"LITE\"\nbaseTree: \"abc\"\ncreatedAt: \"2026-11-02T10:15:30Z\"\nlạ: 1\n"), 0o644)
	m, err := ReadFeatureMeta(f)
	if err != nil {
		t.Fatal(err)
	}
	if m.Owner != "" || m.Key != "PAY-1" || m.Flow != FlowLite || m.CreatedAt.Year() != 2026 {
		t.Fatalf("%+v", m)
	}
}

func TestFeatureMetaRejectsBadKey(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "feature.yaml")
	os.WriteFile(f, []byte("key: pay-1\n"), 0o644)
	if _, err := ReadFeatureMeta(f); err == nil || !strings.Contains(err.Error(), "Feature key không hợp lệ") {
		t.Fatalf("err = %v", err)
	}
}

func TestActiveFeatureRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	if _, ok, err := ReadActive(tmp); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := WriteActive(tmp, "PAY-7"); err != nil {
		t.Fatal(err)
	}
	k, ok, err := ReadActive(tmp)
	if !ok || err != nil || k != "PAY-7" {
		t.Fatalf("k=%q ok=%v err=%v", k, ok, err)
	}
	b, _ := os.ReadFile(filepath.Join(tmp, ".aisdlc", "active"))
	if string(b) != "PAY-7\n" {
		t.Fatalf("%q", b)
	}
}

func TestNfcNormalizes(t *testing.T) {
	nfd := "é"
	if NFC(nfd) != "é" {
		t.Fatal("NFC không chuẩn hóa")
	}
}

func TestFlowUnmarshalValidates(t *testing.T) {
	var f Flow
	if err := yaml.Unmarshal([]byte("lite"), &f); err != nil || f != FlowLite {
		t.Errorf("yaml lite: %v %q", err, f)
	}
	if err := json.Unmarshal([]byte(`" full "`), &f); err != nil || f != FlowFull {
		t.Errorf("json full: %v %q", err, f)
	}
	err := yaml.Unmarshal([]byte("BOGUS"), &f)
	if err == nil || !strings.Contains(err.Error(), "BOGUS") {
		t.Errorf("yaml bogus: %v", err)
	}
	err = json.Unmarshal([]byte(`"x"`), &f)
	if err == nil || !strings.Contains(err.Error(), `"x"`) {
		t.Errorf("json bogus: %v", err)
	}
}
