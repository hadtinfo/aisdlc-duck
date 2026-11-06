package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestFromBuildInfo(t *testing.T) {
	set := func(kv ...string) *debug.BuildInfo {
		bi := &debug.BuildInfo{}
		for i := 0; i < len(kv); i += 2 {
			bi.Settings = append(bi.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
		}
		return bi
	}
	for _, c := range []struct {
		bi   *debug.BuildInfo
		want string
	}{
		{nil, Dev},
		{set(), Dev},
		{set("vcs.revision", "abc123", "vcs.modified", "false"), "abc123"},
		{set("vcs.modified", "true", "vcs.revision", "abc123"), "abc123+dirty"},
	} {
		if got := FromBuildInfo(c.bi); got != c.want {
			t.Errorf("FromBuildInfo(%v) = %q, muốn %q", c.bi, got, c.want)
		}
	}
}

func TestVersionPrefersLdflags(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "v1.2.3"
	if got := Version(); got != "v1.2.3" {
		t.Errorf("Version() = %q", got)
	}
}

func TestCompatible(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"abc", "abc", true},
		{"abc", "def", false},
		{"abc", "", false},
		{"", "", false},
		{Dev, "abc", true},
		{"abc", Dev, true},
		{Dev, "", true},
	} {
		if got := Compatible(c.a, c.b); got != c.want {
			t.Errorf("Compatible(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
