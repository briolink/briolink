package untested

import "testing"

func TestTallyAmount(t *testing.T) {
	if got, _ := tally(3); got != 3 {
		t.Errorf("tally(3) amount = %d, want 3", got)
	}
}
