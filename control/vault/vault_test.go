package vault_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/logic"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/control/vault"
)

var (
	ctx  = context.Background()
	key1 = bytes.Repeat([]byte{1}, 32)
	key2 = bytes.Repeat([]byte{2}, 32)
	now  = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
)

type rig struct {
	st *store.Store
	v  *vault.Vault
}

func open(t *testing.T) rig {
	t.Helper()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "c.db"), events.New(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	v, err := vault.New(st.DB(), key1, st, []string{"operator", "viewer"}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return rig{st, v}
}

func (r rig) withKey(t *testing.T, key []byte) *vault.Vault {
	t.Helper()
	v, err := vault.New(r.st.DB(), key, r.st, []string{"operator"}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSealOpenRotateDelete(t *testing.T) {
	r := open(t)
	if err := r.v.Put(ctx, "cli", "MIRROR_GH_TOKEN", []byte("v1-value"), vault.Dates{ExpiresOn: "2027-10-17", RemindOn: "2027-09-17"}); err != nil {
		t.Fatal(err)
	}
	got, err := r.v.Use(ctx, "mirror", "MIRROR_GH_TOKEN")
	if err != nil || string(got) != "v1-value" {
		t.Fatalf("Use = %q, %v", got, err)
	}
	if err := r.v.Put(ctx, "cli", "MIRROR_GH_TOKEN", []byte("x"), vault.Dates{}); !errors.Is(err, vault.ErrExists) {
		t.Errorf("second put: %v", err)
	}
	if err := r.v.Rotate(ctx, "cli", "MIRROR_GH_TOKEN", []byte("v2-value"), vault.Dates{ExpiresOn: "2028-10-17"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.v.Use(ctx, "mirror", "MIRROR_GH_TOKEN"); string(got) != "v2-value" {
		t.Errorf("after rotate Use = %q", got)
	}
	list, err := r.v.List(ctx)
	if err != nil || len(list) != 1 || list[0].ExpiresOn != "2028-10-17" || list[0].RemindOn != "" {
		t.Errorf("list after rotate = %+v, %v", list, err)
	}
	if err := r.v.Rotate(ctx, "cli", "nope", []byte("x"), vault.Dates{}); !errors.Is(err, vault.ErrNotFound) {
		t.Errorf("rotate missing: %v", err)
	}
	if err := r.v.Delete(ctx, "cli", "MIRROR_GH_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.v.Use(ctx, "mirror", "MIRROR_GH_TOKEN"); !errors.Is(err, vault.ErrNotFound) {
		t.Errorf("use after delete: %v", err)
	}
	if err := r.v.Delete(ctx, "cli", "MIRROR_GH_TOKEN"); !errors.Is(err, vault.ErrNotFound) {
		t.Errorf("delete twice: %v", err)
	}
}

func TestStoredBytesDoNotHoldTheValue(t *testing.T) {
	r := open(t)
	const value = "ghp_very-recognisable-secret-value"
	if err := r.v.Put(ctx, "cli", "A", []byte(value), vault.Dates{}); err != nil {
		t.Fatal(err)
	}
	var sealed []byte
	if err := r.st.DB().QueryRow(`SELECT sealed FROM secrets WHERE name = 'A'`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(value)) || len(sealed) != 12+len(value)+16 {
		t.Errorf("sealed = %x", sealed)
	}
	// Sealing the same value twice gives different bytes (a fresh nonce).
	r.v.Put(ctx, "cli", "B", []byte(value), vault.Dates{})
	var other []byte
	r.st.DB().QueryRow(`SELECT sealed FROM secrets WHERE name = 'B'`).Scan(&other)
	if bytes.Equal(sealed, other) {
		t.Error("two seals of one value are identical")
	}
}

func TestWrongKeyTamperingAndMovedValues(t *testing.T) {
	r := open(t)
	r.v.Put(ctx, "cli", "A", []byte("value-a"), vault.Dates{})
	r.v.Put(ctx, "cli", "B", []byte("value-b"), vault.Dates{})

	_, err := r.withKey(t, key2).Use(ctx, "x", "A")
	if !errors.Is(err, vault.ErrWrongKey) {
		t.Errorf("wrong key: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "value-a") {
		t.Errorf("error leaks the value: %v", err)
	}

	db := r.st.DB()
	// A flipped bit does not open.
	var sealed []byte
	if err := db.QueryRow(`SELECT sealed FROM secrets WHERE name = 'A'`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	sealed[20] ^= 1
	if _, err := db.Exec(`UPDATE secrets SET sealed = ? WHERE name = 'A'`, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := r.v.Use(ctx, "x", "A"); !errors.Is(err, vault.ErrWrongKey) {
		t.Errorf("tampered: %v", err)
	}
	// A sealed value copied onto another name does not open under it.
	if _, err := db.Exec(`UPDATE secrets SET sealed = (SELECT sealed FROM secrets WHERE name = 'B') WHERE name = 'A'`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.v.Use(ctx, "x", "A"); !errors.Is(err, vault.ErrWrongKey) {
		t.Errorf("moved: %v", err)
	}
	if got, err := r.v.Use(ctx, "x", "B"); err != nil || string(got) != "value-b" {
		t.Errorf("B = %q, %v", got, err)
	}
}

func TestKeyParsing(t *testing.T) {
	good := strings.Repeat("ab", 32)
	if k, err := vault.ParseKey([]byte(good + "\n")); err != nil || len(k) != 32 {
		t.Errorf("good key: %v", err)
	}
	for _, bad := range []string{"", "zz", good[:62], good + "00", strings.Repeat("g", 64)} {
		if _, err := vault.ParseKey([]byte(bad)); !errors.Is(err, vault.ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, err := vault.New(nil, make([]byte, 16), nil, nil, time.Now); !errors.Is(err, vault.ErrInvalid) {
		t.Errorf("short key: %v", err)
	}
}

func TestRefusedArguments(t *testing.T) {
	r := open(t)
	for name, f := range map[string]func() error{
		"empty name":      func() error { return r.v.Put(ctx, "cli", "", []byte("v"), vault.Dates{}) },
		"name with space": func() error { return r.v.Put(ctx, "cli", "a b", []byte("v"), vault.Dates{}) },
		"empty value":     func() error { return r.v.Put(ctx, "cli", "a", nil, vault.Dates{}) },
		"bad date":        func() error { return r.v.Put(ctx, "cli", "a", []byte("v"), vault.Dates{ExpiresOn: "17/10/2027"}) },
		"remind after": func() error {
			return r.v.Put(ctx, "cli", "a", []byte("v"), vault.Dates{ExpiresOn: "2027-01-01", RemindOn: "2027-02-01"})
		},
		"unknown role": func() error {
			_, err := r.v.CreateToken(ctx, "cli", "t", "admin", vault.Dates{})
			return err
		},
		"empty rotate": func() error { return r.v.Rotate(ctx, "cli", "a", nil, vault.Dates{}) },
	} {
		if err := f(); !errors.Is(err, vault.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if list, _ := r.v.List(ctx); len(list) != 0 {
		t.Errorf("a refused write left %+v", list)
	}
}

func TestTokens(t *testing.T) {
	r := open(t)
	tok, err := r.v.CreateToken(ctx, "cli", "ci", "operator", vault.Dates{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.v.CreateToken(ctx, "cli", "ci", "operator", vault.Dates{}); !errors.Is(err, vault.ErrExists) {
		t.Errorf("same name twice: %v", err)
	}
	other, _ := r.v.CreateToken(ctx, "cli", "reader", "viewer", vault.Dates{})
	if tok == other || !strings.HasPrefix(tok, "blk_") {
		t.Errorf("tokens = %q, %q", tok, other)
	}

	role, name, err := r.v.Authenticate(ctx, tok, "GET /x")
	if err != nil || role != "operator" || name != "ci" {
		t.Fatalf("Authenticate = %q %q %v", role, name, err)
	}
	// The role is the token's own: an out-of-scope token authenticates as its
	// role, which the routes then refuse.
	if role, _, err := r.v.Authenticate(ctx, other, "GET /x"); err != nil || role != "viewer" {
		t.Errorf("viewer = %q, %v", role, err)
	}
	for _, bad := range []string{"", "blk_", tok + "x", tok[:len(tok)-1], strings.ToUpper(tok)} {
		if _, _, err := r.v.Authenticate(ctx, bad, "GET /x"); !errors.Is(err, vault.ErrDenied) {
			t.Errorf("%q: %v", bad, err)
		}
	}

	var hash []byte
	r.st.DB().QueryRow(`SELECT hash FROM tokens WHERE name = 'ci'`).Scan(&hash)
	if len(hash) != 32 || bytes.Contains(hash, []byte(tok)) {
		t.Errorf("hash = %x", hash)
	}
	var plain int
	r.st.DB().QueryRow(`SELECT count(*) FROM tokens WHERE instr(CAST(hash AS TEXT), ?) > 0 OR name = ? OR role = ?`, tok, tok, tok).Scan(&plain)
	if plain != 0 {
		t.Error("the token is stored in the clear")
	}

	if err := r.v.RevokeToken(ctx, "cli", "ci"); err != nil {
		t.Fatal(err)
	}
	if err := r.v.RevokeToken(ctx, "cli", "ci"); err != nil {
		t.Errorf("revoke twice: %v", err)
	}
	if _, _, err := r.v.Authenticate(ctx, tok, "GET /x"); !errors.Is(err, vault.ErrDenied) {
		t.Errorf("revoked: %v", err)
	}
	if err := r.v.RevokeToken(ctx, "cli", "nobody"); !errors.Is(err, vault.ErrNotFound) {
		t.Errorf("revoke unknown: %v", err)
	}
}

func TestTokenExpiryIsInclusive(t *testing.T) {
	r := open(t)
	today, _ := r.v.CreateToken(ctx, "cli", "today", "operator", vault.Dates{ExpiresOn: "2026-10-17"})
	yesterday, _ := r.v.CreateToken(ctx, "cli", "yesterday", "operator", vault.Dates{ExpiresOn: "2026-10-16"})
	if _, _, err := r.v.Authenticate(ctx, today, "GET /x"); err != nil {
		t.Errorf("expires today: %v", err)
	}
	if _, _, err := r.v.Authenticate(ctx, yesterday, "GET /x"); !errors.Is(err, vault.ErrDenied) {
		t.Errorf("expired yesterday: %v", err)
	}
}

func TestAuditHasEveryUseAndNoSecret(t *testing.T) {
	r := open(t)
	const value = "ghp_do-not-log-me"
	r.v.Put(ctx, "cli", "S", []byte(value), vault.Dates{ExpiresOn: "2027-01-01"})
	r.v.Rotate(ctx, "cli", "S", []byte(value+"2"), vault.Dates{})
	r.v.Use(ctx, "mirror", "S")
	r.withKey(t, key2).Use(ctx, "mirror", "S")
	tok, _ := r.v.CreateToken(ctx, "cli", "ci", "operator", vault.Dates{})
	r.v.Authenticate(ctx, tok, "GET /api/v1/runs")
	r.v.Authenticate(ctx, "blk_stranger", "GET /api/v1/runs")
	r.v.RevokeToken(ctx, "cli", "ci")
	r.v.Authenticate(ctx, tok, "GET /api/v1/runs")
	r.v.Delete(ctx, "cli", "S")

	log, err := r.st.ListAudit(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for i := len(log) - 1; i >= 0; i-- {
		e := log[i]
		got = append(got, e.Actor+"|"+e.Action+"|"+e.Subject)
		all := e.Actor + e.Action + e.Subject + e.Detail
		if strings.Contains(all, value) || strings.Contains(all, tok) {
			t.Errorf("entry leaks: %+v", e)
		}
	}
	want := []string{
		"cli|secret.put|S", "cli|secret.rotate|S", "mirror|secret.use|S",
		"cli|token.create|ci", "token:ci|token.use|GET /api/v1/runs",
		"cli|token.revoke|ci", "token:ci|token.refused|GET /api/v1/runs", "cli|secret.delete|S",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("audit =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

type failing struct{}

func (failing) Audit(context.Context, string, string, string, string) error {
	return errors.New("log full")
}

func (failing) AuditTx(context.Context, *sql.Tx, string, string, string, string) (func(), error) {
	return nil, errors.New("log full")
}

func TestATokenUseThatCannotBeRecordedIsRefused(t *testing.T) {
	r := open(t)
	tok, _ := r.v.CreateToken(ctx, "cli", "ci", "operator", vault.Dates{})
	v, _ := vault.New(r.st.DB(), key1, failing{}, []string{"operator"}, func() time.Time { return now })
	if _, _, err := v.Authenticate(ctx, tok, "GET /x"); err == nil {
		t.Error("authenticated without a record")
	}
	r.v.Put(ctx, "cli", "S", []byte("v"), vault.Dates{})
	if got, err := v.Use(ctx, "x", "S"); err == nil || got != nil {
		t.Errorf("Use without a record = %q, %v", got, err)
	}
}

func TestExpiring(t *testing.T) {
	r := open(t)
	d := func(e, rem string) vault.Dates { return vault.Dates{ExpiresOn: e, RemindOn: rem} }
	r.v.Put(ctx, "cli", "far", []byte("v"), d("2027-10-17", "2027-09-17"))
	r.v.Put(ctx, "cli", "reminded", []byte("v"), d("2026-12-31", "2026-10-17"))
	r.v.Put(ctx, "cli", "soon", []byte("v"), d("2026-11-10", ""))
	r.v.Put(ctx, "cli", "later", []byte("v"), d("2026-11-20", ""))
	r.v.Put(ctx, "cli", "expired", []byte("v"), d("2026-10-01", "2026-09-01"))
	r.v.Put(ctx, "cli", "never", []byte("v"), d("", ""))
	r.v.CreateToken(ctx, "cli", "tok-soon", "operator", d("2026-11-01", ""))
	r.v.CreateToken(ctx, "cli", "tok-revoked", "operator", d("2026-11-01", ""))
	r.v.RevokeToken(ctx, "cli", "tok-revoked")

	got, err := r.v.Expiring(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	if want := "expired tok-soon soon reminded"; strings.Join(names, " ") != want {
		t.Errorf("expiring = %q, want %q", strings.Join(names, " "), want)
	}
}

func TestSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.db")
	st, _ := store.Open(ctx, path, events.New(), func() time.Time { return now })
	v, _ := vault.New(st.DB(), key1, st, []string{"operator"}, func() time.Time { return now })
	v.Put(ctx, "cli", "S", []byte("kept"), vault.Dates{})
	tok, _ := v.CreateToken(ctx, "cli", "ci", "operator", vault.Dates{})
	st.Close()

	st, err := store.Open(ctx, path, events.New(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	v, _ = vault.New(st.DB(), key1, st, []string{"operator"}, func() time.Time { return now })
	if got, err := v.Use(ctx, "x", "S"); err != nil || string(got) != "kept" {
		t.Errorf("Use = %q, %v", got, err)
	}
	if _, _, err := v.Authenticate(ctx, tok, "GET /x"); err != nil {
		t.Error(err)
	}
}

// The store enforces what the credential routes check first with the pure
// function credential_problem: the two must agree on a name and dates.
func TestVaultAgreesWithCredentialProblem(t *testing.T) {
	r := open(t)
	long := strings.Repeat("a", 101)
	i := 0
	for _, tc := range []struct{ name, expires, remind string }{
		{"CF_API_TOKEN", "", ""}, {"a", "", ""}, {long[:100], "", ""}, {long, "", ""}, {"", "", ""}, {"a b", "", ""}, {"a\tb", "", ""}, {"é", "", ""},
		{"a", "2027-10-17", "2027-09-17"}, {"a", "2027-10-17", "2027-10-17"}, {"a", "", "2027-09-17"}, {"a", "2028-02-29", ""},
		{"a", "2027-02-29", ""}, {"a", "2027-13-01", ""}, {"a", "soon", ""}, {"a", "2027-1-7", ""}, {"a", "", "2027-02-30"}, {"a", "2027-09-17", "2027-10-17"},
	} {
		i++
		name := tc.name
		if name != "" && len(name) <= 100 { // a fresh name each time, so only the rules can refuse
			name = strings.Repeat("n", i) + name
			name = name[:min(len(name), 100)]
		}
		want := (logic.Impl{}).CredentialProblem(name, tc.expires, tc.remind) == ""
		err := r.v.Put(ctx, "test", name, []byte("v"), vault.Dates{ExpiresOn: tc.expires, RemindOn: tc.remind})
		if got := err == nil; got != want {
			t.Errorf("%+v: the vault accepts = %v (%v), credential_problem accepts = %v", tc, got, err, want)
		}
		_, tokenErr := r.v.CreateToken(ctx, "test", name, "operator", vault.Dates{ExpiresOn: tc.expires, RemindOn: tc.remind})
		if got := tokenErr == nil; got != want {
			t.Errorf("%+v: the vault issues = %v (%v), credential_problem accepts = %v", tc, got, tokenErr, want)
		}
	}
}

func TestAChangeWithoutItsAuditLineDoesNotHappen(t *testing.T) {
	r := open(t)
	r.v.Put(ctx, "cli", "S", []byte("old"), vault.Dates{})
	tok, _ := r.v.CreateToken(ctx, "cli", "ci", "operator", vault.Dates{})
	v, _ := vault.New(r.st.DB(), key1, failing{}, []string{"operator"}, func() time.Time { return now })

	for name, f := range map[string]func() error{
		"put":    func() error { return v.Put(ctx, "x", "NEW", []byte("v"), vault.Dates{}) },
		"rotate": func() error { return v.Rotate(ctx, "x", "S", []byte("new"), vault.Dates{}) },
		"delete": func() error { return v.Delete(ctx, "x", "S") },
		"issue": func() error {
			_, err := v.CreateToken(ctx, "x", "other", "operator", vault.Dates{})
			return err
		},
		"revoke": func() error { return v.RevokeToken(ctx, "x", "ci") },
	} {
		if err := f(); err == nil || !strings.Contains(err.Error(), "log full") {
			t.Errorf("%s: %v", name, err)
		}
	}
	list, _ := r.v.List(ctx)
	if len(list) != 2 {
		t.Errorf("a change was kept without its line: %+v", list)
	}
	if got, err := r.v.Use(ctx, "x", "S"); err != nil || string(got) != "old" {
		t.Errorf("S = %q, %v", got, err)
	}
	if _, _, err := r.v.Authenticate(ctx, tok, "GET /x"); err != nil {
		t.Errorf("the token was revoked without a line: %v", err)
	}
}

func TestVerify(t *testing.T) {
	r := open(t)
	if err := r.v.Verify(ctx); err != nil {
		t.Errorf("an empty vault: %v", err)
	}
	r.v.Put(ctx, "cli", "A", []byte("a"), vault.Dates{})
	r.v.Put(ctx, "cli", "B", []byte("b-secret-value"), vault.Dates{})
	if err := r.v.Verify(ctx); err != nil {
		t.Errorf("the right key: %v", err)
	}
	err := r.withKey(t, key2).Verify(ctx)
	if !errors.Is(err, vault.ErrWrongKey) || !strings.Contains(err.Error(), "2 of the stored secrets") || !strings.Contains(err.Error(), "A, B") {
		t.Errorf("the wrong key: %v", err)
	}
	if strings.Contains(err.Error(), "b-secret-value") {
		t.Errorf("Verify leaks a value: %v", err)
	}
	// One secret sealed under another key is found among good ones.
	other := r.withKey(t, key2)
	other.Put(ctx, "cli", "C", []byte("c"), vault.Dates{})
	if err := r.v.Verify(ctx); err == nil || !strings.Contains(err.Error(), "1 of the stored secrets") || !strings.Contains(err.Error(), "(C)") {
		t.Errorf("one bad secret: %v", err)
	}
}
