package samples

import (
	"reflect"
	"testing"

	"example.com/acme/gen/domain/acme"
)

// Pure checks the examples of the pure functions (kind "pure"), written by
// hand from their rules.
func Pure(t *testing.T, f acme.Funcs) {
	t.Helper()
	t.Run("new_invoice", func(t *testing.T) {
		want := acme.Invoice{Id: "i2", Customer: "c2", Currency: acme.CurrencyUsd}
		if got := f.NewInvoice("i2", "c2", acme.CurrencyUsd); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})
	t.Run("total", func(t *testing.T) {
		for _, x := range []struct {
			lines []acme.Line
			want  int64
		}{
			{nil, 0},
			{[]acme.Line{line(1, 7)}, 7},
			{[]acme.Line{line(2, 500), line(3, 7)}, 1021},
		} {
			if got := f.Total(acme.Invoice{Lines: x.lines}); got != x.want {
				t.Errorf("total of %v = %d, want %d", x.lines, got, x.want)
			}
		}
	})
}
