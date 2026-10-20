// Package tokens is the control plane's API tokens (A20): each has a name, a
// scope (the one role it may act as) and optional dates. Only a token's
// SHA-256 is kept, so a token is shown once, when it is issued; it can be
// revoked; and every use, and every refusal of a token that was presented, is
// in the audit log. A token never appears in the log or in an error.
package tokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/briolink/briolink/control/expiry"
	"github.com/briolink/briolink/gen/domain/control"
	sqlitegen "github.com/briolink/briolink/gen/sqlite"
)

var (
	// ErrInvalid: the request was refused before it reached the database.
	ErrInvalid = errors.New("tokens: invalid argument")
	// ErrNotFound: no token with this name is active.
	ErrNotFound = errors.New("tokens: not found")
	// ErrDenied: the token is unknown, revoked or expired. The caller learns
	// nothing more; the audit log says which.
	ErrDenied = errors.New("tokens: denied")
)

const prefix = "blt_"

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// Auditor is the audit log. *store.Store is one.
type Auditor interface {
	AddAudit(ctx context.Context, actor, action, subject, detail string) (control.AuditEntry, error)
}

// Service issues and checks tokens.
type Service struct {
	q      *sqlitegen.Queries
	audit  Auditor
	now    func() time.Time
	scopes map[string]bool
}

// New returns a Service over db; scopes are the roles a token may be scoped to.
func New(db sqlitegen.DBTX, audit Auditor, now func() time.Time, scopes []string) *Service {
	s := &Service{q: sqlitegen.New(db), audit: audit, now: now, scopes: map[string]bool{}}
	for _, sc := range scopes {
		s.scopes[sc] = true
	}
	return s
}

// Info is what is known of a token without the token.
type Info struct {
	ID        int64
	Name      string
	Scope     string
	CreatedAt time.Time
	Expiry    expiry.Dates
	RevokedAt *time.Time
}

// Principal is who a valid token acts as.
type Principal struct {
	Name  string
	Scope string
}

// Issue creates a token and returns it. This is the only time it is seen.
func (s *Service) Issue(ctx context.Context, actor, name, scope string, d expiry.Dates) (string, error) {
	switch {
	case !namePattern.MatchString(name):
		return "", fmt.Errorf("%w: a name is 1 to 64 letters, digits, '_', '.' or '-'", ErrInvalid)
	case !s.scopes[scope]:
		return "", fmt.Errorf("%w: scope %q is not a role of this control plane", ErrInvalid, scope)
	}
	if err := d.Validate(); err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := prefix + base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	if _, err := s.q.InsertToken(ctx, name, hash(token), scope, expiry.Format(&now), expiry.Null(d.Expires), expiry.Null(d.Remind)); err != nil {
		if _, ok := s.exists(ctx, name); ok {
			return "", fmt.Errorf("%w: a token named %s exists", ErrInvalid, name)
		}
		return "", fmt.Errorf("tokens: store %s: %w", name, err)
	}
	if err := s.record(ctx, actor, "token.issue", name, "scope "+scope+dateNote(d)); err != nil {
		return "", err
	}
	return token, nil
}

// Revoke ends a token; it is refused from then on.
func (s *Service) Revoke(ctx context.Context, actor, name string) error {
	now := s.now()
	_, err := s.q.RevokeToken(ctx, expiry.Format(&now), name)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return fmt.Errorf("tokens: revoke %s: %w", name, err)
	}
	return s.record(ctx, actor, "token.revoke", name, "")
}

// Authenticate checks a presented token and logs the use, naming what it was
// used for (use: a request line, say). A token that is unknown, revoked or
// expired gets ErrDenied, and the refusal is logged with the reason. Any other
// error, the log's included, also leaves the request unauthenticated: the
// token is not honoured when its use cannot be recorded.
func (s *Service) Authenticate(ctx context.Context, token, use string) (Principal, error) {
	row, err := s.q.GetTokenByHash(ctx, hash(token))
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, s.deny(ctx, "unknown", "unknown token", use)
	}
	if err != nil {
		return Principal{}, fmt.Errorf("tokens: look up: %w", err)
	}
	d, err := readDates(row.ExpiresAt, sql.NullString{})
	if err != nil {
		return Principal{}, err
	}
	switch {
	case row.RevokedAt.Valid:
		return Principal{}, s.deny(ctx, row.Name, "revoked", use)
	case d.At(s.now()) == expiry.Expired:
		return Principal{}, s.deny(ctx, row.Name, "expired", use)
	}
	if err := s.record(ctx, "token:"+row.Name, "token.use", row.Name, "scope "+row.Scope+"; "+use); err != nil {
		return Principal{}, err
	}
	return Principal{Name: row.Name, Scope: row.Scope}, nil
}

// List describes every token, revoked ones included; never a token.
func (s *Service) List(ctx context.Context) ([]Info, error) {
	rows, err := s.q.ListTokens(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(rows))
	for _, r := range rows {
		created, err := expiry.Parse(sql.NullString{String: r.CreatedAt, Valid: true})
		if err != nil {
			return nil, err
		}
		in := Info{ID: r.Id, Name: r.Name, Scope: r.Scope, CreatedAt: *created}
		if in.Expiry, err = readDates(r.ExpiresAt, r.RemindAt); err != nil {
			return nil, err
		}
		if in.RevokedAt, err = expiry.Parse(r.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, nil
}

// Token returns the bearer token of an Authorization header, if it has one.
func Token(header string) (string, bool) {
	t, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || t == "" {
		return "", false
	}
	return t, true
}

func (s *Service) exists(ctx context.Context, name string) (Info, bool) {
	all, err := s.List(ctx)
	if err != nil {
		return Info{}, false
	}
	for _, in := range all {
		if in.Name == name {
			return in, true
		}
	}
	return Info{}, false
}

func (s *Service) deny(ctx context.Context, subject, reason, use string) error {
	if err := s.record(ctx, "token:"+subject, "token.denied", subject, reason+"; "+use); err != nil {
		return err
	}
	return ErrDenied
}

func (s *Service) record(ctx context.Context, actor, action, subject, detail string) error {
	if _, err := s.audit.AddAudit(ctx, actor, action, subject, detail); err != nil {
		return fmt.Errorf("tokens: the audit log refused the entry for %s %s: %w", action, subject, err)
	}
	return nil
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func readDates(expires, remind sql.NullString) (expiry.Dates, error) {
	var d expiry.Dates
	var err error
	if d.Expires, err = expiry.Parse(expires); err != nil {
		return d, err
	}
	d.Remind, err = expiry.Parse(remind)
	return d, err
}

func dateNote(d expiry.Dates) string {
	var b strings.Builder
	if d.Expires != nil {
		b.WriteString("; expires " + d.Expires.UTC().Format(time.DateOnly))
	}
	if d.Remind != nil {
		b.WriteString("; remind " + d.Remind.UTC().Format(time.DateOnly))
	}
	return b.String()
}
