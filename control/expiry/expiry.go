// Package expiry is the optional expiry date and rotation reminder that a
// secret or an API token carries, which the daily credential-expiry check and
// the control center's credentials section read (docs/3-hosting.md § Checks).
package expiry

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Dates are a credential's two optional dates.
type Dates struct {
	Expires *time.Time // after it the credential is of no use
	Remind  *time.Time // from it, a rotation reminder is due
}

// Status is where a credential stands on a given day.
type Status string

const (
	OK      Status = "ok"      // no date has come
	Remind  Status = "remind"  // the reminder date has come
	Expired Status = "expired" // the expiry date has come
)

// ErrInvalid: the dates contradict each other.
var ErrInvalid = errors.New("expiry: invalid dates")

// Validate refuses a reminder set after the expiry it is a reminder of.
func (d Dates) Validate() error {
	if d.Expires != nil && d.Remind != nil && d.Remind.After(*d.Expires) {
		return fmt.Errorf("%w: the reminder (%s) is after the expiry (%s)", ErrInvalid, d.Remind.UTC().Format(time.DateOnly), d.Expires.UTC().Format(time.DateOnly))
	}
	return nil
}

// At is the status at now. A credential is expired from its expiry instant.
func (d Dates) At(now time.Time) Status {
	switch {
	case d.Expires != nil && !now.Before(*d.Expires):
		return Expired
	case d.Remind != nil && !now.Before(*d.Remind):
		return Remind
	}
	return OK
}

// Null is d's date as the database stores it: RFC 3339 text in UTC, or NULL.
func Null(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: Format(t), Valid: true}
}

// Format is the stored text of a time.
func Format(t *time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// Parse reads a stored date; NULL is nil.
func Parse(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return nil, fmt.Errorf("expiry: bad time %q in the database: %w", s.String, err)
	}
	t = t.UTC()
	return &t, nil
}
