package fee

import "testing"

func TestFee(t *testing.T) {
	for amount, want := range map[int64]int64{0: 0, 1_000_000: 1500, 9999: 14} {
		if got := Fee(amount); got != want {
			t.Errorf("Fee(%d) = %d, want %d", amount, got, want)
		}
	}
}
