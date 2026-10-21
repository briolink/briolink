// Package vault is the control plane's secret store and API tokens (A20).
//
// Secrets are sealed with AES-256-GCM under a master key and can be
// replaced, deleted and used, never read back through any listing. API
// tokens are random, kept only as a SHA-256 hash, act as one role, and every
// use of one is appended to the audit log. Secrets and tokens may carry an
// expiry date and a reminder date, which Expiring lists.
//
// No error or audit line produced here holds a secret, a token or the key.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	// ErrNotFound: no secret or token of that name.
	ErrNotFound = errors.New("vault: not found")
	// ErrExists: a secret or token of that name already exists.
	ErrExists = errors.New("vault: already exists")
	// ErrWrongKey: a stored secret does not open under the master key.
	ErrWrongKey = errors.New("vault: the master key does not open this secret")
	// ErrInvalid: the arguments were refused before anything was written.
	ErrInvalid = errors.New("vault: invalid argument")
	// ErrDenied: a token is unknown, expired, revoked, or acts as another role.
	ErrDenied = errors.New("vault: token refused")
)

const dateLayout = "2006-01-02"

// Auditor appends to the audit log. *store.Store is one. A change to the
// vault is recorded with AuditTx, in the transaction that makes the change.
type Auditor interface {
	Audit(ctx context.Context, actor, action, subject, detail string) error
	AuditTx(ctx context.Context, tx *sql.Tx, actor, action, subject, detail string) (publish func(), err error)
}

// change runs fn and records it in the audit log in one transaction: if
// either fails, neither happens. The audit event is published after the commit.
func (v *Vault) change(ctx context.Context, actor, action, subject, detail string, fn func(tx *sql.Tx) error) error {
	tx, err := v.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	publish, err := v.audit.AuditTx(ctx, tx, actor, action, subject, detail)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	publish()
	return nil
}

// Vault is the secret store and token table over the control plane's database.
type Vault struct {
	db    *sql.DB
	aead  cipher.AEAD
	audit Auditor
	roles map[string]bool
	now   func() time.Time
}

// ParseKey reads a master key written as 64 hexadecimal digits (a trailing
// newline is allowed), as `openssl rand -hex 32` prints it.
func ParseKey(text []byte) ([]byte, error) {
	key, err := hex.DecodeString(strings.TrimSpace(string(text)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("%w: the master key must be 64 hexadecimal digits (32 bytes)", ErrInvalid)
	}
	return key, nil
}

// New returns a vault over db sealing with key (32 bytes). Tokens may act as
// the given roles only.
func New(db *sql.DB, key []byte, audit Auditor, roles []string, now func() time.Time) (*Vault, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: the master key must be 32 bytes", ErrInvalid)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	v := &Vault{db: db, aead: aead, audit: audit, roles: map[string]bool{}, now: now}
	for _, r := range roles {
		v.roles[r] = true
	}
	return v, nil
}

func (v *Vault) stamp() string { return v.now().UTC().Format(time.RFC3339) }

// Dates are the optional expiry and reminder of a secret or token. A zero
// value is no date. A reminder must come before the expiry it reminds of.
type Dates struct {
	ExpiresOn string // YYYY-MM-DD
	RemindOn  string // YYYY-MM-DD
}

func (d Dates) check() error {
	for _, s := range []string{d.ExpiresOn, d.RemindOn} {
		if s == "" {
			continue
		}
		if _, err := time.Parse(dateLayout, s); err != nil {
			return fmt.Errorf("%w: %q is not a date like 2027-10-17", ErrInvalid, s)
		}
	}
	if d.ExpiresOn != "" && d.RemindOn != "" && d.RemindOn > d.ExpiresOn {
		return fmt.Errorf("%w: the reminder %s is after the expiry %s", ErrInvalid, d.RemindOn, d.ExpiresOn)
	}
	return nil
}

func validName(s string) bool {
	if s == "" || len(s) > 100 {
		return false
	}
	for _, r := range s {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

// seal binds the ciphertext to the secret's name, so a sealed value moved to
// another row does not open.
func (v *Vault) seal(name string, value []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, value, []byte(name)), nil
}

func (v *Vault) open(name string, sealed []byte) ([]byte, error) {
	n := v.aead.NonceSize()
	if len(sealed) < n {
		return nil, ErrWrongKey
	}
	plain, err := v.aead.Open(nil, sealed[:n], sealed[n:], []byte(name))
	if err != nil {
		return nil, ErrWrongKey
	}
	return plain, nil
}

// Put stores a new secret.
func (v *Vault) Put(ctx context.Context, actor, name string, value []byte, d Dates) error {
	if !validName(name) || len(value) == 0 {
		return fmt.Errorf("%w: a secret needs a name (printable, no spaces) and a value", ErrInvalid)
	}
	if err := d.check(); err != nil {
		return err
	}
	sealed, err := v.seal(name, value)
	if err != nil {
		return err
	}
	now := v.stamp()
	return v.change(ctx, actor, "secret.put", name, datesDetail(d), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO secrets (name, sealed, created_at, rotated_at, expires_on, remind_on) VALUES (?, ?, ?, ?, ?, ?)`,
			name, sealed, now, now, d.ExpiresOn, d.RemindOn)
		if isUnique(err) {
			return fmt.Errorf("%w: secret %s", ErrExists, name)
		}
		return err
	})
}

// Rotate replaces a secret's value and its dates: a date not given is cleared,
// not kept.
func (v *Vault) Rotate(ctx context.Context, actor, name string, value []byte, d Dates) error {
	if len(value) == 0 {
		return fmt.Errorf("%w: a secret needs a value", ErrInvalid)
	}
	if err := d.check(); err != nil {
		return err
	}
	sealed, err := v.seal(name, value)
	if err != nil {
		return err
	}
	return v.change(ctx, actor, "secret.rotate", name, datesDetail(d), func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE secrets SET sealed = ?, rotated_at = ?, expires_on = ?, remind_on = ? WHERE name = ?`,
			sealed, v.stamp(), d.ExpiresOn, d.RemindOn, name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("%w: secret %s", ErrNotFound, name)
		}
		return nil
	})
}

