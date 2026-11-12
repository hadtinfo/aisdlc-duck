package snapshot

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/i18n"
	"aisdlc/internal/spec"
)

func sampleGateState() FeatureGateState {
	t := time.Date(2026, 11, 4, 2, 5, 0, 0, time.UTC)
	return FeatureGateState{
		Repo: "repo1", Feature: "PAY-123", Mode: "web", IssuedAt: t,
		Submissions: []Sub{{"BRD", "h1", t, []spec.Finding{{Code: "X", Message: "ế"}}}},
		Approvals:   []Appr{{Gate: "BRD", Hash: "h1", Approver: "an", At: t, EvaluatedTree: "T", Channel: "auto", Rule: "flow:LOW"}},
		Feedback:    []Fb{{1, "BRD", "REJECTED", "an", t, "sai"}},
	}
}

func TestGateStateRoundTrip(t *testing.T) {
	g := sampleGateState()
	b, err := g.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := FeatureGateStateFromJSON(b)
	if err != nil || !reflect.DeepEqual(got, g) {
		t.Fatalf("%v\n%+v\n%+v", err, got, g)
	}
	if strings.Contains(string(b), "superseded") {
		t.Errorf("superseded rỗng phải bị bỏ: %s", b)
	}
}

func TestGateStateSupersededAndOldPayload(t *testing.T) {
	g := sampleGateState()
	g.Superseded = []Appr{g.Approvals[0]}
	b, _ := g.ToJSON()
	if !strings.Contains(string(b), `"superseded":[`) {
		t.Errorf("%s", b)
	}
	old := `{"repo":"r","feature":"K-1","mode":"web","issuedAt":"2026-11-04T02:05:00Z","submissions":[],"approvals":[{"gate":"BRD","hash":"h","approver":"a","at":"2026-11-04T02:05:00Z","evaluatedTree":"T","channel":"web"}],"feedback":[],"extra":1}`
	got, err := FeatureGateStateFromJSON([]byte(old))
	if err != nil || len(got.Superseded) != 0 || got.Approvals[0].Rule != "" {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := FeatureGateStateFromJSON([]byte("{nope")); err == nil {
		t.Error("phải lỗi")
	}
}

func TestToJSONInExplicitLocale(t *testing.T) {
	// ToJSONIn không phụ thuộc locale mặc định; nội dung JSON như ToJSON.
	g := FeatureGateState{Repo: "r", Feature: "K-1"}
	a, err := g.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	b, err := g.ToJSONIn(i18n.EN)
	if err != nil || string(a) != string(b) {
		t.Fatalf("ToJSONIn(EN) = %s, %v; muốn %s", b, err, a)
	}
	c, err := FeatureControl{Repo: "r", Feature: "K-1"}.ToJSONIn(i18n.EN)
	if err != nil || len(c) == 0 {
		t.Fatalf("FeatureControl.ToJSONIn(EN) = %s, %v", c, err)
	}
}
