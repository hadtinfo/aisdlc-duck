package risk

import "testing"

func TestGlobOverlap(t *testing.T) {
	cases := []struct {
		name       string
		rule, scop string
		want       bool
	}{
		{"middleWildcardsOverlap", "**/accounting/**", "src/main/java/**/accounting/*.java", true},
		{"differentDirectoriesDoNotOverlap", "**/accounting/**", "src/main/java/**/fee/**", false},
		{"openScopeUnderPrefixOverlapsLeadingWildcardRule", "**/report/**", "src/**", true},
		{"singleStarDoesNotCrossDirectories", "src/*.java", "src/a/b.java", false},
		{"concretePathAgainstGlob", "src/**/*.java", "src/a/b.java", true},
		{"blank", "", "src/**", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Overlaps(c.rule, c.scop, "AisdlcX.java"); got != c.want {
				t.Errorf("Overlaps(%q,%q) = %v", c.rule, c.scop, got)
			}
		})
	}
}
