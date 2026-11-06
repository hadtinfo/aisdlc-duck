package store

import "testing"

func TestPoolConfigDefaultUser(t *testing.T) {
	t.Setenv("PGUSER", "")
	for _, c := range []struct{ url, user, want string }{
		{"postgres://localhost:5432/aisdlc", "", "aisdlc"},
		{"postgres://localhost:5432/aisdlc", "khac", "khac"},
		{"postgres://u1:pw@localhost:5432/aisdlc", "", "u1"},
		{"postgres://u1@localhost:5432/aisdlc", "u2", "u2"},
	} {
		cfg, err := poolConfig(c.url, c.user, "")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ConnConfig.User != c.want {
			t.Fatalf("%+v: user %q", c, cfg.ConnConfig.User)
		}
	}
}
