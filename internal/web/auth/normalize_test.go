package auth

import "testing"

// Mọi ca của AuthFilter.normalize (Java).
func TestNormalize(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{"/api/board", "/api/board"},
		{"", "/"},
		{"/", "/"},
		{"/api/cli/", "/api/cli"},
		{"/api/cli;x/y", "/api/cli/y"},
		{"/api/;x/cli", "/api/cli"},
		{"/api/login;jsessionid=1", "/api/login"},
		{"/api/%63li/x", "/api/cli/x"},
		{"/api/c%69/x", "/api/ci/x"},
		{"/%61pi/gates/x", "/api/gates/x"},
		{"/api/./gates/x", "/api/gates/x"},
		{"/api/x/../control/y", "/api/control/y"},
		{"/api/%2e%2e/x", "/x"},
		{"/api/x/%2E%2E/control", "/api/control"},
		{"/api/%252e%252e/x", "/api/%2e%2e/x"},
		{"/../../api/x", "/api/x"},
		{"//api/x", "/api/x"},
		{"\\api\\x", "/api/x"},
		{"/api\\cli\\x", "/api/cli/x"},
		{"/api/x/..%2Fci/x", "/api/ci/x"},
		{"/api/x%5C..%5Cci", "/api/ci"},
		{"/api/x;a/..;b/ci", "/api/ci"},
		{"/a+b", "/a+b"},
		{"/a%2Bb", "/a+b"},
		{"/a%20b", "/a b"},
		{"/api/%zz", "/invalid"},
		{"/api/%", "/invalid"},
		{"/api/%a", "/invalid"},
	} {
		if got := Normalize(c.raw); got != c.want {
			t.Errorf("Normalize(%q) = %q, muốn %q", c.raw, got, c.want)
		}
	}
}

func TestAreaOf(t *testing.T) {
	for _, c := range []struct {
		path string
		want Area
	}{
		{"/api/login", AreaLogin},
		{"/api/login/x", AreaLogin},
		{"/api/loginx", AreaOther},
		{"/api/ingest", AreaIngest},
		{"/api/ingest/snapshot", AreaIngest},
		{"/api/ingestx", AreaOther},
		{"/api/cli", AreaCLI},
		{"/api/cli/x", AreaCLI},
		{"/api/clix", AreaOther},
		{"/api/ci", AreaCI},
		{"/api/ci/x", AreaCI},
		{"/api/gates/x", AreaGates},
		{"/api/control", AreaControl},
		{"/api/control/x", AreaControl},
		{"/api/board", AreaOther},
		{"/api/cli;x/y", AreaOther},
		{"/api/./gates/x", AreaOther},
		{"//api/cli/x", AreaOther},
		{"/", AreaOther},
	} {
		if got := AreaOf(c.path); got != c.want {
			t.Errorf("AreaOf(%q) = %v, muốn %v", c.path, got, c.want)
		}
	}
	if AreaGates.String() != "GATES" || AreaOther.String() != "OTHER" {
		t.Fatal(AreaGates.String(), AreaOther.String())
	}
}

func TestJavaWhitespace(t *testing.T) {
	if !javaBlank(" \t \u001f") || javaBlank(" ") || javaStrip("  x  ") != "  x" {
		t.Fatal("isBlank/strip như Java")
	}
	if utf16Len("Bình😀") != 6 {
		t.Fatal(utf16Len("Bình😀"))
	}
}
