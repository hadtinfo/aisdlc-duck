package step

import (
	"slices"
	"testing"

	"aisdlc/internal/model"
)

func TestScopeExtras_RemoveSteps(t *testing.T) {
	p := model.FeaturePaths{RepoRoot: t.TempDir(), Key: "K-1"}
	x := NewScopeExtras(p)
	for _, e := range [][2]string{{"a", "S2"}, {"b", "S1"}, {"c", ""}} {
		if err := x.Add(e[0], "dev", e[1], t0); err != nil {
			t.Fatal(err)
		}
	}
	if err := x.RemoveSteps([]string{"S2", "S3"}); err != nil {
		t.Fatal(err)
	}
	got, err := x.Paths()
	if err != nil || !slices.Equal(got, []string{"b", "c"}) {
		t.Errorf("got %v err %v", got, err)
	}
}
