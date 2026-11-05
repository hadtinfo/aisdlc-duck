package snapshot

import (
	"reflect"
	"testing"
	"time"
)

func sampleControl() FeatureControl {
	t := time.Date(2026, 11, 4, 2, 5, 0, 0, time.UTC)
	exp := t.Add(60 * time.Second)
	return FeatureControl{
		Repo: "repo1", Feature: "PAY-123", IssuedAt: t,
		Pause:        &Pause{By: "an", At: t},
		Instructions: []Instruction{{1, "an", t, "a"}, {2, "an", t, "ế"}},
		Actions: []Action{
			{ID: 3, Kind: "ACCEPT_STEP", Step: "S1", EndTree: "T", By: "an", At: t, ExpiresAt: &exp},
			{ID: 4, Kind: "ALLOW_SCOPE", Step: "S1", Path: "src/A.java", By: "an", At: t, ExpiresAt: &exp},
		},
	}
}

func TestRoundTrip(t *testing.T) {
	c := sampleControl()
	b, err := c.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := FeatureControlFromJSON(b)
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("%v\n%+v\n%+v", err, got, c)
	}
}

func TestBrokenJsonThrows(t *testing.T) {
	for _, s := range []string{"{nope", "null", ""} {
		if _, err := FeatureControlFromJSON([]byte(s)); err == nil {
			t.Errorf("%q phải lỗi", s)
		}
	}
}

func TestControlJSONShape(t *testing.T) {
	b, _ := FeatureControl{Repo: "r", Feature: "K-1", IssuedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}.ToJSON()
	want := `{"repo":"r","feature":"K-1","issuedAt":"2026-01-02T03:04:05Z","pause":null,"instructions":[],"actions":[]}`
	if string(b) != want {
		t.Fatalf("%s", b)
	}
}
