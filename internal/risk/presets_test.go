package risk

import (
	"reflect"
	"strings"
	"testing"
)

func latest(t *testing.T, name string) RiskPolicy {
	t.Helper()
	p, err := LatestPreset(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBothPresetsParse(t *testing.T) {
	if !reflect.DeepEqual(PresetNames(), []string{"banking", "generic"}) {
		t.Errorf("names = %v", PresetNames())
	}
	for _, n := range PresetNames() {
		if len(latest(t, n).Levels) == 0 {
			t.Errorf("%s không có mức", n)
		}
	}
}

func TestGenericHasNoBusinessRules(t *testing.T) {
	labels := map[string]bool{"money-movement": true, "schema-change": true, "auth": true, "external-api": true}
	for _, r := range latest(t, "generic").Rules {
		if r.Kind != MatchLabel {
			t.Errorf("%s: kind %s", r.ID, r.Kind)
		}
		for _, m := range r.Match {
			if !labels[m] {
				t.Errorf("%s: nhãn lạ %s", r.ID, m)
			}
		}
	}
}

func TestBankingExtendsGeneric(t *testing.T) {
	banking := latest(t, "banking")
	for _, g := range latest(t, "generic").Rules {
		found := false
		for _, b := range banking.Rules {
			if reflect.DeepEqual(b, g) {
				found = true
			}
		}
		if !found {
			t.Errorf("banking thiếu rule %s của generic", g.ID)
		}
	}
	hasKeyword := false
	for _, r := range banking.Rules {
		hasKeyword = hasKeyword || r.Kind == MatchKeyword
	}
	if !hasKeyword {
		t.Error("banking phải có rule keyword")
	}
}

func TestPresetRefMatchesFile(t *testing.T) {
	ref := latest(t, "banking").Preset
	if ref == nil || ref.Name != "banking" || ref.Version != "2" {
		t.Errorf("ref = %+v", ref)
	}
	if PresetLatestVersion("generic") != "2" {
		t.Error("generic phải ở phiên bản 2")
	}
}

func TestPresets_v2_haveFlow_v1_unchanged(t *testing.T) {
	for _, n := range PresetNames() {
		v2, err := LoadPreset(n, "2")
		if err != nil {
			t.Fatal(err)
		}
		if v2.Preset.Version != "2" {
			t.Errorf("%s: version %s", n, v2.Preset.Version)
		}
		for _, u := range FlowUnits {
			if v2.FlowFor("low", u) != "auto" || v2.FlowFor("high", u) != "manual" {
				t.Errorf("%s: low/high %s", n, u)
			}
		}
		for u, want := range map[string]string{"brd": "manual", "design": "manual", "cases": "manual", "step": "auto"} {
			if v2.FlowFor("medium", u) != want {
				t.Errorf("%s: medium %s", n, u)
			}
		}
		v1, err := LoadPreset(n, "1")
		if err != nil {
			t.Fatal(err)
		}
		if len(v1.Flow) != 0 {
			t.Errorf("%s v1 có flow", n)
		}
		y, _ := PresetYAML(n, "1")
		if strings.Contains(y, "flow") {
			t.Errorf("%s v1 yaml chứa flow", n)
		}
		if !reflect.DeepEqual(v2.Rules, v1.Rules) || !reflect.DeepEqual(v2.Requirements, v1.Requirements) {
			t.Errorf("%s: v2 đổi rules/requirements", n)
		}
	}
}

func TestPresets_errors(t *testing.T) {
	if _, err := PresetYAML("nope", "1"); err == nil || !strings.Contains(err.Error(), "Không có mẫu 'nope'; các mẫu có sẵn: [banking, generic]") {
		t.Errorf("err = %v", err)
	}
	if _, err := PresetYAML("banking", "99"); err == nil || !strings.Contains(err.Error(), "Không có mẫu 'banking' phiên bản '99'") {
		t.Errorf("err = %v", err)
	}
	if _, err := PresetYAML("banking", "../1"); err == nil {
		t.Error("version có ký tự lạ phải lỗi")
	}
}

// Băm do bản Java tính (RiskPolicy.hash) cho cùng file mẫu: hai bản phải ra cùng chữ ký.
func TestPresetHashesMatchJava(t *testing.T) {
	want := map[string]string{
		"banking/1": "sha256:9d4ed03ca4b698415788b5fd1a7168b5ce90e659bd5b99d7dce8208b31026e9d",
		"banking/2": "sha256:c8a78ba12debaa4f861b8ddc409ad353e96bae99fa42774b5366e5382e5748e2",
		"generic/1": "sha256:df2dc7ccb609a14f4c49d309db0ddbf0d970416aa1c746aba8bb843b71a50300",
		"generic/2": "sha256:40dcc55640ceddbd59b8885d815b33a683407b46744014862af35f5e5244beea",
	}
	for k, h := range want {
		parts := strings.Split(k, "/")
		p, err := LoadPreset(parts[0], parts[1])
		if err != nil {
			t.Fatal(err)
		}
		if p.Hash() != h {
			t.Errorf("%s: %s, want %s", k, p.Hash(), h)
		}
	}
}
