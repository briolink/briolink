// Package secrets is the control plane's secret store (A20): each secret is
// sealed with AES-256-GCM under a master key that comes from a root-only
// systemd credential. A secret can be stored, rotated and deleted, and read
// back by the code that needs it; nothing lists or displays a value. Every
// operation is in the audit log, by name; a value is never in an error, a
// log line or an audit entry.
package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/briolink/briolink/control/expiry"
	"github.com/briolink/briolink/gen/domain/control"
	sqlitegen "github.com/briolink/briolink/gen/sqlite"
)

var (
	// ErrInvalid: the request was refused before it reached the database.
	ErrInvalid = errors.New("secrets: invalid argument")
	// ErrNotFound: no secret has this name.
	ErrNotFound = errors.New("secrets: not found")
	// ErrExists: a secret already has this name (rotate it instead).
	ErrExists = errors.New("secrets: already exists")
	// ErrCannotOpen: a sealed value did not open: the master key is not the
	// one it was sealed under, or the stored value was altered.
	ErrCannotOpen = errors.New("secrets: cannot open a sealed value (wrong master key, or the value was altered)")
)

// maxValue bounds a secret: these are tokens and keys, not files.
const maxValue = 64 << 10

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// Auditor is the audit log. *store.Store is one.
type Auditor interface {
	AddAudit(ctx context.Context, actor, action, subject, detail string) (control.AuditEntry, error)
}

// Vault seals secrets into the control plane's database.
type Vault struct {
	q     *sqlitegen.Queries
	gcm   cipher.AEAD
	audit Auditor
	now   func() time.Time
}

// New returns a Vault over db, sealing under key, which must be 32 bytes.
func New(db sqlitegen.DBTX, key []byte, audit Auditor, now func() time.Time) (*Vault, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: the master key must be 32 bytes, got %d", ErrInvalid, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{q: sqlitegen.New(db), gcm: gcm, audit: audit, now: now}, nil
}

