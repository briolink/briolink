package strong

import "testing"

func TestSettlesBalance(t *testing.T) {
	for _, c := range []struct {
		amount, balance int
		want            bool
	}{
		{1, 1, true},
		{100, 100, true},
		{0, 0, false},
		{-1, -1, false},
		{1, 2, false},
		{2, 1, false},
		{0, 1, false},
	} {
		if got := settles_balance(c.amount, c.balance); got != c.want {
			t.Errorf("settles_balance(%d, %d) = %v, want %v", c.amount, c.balance, got, c.want)
		}
	}
}

func TestLabel(t *testing.T) {
	for amount, want := range map[int]string{-1: "", 0: "", 1: "payment", 5: "payment"} {
		if got := label(amount); got != want {
			t.Errorf("label(%d) = %q, want %q", amount, got, want)
		}
	}
}

func TestTotal(t *testing.T) {
	sum, positive := total([]int{4, -1, 0, 1, 2})
	if sum != 6 || positive != 3 {
		t.Errorf("total = %d, %d; want 6, 3", sum, positive)
	}
}
