package policy

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/risk"
)

// Port PolicyServiceTest.

func genericWith(t *testing.T, presetVersion string) risk.RiskPolicy {
	t.Helper()
	latest := risk.PresetLatestVersion("generic")
	y, err := risk.PresetYAML("generic", latest)
	if err != nil {
		t.Fatal(err)
	}
	old := `preset: { name: generic, version: "` + latest + `" }`
	if !strings.Contains(y, old) {
		t.Fatal("mẫu generic không có dòng preset như mong đợi")
	}
	p, err := risk.ParsePolicy(strings.Replace(y, old, `preset: { name: generic, version: "`+presetVersion+`" }`, 1))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func noPreset(t *testing.T) risk.RiskPolicy {
	t.Helper()
	y, _ := risk.PresetYAML("generic", "1")
	p, err := risk.ParsePolicy(regexp.MustCompile(`(?m)^preset:.*\n`).ReplaceAllString(y, ""))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPresetUpdateNullWhenCurrentOrNoPreset(t *testing.T) {
	if u, err := presetUpdate(genericWith(t, risk.PresetLatestVersion("generic"))); err != nil || u != nil {
		t.Fatal(u, err)
	}
	none := noPreset(t)
	if none.Preset != nil {
		t.Fatal("còn preset")
	}
	if u, err := presetUpdate(none); err != nil || u != nil {
		t.Fatal(u, err)
	}
}

func TestPresetUpdateBaseMissingWhenOldVersionNotBundled(t *testing.T) {
	u, err := presetUpdate(genericWith(t, "0"))
	if err != nil || u == nil {
		t.Fatal(u, err)
	}
	if u["name"] != "generic" || u["from"] != "0" || u["to"] != risk.PresetLatestVersion("generic") || u["baseMissing"] != true {
		t.Fatal(u)
	}
	if ch := u["changes"].([]ChangeJSON); len(ch) != 0 { // chính sách sống trùng mẫu mới
		t.Fatal(ch)
	}
	b, _ := json.Marshal(u["policy"]) // bản mẫu mới đầy đủ để UI "Áp dụng vào nháp" từng thay đổi
	if !strings.Contains(string(b), `"G4"`) {
		t.Fatal(string(b))
	}
}

func TestPresetRuleIDsFromBundledPresetVersionElseEmpty(t *testing.T) {
	eq := func(got []string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") || got == nil {
			t.Fatalf("%v != %v", got, want)
		}
	}
	eq(presetRuleIDs(genericWith(t, risk.PresetLatestVersion("generic"))), "G1", "G2", "G3", "G4")
	eq(presetRuleIDs(genericWith(t, "0"))) // phiên bản mẫu không còn đi kèm
	eq(presetRuleIDs(noPreset(t)))
	y, _ := risk.PresetYAML("generic", "1")
	unknown, err := risk.ParsePolicy(strings.Replace(y, "name: generic", "name: acme", 1))
	if err != nil {
		t.Fatal(err)
	}
	eq(presetRuleIDs(unknown))
}

func TestMigrateDraftPure(t *testing.T) {
	y, _ := risk.PresetYAML("generic", "2")
	p, err := risk.ParsePolicy(y)
	if err != nil {
		t.Fatal(err)
	}
	pj, ch, err := migrateDraft(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pj, `"schema":2`) || len(ch) == 0 || ch[0].Kind != "SCHEMA_MIGRATED" {
		t.Fatal(pj, ch)
	}
	if _, _, err := migrateDraft(genericWith(t, risk.PresetLatestVersion("generic"))); err == nil {
		t.Fatal("schema 2 phải báo không cần chuyển")
	}
}
