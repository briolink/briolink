package weak

import "testing"

// Only far-from-the-boundary cases: amount 0 and amount 1 are never tried.
func TestSettlesBalance(t *testing.T) {
	if !settles_balance(100, 100) {
		t.Error("100 of 100 must settle")
	}
	if settles_balance(100, 50) {
		t.Error("100 of 50 must not settle")
	}
}
