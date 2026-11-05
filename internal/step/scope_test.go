package step

import "testing"

func TestDoubleStarMatchesZeroDirs(t *testing.T) {
	globs := []string{"src/main/java/**/limit/**"}
	cases := map[string]bool{
		"src/main/java/limit/A.java":          true,
		"src/main/java/vn/x/limit/web/B.java": true,
		"src/main/java/vn/x/other/C.java":     false,
	}
	for p, want := range cases {
		if got := ScopeMatches(globs, p); got != want {
			t.Errorf("%s: got %v, want %v", p, got, want)
		}
	}
}

func TestEmptyGlobsMatchNothing(t *testing.T) {
	if ScopeMatches(nil, "a.txt") {
		t.Error("glob rỗng không được khớp")
	}
}

func TestCompiledMatchesSameAsMatches(t *testing.T) {
	globs := []string{"src/main/java/**/limit/**", "**/*.sql", "docs/*.md"}
	compiled := CompileScope(globs)
	for _, p := range []string{"src/main/java/limit/A.java", "src/main/java/vn/x/limit/web/B.java",
		"src/main/java/vn/x/other/C.java", "V1__init.sql", "a/b/V2.sql", "docs/a.md", "docs/x/a.md", "a.txt"} {
		if compiled(p) != ScopeMatches(globs, p) {
			t.Errorf("%s: compiled khác ScopeMatches", p)
		}
	}
	if CompileScope(nil)("a.txt") {
		t.Error("glob rỗng không được khớp")
	}
}

func TestCompileRejectsBadGlobEagerly(t *testing.T) {
	if _, err := CompileScopeErr([]string{"a/[b"}); err == nil {
		t.Error("glob a/[b phải bị từ chối")
	}
	if ScopeMatches([]string{"a/[b"}, "a/[b") {
		t.Error("glob sai cú pháp không được khớp")
	}
}

// Ngoặc vuông là lớp ký tự như java.nio glob: [id] khớp đúng một ký tự i hoặc d, không khớp chữ "[id]".
func TestScopeUnicodeAndBrackets(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"src/**/[id].go", "src/a/b/[id].go", false},
		{"src/**/[id].go", "src/a/b/i.go", true},
		{"src/**/[id].go", "src/a/b/d.go", true},
		{"src/**/[id].go", "src/b/x.go", false},
		{"src/[!a]/x", "src/b/x", true},
		{"src/[!a]/x", "src/a/x", false},
		{"src/[a-c]/x", "src/b/x", true},
		{"src/[a-c]/x", "src/d/x", false},
		{`src/\[id\].go`, "src/[id].go", true},
		{"docs/**", "docs/hướng-dẫn.md", true},
		{"docs/*.md", "docs/hướng-dẫn.md", true},
		{"docs/h?ớng-dẫn.md", "docs/hướng-dẫn.md", true},
		{"a/*.{go,sql}", "a/x.sql", true},
		{"a/*.{go,sql}", "a/x.md", false},
		{"a/*", "a/b/c", false},
	}
	for _, c := range cases {
		if got := ScopeMatches([]string{c.glob}, c.path); got != c.want {
			t.Errorf("%s vs %s: got %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}
