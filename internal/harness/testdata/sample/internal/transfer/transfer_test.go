package transfer

import "testing"

func TestSufficient(t *testing.T) {
	if !Sufficient(1000, 1000) || Sufficient(10, 100) {
		t.Fatal("Sufficient sai")
	}
}
