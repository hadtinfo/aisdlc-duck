package javaish

import "testing"

func TestIsBlankFollowsCharacterIsWhitespace(t *testing.T) {
	for s, want := range map[string]bool{"": true, " \t\n\r\f\v": true, " 　\u001c": true, " ": false,
		" ": false, " ": false, "\u0085": false, " x ": false} {
		if IsBlank(s) != want {
			t.Errorf("%q: %v", s, !want)
		}
	}
	if Trim("\x01 a b　 \x00") != "a b　" {
		t.Fatal(Trim("\x01 a b　 \x00"))
	}
}
