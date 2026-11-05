package policy

import (
	"reflect"
	"testing"
)

func parse(t *testing.T, s string, want *DevCommand) {
	t.Helper()
	got, ok := ParseDevCommand(s)
	if want == nil {
		if ok {
			t.Errorf("%q: want empty, got %+v", s, got)
		}
		return
	}
	if !ok || got.Verb != want.Verb || !reflect.DeepEqual(got.Args, want.Args) {
		t.Errorf("%q: got %+v (%v), want %+v", s, got, ok, *want)
	}
}

func TestParsesSimple(t *testing.T) {
	parse(t, "aisdlc approve brd", &DevCommand{"approve", []string{"brd"}})
}

func TestParsesTwoWordVerb(t *testing.T) {
	parse(t, "aisdlc step accept S1", &DevCommand{"step accept", []string{"S1"}})
	c, _ := ParseDevCommand("aisdlc step accept S1")
	if c.Key() != "step accept S1" {
		t.Error(c.Key())
	}
}

func TestStripsQuotes(t *testing.T) {
	parse(t, "aisdlc reject brd -m 'thiếu số'", &DevCommand{"reject", []string{"brd", "-m", "thiếu số"}})
}

func TestParsesRiskSet(t *testing.T) {
	parse(t, "aisdlc risk set high", &DevCommand{"risk set", []string{"high"}})
}

func TestCompoundIsEmpty(t *testing.T) {
	for _, c := range []string{"A=aisdlc; $A approve brd", "bash -c \"aisdlc approve brd\"", "cd x && aisdlc approve brd",
		"aisdlc approve brd | cat", "aisdlc approve brd || true", "aisdlc approve $(echo brd)", "X=1 aisdlc approve brd",
		"eval aisdlc approve brd", "aisdlc status"} {
		parse(t, c, nil)
	}
}

func TestLauncherPathIsEmptyButMentioned(t *testing.T) {
	parse(t, "/p/bin/aisdlc step accept S1", nil)
	parse(t, "./aisdlc approve brd", nil)
	eq(t, "m1", DevCommandMentions("/p/bin/aisdlc step accept S1"), true)
	eq(t, "m2", DevCommandMentions("./aisdlc approve brd"), true)
}

func TestShellExpansionIsEmpty(t *testing.T) {
	for _, c := range []string{
		"aisdlc approve \"br\"d", "aisdlc approve brd\\ x", "aisdlc approve $X",
		"aisdlc approve ${X}", "aisdlc approve $'x'", "aisdlc approve b*", "aisdlc approve {a,b}",
		"aisdlc approve ~/x", "aisdlc approve 'brd", "aisdlc approve \"brd",
		"aisdlc ap\"\"prove brd", "aisdlc approve b?d", "aisdlc approve b[r]d",
		"aisdlc approve brd #x", "aisdlc approve (brd)"} {
		parse(t, c, nil)
	}
}

func TestParseImpliesMentions(t *testing.T) {
	for _, c := range []string{
		"aisdlc approve brd", "aisdlc step accept S1", "aisdlc reject brd -m 'thiếu số'",
		"aisdlc risk set high", "A=aisdlc; $A approve brd", "bash -c \"aisdlc approve brd\"",
		"cd x && aisdlc approve brd", "aisdlc approve brd | cat", "aisdlc status",
		"aisdlc \"approve\" brd", "\"aisdlc\" approve brd", "aisdlc ap\"\"prove brd",
		"X=1 aisdlc approve brd", "/p/bin/aisdlc step accept S1"} {
		if _, ok := ParseDevCommand(c); ok {
			eq(t, c, DevCommandMentions(c), true)
		}
	}
}

func TestMentionsQuotedForms(t *testing.T) {
	eq(t, "1", DevCommandMentions("aisdlc \"approve\" brd"), true)
	eq(t, "2", DevCommandMentions("\"aisdlc\" approve brd"), true)
	eq(t, "3", DevCommandMentions("aisdlc ap\"\"prove brd"), true)
	parse(t, "aisdlc \"approve\" brd", &DevCommand{"approve", []string{"brd"}})
}

func TestMentionsAllForms(t *testing.T) {
	for _, c := range []string{"aisdlc approve brd", "A=aisdlc; $A approve brd; aisdlc approve brd",
		"bash -c \"aisdlc approve brd\"", "cd x && aisdlc reopen brd", "aisdlc approve brd | cat", "aisdlc risk set high"} {
		eq(t, c, DevCommandMentions(c), true)
	}
	eq(t, "status", DevCommandMentions("aisdlc status"), false)
}
