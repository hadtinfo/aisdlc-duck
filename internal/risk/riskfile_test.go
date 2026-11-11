package risk

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/model"
)

func highRisk(t *testing.T) (RiskPolicy, RiskFile) {
	t.Helper()
	p := mustPreset(t, "banking", "3")
	return p, FromLLM(p, LLMResult{InputsHash: "h", Level: "high",
		Matched:  []LLMMatch{{Rule: "R1", Evidence: "bút toán", File: "aisdlc/features/PAY-1/brd.md"}},
		Override: &LLMOverride{From: "high", Reason: "r"}, Summary: "s"})
}

func dumpOf(t *testing.T, f RiskFile) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.yaml")
	if err := f.Write(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	return string(b)
}

func TestWriteReadRoundTrip(t *testing.T) {
	p, h := highRisk(t)
	f := h.WithOverride(p, "medium", "dev1", "đã xem", time.Date(2026, 11, 4, 1, 2, 3, 0, time.UTC))
	file := filepath.Join(t.TempDir(), "a", "risk.yaml")
	if err := f.Write(file); err != nil {
		t.Fatal(err)
	}
	back, ok, err := ReadRiskFile(file)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if dumpOf(t, back) != dumpOf(t, f) {
		t.Errorf("khác nhau:\n%s\n%s", dumpOf(t, back), dumpOf(t, f))
	}
	if back.Policy.Hash != p.Hash() || back.Policy.Version != p.Version {
		t.Errorf("policy = %+v", back.Policy)
	}
	if !back.Override.At.Equal(f.Override.At) || back.Override.From != "high" {
		t.Errorf("override = %+v", back.Override)
	}
}

func TestRoundTripWithoutOverrideOrAgent(t *testing.T) {
	p := mustPreset(t, "banking", "3")
	f := FromLLM(p, LLMResult{InputsHash: "h", Level: "low", Summary: "s"})
	file := filepath.Join(t.TempDir(), "risk.yaml")
	if err := f.Write(file); err != nil {
		t.Fatal(err)
	}
	back, ok, err := ReadRiskFile(file)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if back.Agent != nil || back.Override != nil || back.Level != f.Level {
		t.Errorf("back = %+v", back)
	}
	if dumpOf(t, back) != dumpOf(t, f) {
		t.Error("khác nhau")
	}
}

// Byte của risk.yaml đi vào băm cổng CASES: ghi, đọc, ghi lại phải ra cùng byte.
func TestRiskFileRoundTripStable(t *testing.T) {
	p, h := highRisk(t)
	pf := mustParse(t, "schema: 2\nlevels: [low, high]\ndefault: low\nflow:\n  low: { design: auto, step: auto }\n  high: { step: manual }\n"+
		"rules:\n  - { id: R1, level: high, describe: \"x: y\" }\n"+
		"requirements:\n  high:\n    - { id: coverage, min: 90 }\n    - { id: sql-zero, name: n, sql: \"select 1 where 1=0\" }\n")
	pr := FromLLM(pf, LLMResult{InputsHash: "h", Level: "high", Summary: "tóm: tắt\nhai dòng",
		Matched: []LLMMatch{{Rule: "R1", Evidence: "x: y", File: "a.md"}}})
	for name, f := range map[string]RiskFile{
		"banking":  h,
		"override": h.WithOverride(p, "low", "dev", "r: có dấu hai chấm\nnhiều dòng", time.Date(2026, 11, 4, 1, 2, 3, 456, time.UTC)),
		"flow":     pr,
		"flow-low": pr.WithOverride(pf, "low", "d", "r", time.Unix(0, 0).UTC()),
	} {
		file := filepath.Join(t.TempDir(), "risk.yaml")
		if err := f.Write(file); err != nil {
			t.Fatal(err)
		}
		first, _ := os.ReadFile(file)
		back, ok, err := ReadRiskFile(file)
		if err != nil || !ok {
			t.Fatal(name, ok, err)
		}
		if err := back.Write(file); err != nil {
			t.Fatal(err)
		}
		second, _ := os.ReadFile(file)
		if !bytes.Equal(first, second) {
			t.Errorf("%s: byte đổi:\n%s\n---\n%s", name, first, second)
		}
	}
}

func TestReadMissingIsEmpty(t *testing.T) {
	_, ok, err := ReadRiskFile(filepath.Join(t.TempDir(), "nope.yaml"))
	if ok || err != nil {
		t.Errorf("ok=%v err=%v", ok, err)
	}
}

func TestOfCopiesRequirementsOfLevel(t *testing.T) {
	p, f := highRisk(t)
	if f.Level != "high" || f.Override != nil {
		t.Errorf("f = %+v", f)
	}
	if !reflect.DeepEqual(f.Requirements, p.RequirementsFor("high")) {
		t.Errorf("requirements = %+v", f.Requirements)
	}
}

func TestWithOverrideRecomputesRequirements(t *testing.T) {
	p, h := highRisk(t)
	f := h.WithOverride(p, "low", "dev1", "lý do", time.Unix(0, 0))
	if f.Level != "low" || !reflect.DeepEqual(f.Requirements, p.RequirementsFor("low")) {
		t.Errorf("f = %+v", f)
	}
	if f.Override.From != "high" || f.Override.To != "low" || len(f.Matched) == 0 {
		t.Errorf("f = %+v", f)
	}
	if h.Level != "high" || h.Override != nil {
		t.Error("WithOverride làm đổi bản gốc")
	}
}

func TestOverrideKeepsOriginalFrom(t *testing.T) {
	p, h := highRisk(t)
	f := h.WithOverride(p, "low", "d", "r", time.Unix(0, 0)).WithOverride(p, "medium", "d", "r2", time.Unix(0, 0))
	if f.Override.From != "high" {
		t.Errorf("from = %s", f.Override.From)
	}
}