// LoadMasterKey reads the master key from the systemd credential called name
// in dir ($CREDENTIALS_DIRECTORY): 64 hex characters, the form the founder's
// password manager also holds. It refuses a file that group or others can
// read, and its errors never quote the content.
func LoadMasterKey(dir, name string) ([]byte, error) {
	if dir == "" {
		return nil, errors.New("secrets: CREDENTIALS_DIRECTORY is not set: the master key comes from a systemd credential")
	}
	if name == "" || strings.ContainsAny(name, `/\`) || name == ".." || name == "." {
		return nil, fmt.Errorf("secrets: the master key credential %q must be a file name, not a path", name)
	}
	path := filepath.Join(dir, name)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("secrets: master key: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("secrets: master key %s must be readable by its owner only, not %v", path, info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("secrets: master key: %w", err)
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("secrets: master key %s must be 64 hexadecimal characters (32 bytes)", path)
	}
	return key, nil
}

// Info is what is known of a secret without its value.
type Info struct {
	Name      string
	CreatedAt time.Time
	RotatedAt *time.Time
	Expiry    expiry.Dates
}

// Put stores a new secret.
func (v *Vault) Put(ctx context.Context, actor, name string, value []byte, d expiry.Dates) error {
	sealed, err := v.prepare(name, value, d)
	if err != nil {
		return err
	}
	_, err = v.q.InsertSecret(ctx, name, sealed, expiry.Format(ptr(v.now())), expiry.Null(d.Expires), expiry.Null(d.Remind))
	if err != nil {
		if _, gerr := v.q.GetSecret(ctx, name); gerr == nil {
			return fmt.Errorf("%w: %s", ErrExists, name)
		}
		return fmt.Errorf("secrets: store %s: %w", name, err)
	}
	return v.record(ctx, actor, "secret.put", name, dates(d))
}

// Rotate replaces a secret's value and its dates.
func (v *Vault) Rotate(ctx context.Context, actor, name string, value []byte, d expiry.Dates) error {
	sealed, err := v.prepare(name, value, d)
	if err != nil {
		return err
	}
	_, err = v.q.RotateSecret(ctx, sealed, expiry.Format(ptr(v.now())), expiry.Null(d.Expires), expiry.Null(d.Remind), name)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return fmt.Errorf("secrets: rotate %s: %w", name, err)
	}
	return v.record(ctx, actor, "secret.rotate", name, dates(d))
}

// Delete removes a secret.
func (v *Vault) Delete(ctx context.Context, actor, name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w: a name is 1 to 64 letters, digits, '_', '.' or '-'", ErrInvalid)
	}
	_, err := v.q.DeleteSecret(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return fmt.Errorf("secrets: delete %s: %w", name, err)
	}
	return v.record(ctx, actor, "secret.delete", name, "")
}

// Open returns a secret's value to the code that needs it. The access is
// logged first: if it cannot be, the value is not returned.
func (v *Vault) Open(ctx context.Context, actor, name string) ([]byte, error) {
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("%w: a name is 1 to 64 letters, digits, '_', '.' or '-'", ErrInvalid)
	}
	row, err := v.q.GetSecret(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return nil, fmt.Errorf("secrets: read %s: %w", name, err)
	}
	value, err := v.unseal(name, row.Sealed)
	if err != nil {
		if aerr := v.record(ctx, actor, "secret.open_failed", name, ""); aerr != nil {
			return nil, errors.Join(err, aerr)
		}
		return nil, err
	}
	if err := v.record(ctx, actor, "secret.open", name, ""); err != nil {
		return nil, err
	}
	return value, nil
}

// List describes every secret, by name; never a value.
func (v *Vault) List(ctx context.Context) ([]Info, error) {
	rows, err := v.q.ListSecrets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(rows))
	for _, r := range rows {
		created, err := expiry.Parse(sql.NullString{String: r.CreatedAt, Valid: true})
		if err != nil {
			return nil, err
		}
		in := Info{Name: r.Name, CreatedAt: *created}
		if in.RotatedAt, err = expiry.Parse(r.RotatedAt); err != nil {
			return nil, err
		}
		if in.Expiry.Expires, err = expiry.Parse(r.ExpiresAt); err != nil {
			return nil, err
		}
		if in.Expiry.Remind, err = expiry.Parse(r.RemindAt); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, nil
}

// Verify opens every sealed value, to find out at start that the master key
// is the right one. It returns how many secrets there are, and the name of
// the first that did not open. It logs nothing per secret.
func (v *Vault) Verify(ctx context.Context) (int, error) {
	rows, err := v.q.ListSealed(ctx)
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		if _, err := v.unseal(r.Name, r.Sealed); err != nil {
			return 0, fmt.Errorf("%s: %w", r.Name, err)
		}
	}
	return len(rows), nil
}

func (v *Vault) prepare(name string, value []byte, d expiry.Dates) (string, error) {
	switch {
	case !namePattern.MatchString(name):
		return "", fmt.Errorf("%w: a name is 1 to 64 letters, digits, '_', '.' or '-'", ErrInvalid)
	case len(value) == 0 || len(value) > maxValue:
		return "", fmt.Errorf("%w: a value is 1 to %d bytes", ErrInvalid, maxValue)
	}
	if err := d.Validate(); err != nil {
		return "", err
	}
	return v.seal(name, value)
}

// seal is base64(nonce || ciphertext || tag). The name is the associated
// data, so a sealed value moved under another name does not open.
func (v *Vault) seal(name string, value []byte) (string, error) {
	nonce := make([]byte, v.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(v.gcm.Seal(nonce, nonce, value, []byte(name))), nil
}

func (v *Vault) unseal(name, sealed string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < v.gcm.NonceSize()+v.gcm.Overhead() {
		return nil, ErrCannotOpen
	}
	value, err := v.gcm.Open(nil, raw[:v.gcm.NonceSize()], raw[v.gcm.NonceSize():], []byte(name))
	if err != nil {
		return nil, ErrCannotOpen
	}
	return value, nil
}

func (v *Vault) record(ctx context.Context, actor, action, name, detail string) error {
	if _, err := v.audit.AddAudit(ctx, actor, action, name, detail); err != nil {
		return fmt.Errorf("secrets: the audit log refused the entry for %s %s: %w", action, name, err)
	}
	return nil
}

func dates(d expiry.Dates) string {
	var parts []string
	if d.Expires != nil {
		parts = append(parts, "expires "+d.Expires.UTC().Format(time.DateOnly))
	}
	if d.Remind != nil {
		parts = append(parts, "remind "+d.Remind.UTC().Format(time.DateOnly))
	}
	return strings.Join(parts, ", ")
}

func ptr[T any](v T) *T { return &v }
