package yamlx

import (
	"encoding/json"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestJSONValue(t *testing.T) {
	cases := []struct {
		name, in string
		want     any
	}{
		{"date", "2026-11-05", "2026-11-05"},
		{"datetime", "2026-11-05T10:00:00Z", "2026-11-05T10:00:00Z"},
		{"int", "42", 42},
		{"float", "1.5", 1.5},
		{"bool", "true", true},
		{"null", "null", nil},
		{"str", "abc", "abc"},
		{"quoted date", `"2026-11-05"`, "2026-11-05"},
		{"int key", "{1: x, a: 2026-11-05}", map[string]any{"1": "x", "a": "2026-11-05"}},
		{"list", "[1, 2026-11-05, {k: v}]", []any{1, "2026-11-05", map[string]any{"k": "v"}}},
		{"alias", "a: &x {d: 2026-11-05}\nb: *x", map[string]any{"a": map[string]any{"d": "2026-11-05"}, "b": map[string]any{"d": "2026-11-05"}}},
	}
	for _, c := range cases {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(c.in), &doc); err != nil {
			t.Fatal(err)
		}
		got, err := JSONValue(doc.Content[0])
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v want %#v", c.name, got, c.want)
		}
		if _, err := json.Marshal(got); err != nil {
			t.Errorf("%s: marshal: %v", c.name, err)
		}
	}
}

func TestJSONValueNil(t *testing.T) {
	if v, err := JSONValue(nil); err != nil || v != nil {
		t.Errorf("got %v %v", v, err)
	}
}
