package policy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC)

const cmdApproveBrd = `["approve","brd"]`

func issue(t *testing.T, root, session, hash string) {
	t.Helper()
	if err := IssueGrant(root, Grant{SessionID: session, Feature: "PAY-1", Command: cmdApproveBrd, Hash: hash,
		ExpiresAt: t0.Add(GrantTTL)}); err != nil {
		t.Fatal(err)
	}
}

func consume(t *testing.T, root, session, feature, command, hash string, now time.Time) bool {
	t.Helper()
	ok, err := ConsumeGrant(root, session, feature, command, hash, now)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestConsumeOnce(t *testing.T) {
	root := t.TempDir()
	issue(t, root, "s1", "h1")
	if !exists(GrantFile(root, "s1")) {
		t.Fatal("grant file missing")
	}
	eq(t, "first", consume(t, root, "s1", "PAY-1", cmdApproveBrd, "h1", t0.Add(5*time.Second)), true)
	if exists(GrantFile(root, "s1")) {
		t.Error("grant file should be gone")
	}
	eq(t, "second", consume(t, root, "s1", "PAY-1", cmdApproveBrd, "h1", t0.Add(5*time.Second)), false)
}

func TestNullHashMatchesNullOnly(t *testing.T) {
	root := t.TempDir()
	issue(t, root, "s1", "")
	eq(t, "h1", consume(t, root, "s1", "PAY-1", cmdApproveBrd, "h1", t0), false)
	issue(t, root, "s1", "")
	eq(t, "null", consume(t, root, "s1", "PAY-1", cmdApproveBrd, "", t0), true)
}

func TestExpiredAfter60s(t *testing.T) {
	if GrantTTL != 60*time.Second {
		t.Fatal(GrantTTL)
	}
	root := t.TempDir()
	issue(t, root, "s1", "h1")
	eq(t, "60", consume(t, root, "s1", "PAY-1", cmdApproveBrd, "h1", t0.Add(60*time.Second)), false)
	issue(t, root, "s1", "h1")
	eq(t, "59", consume(t, root, "s1", "PAY-1", cmdApproveBrd, "h1", t0.Add(59*time.Second)), true)
}

func TestMismatchSessionFeatureCommandHash(t *testing.T) {
	root := t.TempDir()
	issue(t, root, "s1", "h1")
	eq(t, "session", consume(t, root, "s2", "PAY-1", cmdApproveBrd, "h1", t0), false)
	eq(t, "feature", consume(t, root, "s1", "PAY-2", cmdApproveBrd, "h1", t0), false)
	issue(t, root, "s1", "h1")
	eq(t, "command", consume(t, root, "s1", "PAY-1", `["approve","design"]`, "h1", t0), false)
	issue(t, root, "s1", "h1")
	eq(t, "hash", consume(t, root, "s1", "PAY-1", cmdApproveBrd, "h2", t0), false)
	// lần thử sai đã đốt phiếu
	eq(t, "burnt", consume(t, root, "s1", "PAY-1", cmdApproveBrd, "h1", t0), false)
	eq(t, "empty session", consume(t, root, "", "PAY-1", cmdApproveBrd, "h1", t0), false)
}

func TestClearRemoves(t *testing.T) {
	root := t.TempDir()
	issue(t, root, "s1", "h1")
	issue(t, root, "s2", "h1")
	for _, s := range []string{"s1", "", "never"} {
		if err := ClearGrant(root, s); err != nil {
			t.Fatal(err)
		}
	}
	eq(t, "s1", consume(t, root, "s1", "PAY-1", cmdApproveBrd, "h1", t0), false)
	eq(t, "s2", consume(t, root, "s2", "PAY-1", cmdApproveBrd, "h1", t0), true)
}

func TestSessionIdCannotEscapeGrantsDir(t *testing.T) {
	root := t.TempDir()
	issue(t, root, "../../x", "h1")
	if d := filepath.Dir(GrantFile(root, "../../x")); d != filepath.Join(root, ".aisdlc", "grants") {
		t.Error(d)
	}
	eq(t, "consume", consume(t, root, "../../x", "PAY-1", cmdApproveBrd, "h1", t0), true)
}

func TestGrantFileFieldNames(t *testing.T) {
	root := t.TempDir()
	issue(t, root, "s1", "")
	b, _ := os.ReadFile(GrantFile(root, "s1"))
	want := `{"sessionId":"s1","feature":"PAY-1","command":"[\"approve\",\"brd\"]","hash":null,"expiresAt":"2026-11-05T03:01:00Z"}`
	if string(b) != want {
		t.Errorf("got %s\nwant %s", b, want)
	}
}

func TestGrantKeyIsUnambiguous(t *testing.T) {
	a, _ := ParseDevCommand(`aisdlc reject brd -m "a b"`)
	b, _ := ParseDevCommand(`aisdlc reject brd -m a b`)
	if a.GrantKey() == b.GrantKey() {
		t.Error("same key")
	}
	p, _ := ParseDevCommand("aisdlc approve brd")
	f, ok := DevCommandFromArgs([]string{"approve", "brd"})
	if !ok || p.GrantKey() != f.GrantKey() {
		t.Error(p.GrantKey(), f.GrantKey())
	}
	if f.GrantKey() != `["approve","brd"]` {
		t.Error(f.GrantKey())
	}
	s, _ := DevCommandFromArgs([]string{"step", "accept", "S1"})
	if s.Verb != "step accept" {
		t.Error(s.Verb)
	}
	if _, ok := DevCommandFromArgs([]string{"risk", "reclassify"}); ok {
		t.Error("reclassify")
	}
	if _, ok := DevCommandFromArgs([]string{"status"}); ok {
		t.Error("status")
	}
}