// Delete removes a secret.
func (v *Vault) Delete(ctx context.Context, actor, name string) error {
	return v.change(ctx, actor, "secret.delete", name, "", func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM secrets WHERE name = ?`, name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("%w: secret %s", ErrNotFound, name)
		}
		return nil
	})
}

// Use opens a secret for an adapter that needs its value, and records the
// use. It is the only way to the value; nothing lists or displays it.
func (v *Vault) Use(ctx context.Context, actor, name string) ([]byte, error) {
	var sealed []byte
	err := v.db.QueryRowContext(ctx, `SELECT sealed FROM secrets WHERE name = ?`, name).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: secret %s", ErrNotFound, name)
	}
	if err != nil {
		return nil, err
	}
	plain, err := v.open(name, sealed)
	if err != nil {
		return nil, fmt.Errorf("%w (secret %s)", err, name)
	}
	if err := v.audit.Audit(ctx, actor, "secret.use", name, ""); err != nil {
		return nil, err
	}
	return plain, nil
}

// Verify opens every stored secret under the master key and reports the names
// of those that do not open (never a value). It is the check that the key in
// hand is the key the secrets were sealed with: the control plane runs it
// before it serves, and before a command seals something new under the key.
func (v *Vault) Verify(ctx context.Context) error {
	rows, err := v.db.QueryContext(ctx, `SELECT name, sealed FROM secrets ORDER BY name`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var bad []string
	for rows.Next() {
		var (
			name   string
			sealed []byte
		)
		if err := rows.Scan(&name, &sealed); err != nil {
			return err
		}
		if _, err := v.open(name, sealed); err != nil {
			bad = append(bad, name)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(bad) > 0 {
		return fmt.Errorf("%w: %d of the stored secrets do not open (%s)", ErrWrongKey, len(bad), strings.Join(bad, ", "))
	}
	return nil
}

// Entry is what is known of a secret or token without its value.
type Entry struct {
	Kind      string // "secret" or "token"
	Name      string
	Role      string // tokens only
	CreatedAt string
	RotatedAt string // secrets only
	ExpiresOn string
	RemindOn  string
	Revoked   bool // tokens only
}

// List returns every secret and token without its value, by kind then name.
func (v *Vault) List(ctx context.Context) ([]Entry, error) {
	var out []Entry
	rows, err := v.db.QueryContext(ctx, `SELECT name, created_at, rotated_at, expires_on, remind_on FROM secrets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		e := Entry{Kind: "secret"}
		if err := rows.Scan(&e.Name, &e.CreatedAt, &e.RotatedAt, &e.ExpiresOn, &e.RemindOn); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = v.db.QueryContext(ctx, `SELECT name, role, created_at, expires_on, remind_on, revoked_at FROM tokens ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		e := Entry{Kind: "token"}
		var revoked string
		if err := rows.Scan(&e.Name, &e.Role, &e.CreatedAt, &e.ExpiresOn, &e.RemindOn, &revoked); err != nil {
			return nil, err
		}
		e.Revoked = revoked != ""
		out = append(out, e)
	}
	return out, rows.Err()
}

// Expiring returns the secrets and live tokens whose reminder date has come
// or, without one, whose expiry falls within 30 days of today (the entries
// the daily credential-expiry check lists), soonest expiry first. Expired
// ones are included.
func (v *Vault) Expiring(ctx context.Context) ([]Entry, error) {
	all, err := v.List(ctx)
	if err != nil {
		return nil, err
	}
	today := v.now().UTC().Format(dateLayout)
	horizon := v.now().UTC().AddDate(0, 0, 30).Format(dateLayout)
	var out []Entry
	for _, e := range all {
		if e.Revoked || e.ExpiresOn == "" {
			continue
		}
		if (e.RemindOn != "" && e.RemindOn <= today) || (e.RemindOn == "" && e.ExpiresOn <= horizon) || e.ExpiresOn <= today {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ExpiresOn < out[j].ExpiresOn })
	return out, nil
}

// CreateToken makes an API token acting as role, and returns it. This is the
// only time the token exists in the clear.
func (v *Vault) CreateToken(ctx context.Context, actor, name, role string, d Dates) (string, error) {
	if !validName(name) {
		return "", fmt.Errorf("%w: a token needs a name (printable, no spaces)", ErrInvalid)
	}
	if !v.roles[role] {
		return "", fmt.Errorf("%w: %q is not a role", ErrInvalid, role)
	}
	if err := d.check(); err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := "blk_" + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	detail := strings.TrimSuffix("role "+role+"; "+datesDetail(d), "; ")
	err := v.change(ctx, actor, "token.create", name, detail, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO tokens (name, hash, role, created_at, expires_on, remind_on, revoked_at) VALUES (?, ?, ?, ?, ?, ?, '')`,
			name, sum[:], role, v.stamp(), d.ExpiresOn, d.RemindOn)
		if isUnique(err) {
			return fmt.Errorf("%w: token %s", ErrExists, name)
		}
		return err
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// RevokeToken stops a token from authenticating. Revoking twice is not an error.
func (v *Vault) RevokeToken(ctx context.Context, actor, name string) error {
	return v.change(ctx, actor, "token.revoke", name, "", func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE tokens SET revoked_at = CASE WHEN revoked_at = '' THEN ? ELSE revoked_at END WHERE name = ?`, v.stamp(), name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("%w: token %s", ErrNotFound, name)
		}
		return nil
	})
}

// Authenticate checks a presented token and returns the role it acts as and
// its name. Every use of a known token is audited, accepted or not; a token
// nobody issued is refused without a line, so a stranger cannot grow the log.
// If the audit line cannot be written the use is refused.
func (v *Vault) Authenticate(ctx context.Context, token, use string) (role, name string, err error) {
	sum := sha256.Sum256([]byte(token))
	var (
		expires, revokedAt string
	)
	err = v.db.QueryRowContext(ctx, `SELECT name, role, expires_on, revoked_at FROM tokens WHERE hash = ?`, sum[:]).Scan(&name, &role, &expires, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrDenied
	}
	if err != nil {
		return "", "", err
	}
	switch {
	case revokedAt != "":
		err = v.audit.Audit(ctx, "token:"+name, "token.refused", use, "revoked")
		if err == nil {
			err = ErrDenied
		}
		return "", "", err
	case expires != "" && expires < v.now().UTC().Format(dateLayout):
		err = v.audit.Audit(ctx, "token:"+name, "token.refused", use, "expired "+expires)
		if err == nil {
			err = ErrDenied
		}
		return "", "", err
	}
	if err := v.audit.Audit(ctx, "token:"+name, "token.use", use, ""); err != nil {
		return "", "", err
	}
	return role, name, nil
}

func datesDetail(d Dates) string {
	var p []string
	if d.ExpiresOn != "" {
		p = append(p, "expires "+d.ExpiresOn)
	}
	if d.RemindOn != "" {
		p = append(p, "remind "+d.RemindOn)
	}
	return strings.Join(p, "; ")
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
