package expiry_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/briolink/briolink/control/expiry"
)

func day(s string) *time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func TestStatus(t *testing.T) {
	d := expiry.Dates{Expires: day("2027-10-17"), Remind: day("2027-09-17")}
	for _, c := range []struct {
		now  string
		want expiry.Status
	}{
		{"2027-09-16", expiry.OK},
		{"2027-09-17", expiry.Remind},
		{"2027-10-16", expiry.Remind},
		{"2027-10-17", expiry.Expired},
		{"2028-01-01", expiry.Expired},
	} {
		if got := d.At(*day(c.now)); got != c.want {
			t.Errorf("at %s: %s, want %s", c.now, got, c.want)
		}
	}
	if got := (expiry.Dates{}).At(*day("2099-01-01")); got != expiry.OK {
		t.Errorf("no dates: %s, want ok", got)
	}
	if got := (expiry.Dates{Remind: day("2027-01-01")}).At(*day("2027-01-02")); got != expiry.Remind {
		t.Errorf("reminder only: %s, want remind", got)
	}
}

func TestValidate(t *testing.T) {
	if err := (expiry.Dates{Expires: day("2027-01-01"), Remind: day("2027-02-01")}).Validate(); !errors.Is(err, expiry.ErrInvalid) {
		t.Errorf("reminder after expiry: %v", err)
	}
	for _, d := range []expiry.Dates{{}, {Expires: day("2027-01-01")}, {Remind: day("2027-01-01")}, {Expires: day("2027-01-01"), Remind: day("2027-01-01")}} {
		if err := d.Validate(); err != nil {
			t.Errorf("%+v: %v", d, err)
		}
	}
}

func TestStoredRoundTrip(t *testing.T) {
	in := day("2027-10-17")
	got, err := expiry.Parse(expiry.Null(in))
	if err != nil || got == nil || !got.Equal(*in) {
		t.Fatalf("round trip: %v, %v", got, err)
	}
	if got, err := expiry.Parse(expiry.Null(nil)); err != nil || got != nil {
		t.Fatalf("NULL: %v, %v", got, err)
	}
	if _, err := expiry.Parse(sql.NullString{String: "soon", Valid: true}); err == nil {
		t.Fatal("garbage text was accepted")
	}
}