func TestWrittenFileHasNoSurprises(t *testing.T) {
	_, h := highRisk(t)
	s := dumpOf(t, h)
	if !strings.Contains(s, "level:") || !strings.Contains(s, "requirements:") {
		t.Errorf("s = %s", s)
	}
}

func TestRiskFileOldWithoutFlowReadsDefaults(t *testing.T) {
	file := filepath.Join(t.TempDir(), "old.yaml")
	os.WriteFile(file, []byte("level: high\npolicy: { version: 1, hash: h }\nrequirements: []\n"), 0o644)
	f, ok, err := ReadRiskFile(file)
	if err != nil || !ok || len(f.Flow) != 0 {
		t.Errorf("f=%+v ok=%v err=%v", f, ok, err)
	}
}

func TestOfAndOverrideSnapshotFlowOfLevel(t *testing.T) {
	p := mustParse(t, "schema: 1\nlevels: [low, high]\ndefault: low\nflow:\n  low: { brd: auto, step: auto }\n  high: { step: manual }\n")
	f := FromLLM(p, LLMResult{Level: "low"})
	if f.Flow["brd"] != "auto" || f.Flow["step"] != "auto" {
		t.Errorf("flow = %v", f.Flow)
	}
	h := f.WithOverride(p, "high", "dev", "x", time.Unix(0, 0))
	if h.Flow["step"] != "manual" || h.Flow["brd"] != "manual" {
		t.Errorf("flow = %v", h.Flow)
	}
	file := filepath.Join(t.TempDir(), "f.yaml")
	f.Write(file)
	back, _, _ := ReadRiskFile(file)
	if !reflect.DeepEqual(back.Flow, f.Flow) {
		t.Errorf("back = %v", back.Flow)
	}
}

func TestRequiredFor(t *testing.T) {
	dir := t.TempDir()
	w := func(name, s string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(s), 0o644)
		return p
	}
	old, marked := model.FeatureMeta{}, model.FeatureMeta{RiskSchema: 2}
	cases := []struct {
		meta model.FeatureMeta
		path string
		want bool
	}{
		{old, filepath.Join(dir, "none.yaml"), false},
		{old, w("with.yaml", "steps: []\nimpact: { level: high, labels: [], reason: r }\n"), true},
		{old, w("without.yaml", "steps: []\n"), false},
		{old, w("broken.yaml", "steps: [\n"), false},
		{marked, filepath.Join(dir, "none.yaml"), true},
		{marked, w("without2.yaml", "steps: []\n"), true},
	}
	for _, c := range cases {
		if got := RequiredFor(c.meta, c.path); got != c.want {
			t.Errorf("%s (schema %d): got %v", c.path, c.meta.RiskSchema, got)
		}
	}
}

func TestWithOverrideStoresUTC(t *testing.T) {
	p, h := highRisk(t)
	ict := time.FixedZone("ICT", 7*3600)
	f := h.WithOverride(p, "medium", "dev1", "đã xem", time.Date(2026, 11, 4, 8, 2, 3, 0, ict))
	d := dumpOf(t, f)
	if strings.Contains(d, "+07:00") || !strings.Contains(d, "2026-11-04T01:02:03Z") {
		t.Errorf("risk.yaml không phải UTC:\n%s", d)
	}
}

func TestFromLLMRaisesToDefaultAndSnapshots(t *testing.T) {
	p := mustParse(t, "schema: 2\nlevels: [low, medium, high]\ndefault: medium\n"+
		"rules:\n  - { id: R1, level: high, describe: d }\n  - { id: R2, level: low, describe: d }\n")
	f := FromLLM(p, LLMResult{InputsHash: "abc", Level: "low", Summary: "tóm tắt",
		Matched:  []LLMMatch{{Rule: "R2", Evidence: "câu", File: "aisdlc/features/X-1/brd.md"}},
		Override: &LLMOverride{From: "medium", Reason: "hạ"}})
	if f.Level != "medium" {
		t.Errorf("level = %s", f.Level)
	}
	if len(f.Matched) != 1 || f.Matched[0] != (Match{Rule: "R2", Level: "low", On: "“câu” (aisdlc/features/X-1/brd.md)"}) {
		t.Errorf("matched = %+v", f.Matched)
	}
	if f.LLM == nil || f.LLM.Level != "low" || f.LLM.InputsHash != "abc" || f.LLM.Summary != "tóm tắt" ||
		f.LLM.Override == nil || f.LLM.Override.Reason != "hạ" {
		t.Errorf("llm = %+v", f.LLM)
	}
	if f.Agent != nil || strings.Contains(dumpOf(t, f), "agent:") {
		t.Error("risk.yaml mới không ghi agent")
	}
	if f.Policy.Hash != p.Hash() || !reflect.DeepEqual(f.Requirements, p.RequirementsFor("medium")) {
		t.Errorf("f = %+v", f)
	}
}

// risk.yaml cũ (agent của agent chính, không có llm) vẫn đọc được.
func TestLegacyRiskFileWithAgentReads(t *testing.T) {
	file := filepath.Join(t.TempDir(), "old.yaml")
	os.WriteFile(file, []byte("level: high\npolicy: { version: 1, hash: h }\nmatched: []\n"+
		"agent: { level: high, labels: [money-movement], reason: r }\noverride: null\nrequirements: []\n"), 0o644)
	f, ok, err := ReadRiskFile(file)
	if err != nil || !ok || f.Agent == nil || f.Agent.Level != "high" || f.LLM != nil {
		t.Errorf("f=%+v ok=%v err=%v", f, ok, err)
	}
}
