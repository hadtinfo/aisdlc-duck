package risk

import (
	"reflect"
	"testing"
)

func TestMigrateKeywordRule(t *testing.T) {
	p, err := LoadPreset("banking", "2")
	if err != nil {
		t.Fatal(err)
	}
	m, err := MigrateToV2(p)
	if err != nil {
		t.Fatal(err)
	}
	var r1 RiskRule
	for _, r := range m.Rules {
		if r.ID == "R1" {
			r1 = r
		}
	}
	if r1.Describe != "Mọi thay đổi chạm sổ cái (từ khóa cũ: hạch toán|bút toán|định khoản)" || r1.Level != "high" {
		t.Errorf("R1 = %+v", r1)
	}
	if m.Schema != 2 || m.Classification.Agent != "" || m.Preset != nil {
		t.Errorf("schema=%d agent=%q preset=%v", m.Schema, m.Classification.Agent, m.Preset)
	}
	if r1.Kind != "" || len(r1.Match) != 0 || r1.Note != "" {
		t.Errorf("còn dữ liệu schema 1: %+v", r1)
	}
}

func TestMigrateKindSuffixes(t *testing.T) {
	cases := []struct {
		r    RiskRule
		want string
	}{
		{RiskRule{ID: "A", Kind: MatchPath, Match: []string{"a/**", "b/**"}, Note: "N"}, "N (đường dẫn cũ: a/**, b/**)"},
		{RiskRule{ID: "A", Kind: MatchTable, Match: []string{"t_*"}, Note: "N"}, "N (bảng cũ: t_*)"},
		{RiskRule{ID: "A", Kind: MatchEndpoint, Match: []string{"POST /x"}, Note: "N"}, "N (endpoint cũ: POST /x)"},
		{RiskRule{ID: "A", Kind: MatchLabel, Match: []string{"auth"}, Note: "N"}, "N (nhãn cũ: auth)"},
		{RiskRule{ID: "A", Kind: MatchLabel, Match: []string{"auth"}}, "nhãn cũ: auth"},
	}
	for _, c := range cases {
		c.r.Level, c.r.Enabled = "high", true
		p := RiskPolicy{Schema: 1, Levels: []string{"low", "high"}, DefaultLevel: "low", Rules: []RiskRule{c.r},
			Classification: DefaultClassification()}
		m, err := MigrateToV2(p)
		if err != nil {
			t.Fatal(err)
		}
		if m.Rules[0].Describe != c.want {
			t.Errorf("got %q want %q", m.Rules[0].Describe, c.want)
		}
	}
}

func TestMigrateDropsFlowBrd(t *testing.T) {
	p, _ := LoadPreset("generic", "2")
	if _, ok := p.Flow["low"]["brd"]; !ok {
		t.Fatal("v2 phải có flow.brd")
	}
	m, err := MigrateToV2(p)
	if err != nil {
		t.Fatal(err)
	}
	for level, f := range m.Flow {
		if _, ok := f["brd"]; ok {
			t.Errorf("%s còn brd", level)
		}
	}
	if m.Flow["low"]["design"] != "auto" {
		t.Errorf("flow = %v", m.Flow)
	}
	if _, ok := p.Flow["low"]["brd"]; !ok {
		t.Error("MigrateToV2 sửa chính sách đầu vào")
	}
}

func TestMigrateIdempotentOnSchema2(t *testing.T) {
	p := latest(t, "banking")
	m, err := MigrateToV2(p)
	if err != nil || !reflect.DeepEqual(m, p) {
		t.Errorf("err=%v, đổi chính sách schema 2", err)
	}
}

func TestPresetV3IsSchema2(t *testing.T) {
	for _, n := range PresetNames() {
		v3, v2 := latest(t, n), mustPreset(t, n, "2")
		if v3.Schema != 2 || v3.Usable() != nil {
			t.Errorf("%s: schema %d, usable %v", n, v3.Schema, v3.Usable())
		}
		if len(v3.Rules) != len(v2.Rules) {
			t.Fatalf("%s: số rule", n)
		}
		for i, r := range v3.Rules {
			if r.ID != v2.Rules[i].ID || r.Level != v2.Rules[i].Level || r.Describe == "" {
				t.Errorf("%s: rule %d = %+v", n, i, r)
			}
		}
	}
}

func mustPreset(t *testing.T, n, v string) RiskPolicy {
	p, err := LoadPreset(n, v)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
