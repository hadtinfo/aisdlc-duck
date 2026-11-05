package snapshot

import "testing"

func TestNormalizeRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@gitlab.corp:pay/svc.git":             "gitlab.corp/pay/svc",
		"https://GitLab.Corp/pay/svc.git":         "gitlab.corp/pay/svc",
		"ssh://git@gitlab.corp:2222/pay/svc.git/": "gitlab.corp/pay/svc",
		"https://user:pw@host/a/b/c":              "host/a/b/c",
		"  /local/path  ":                         "/local/path",
	} {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
