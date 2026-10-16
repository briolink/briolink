package broken

import "testing"

func TestSettlesBalance(t *testing.T) {
	if settles_balance(1, 1) {
		t.Error("wrong on purpose")
	}
}
