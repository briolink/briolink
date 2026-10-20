package secrets_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/expiry"
	"github.com/briolink/briolink/control/secrets"
	"github.com/briolink/briolink/control/store"
)

var epoch = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

type rig struct {
	st    *store.Store
	vault *secrets.Vault
	key   []byte
	path  string
}

func newRig(t *testing.T) rig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.db")
	st, err := store.Open(context.Background(), path, events.New(), func() time.Time { return epoch })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	key := make([]byte, 32)
	rand.Read(key)
	v, err := secrets.New(st.DB(), key, st, func() time.Time { return epoch })
	if err != nil {
		t.Fatal(err)
	}
	return rig{st, v, key, path}
}

func day(s string) *time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return &t
}

const value = "ghp_example-VALUE-0123456789"

func (r rig) auditText(t *testing.T) string {
	t.Helper()
	entries, err := r.st.ListAudit(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Actor + "|" + e.Action + "|" + e.Subject + "|" + e.Detail + "\n")
	}
	return b.String()
}

func TestSealAndOpen(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	if err := r.vault.Put(ctx, "founder", "MIRROR_GH_TOKEN", []byte(value), expiry.Dates{Expires: day("2027-10-17"), Remind: day("2027-09-17")}); err != nil {
		t.Fatal(err)
	}
	got, err := r.vault.Open(ctx, "runner", "MIRROR_GH_TOKEN")
	if err != nil || string(got) != value {
		t.Fatalf("Open = %q, %v", got, err)
	}
	// Sealing is randomised: two secrets with one value are not alike on disk.
	if err := r.vault.Put(ctx, "founder", "COPY", []byte(value), expiry.Dates{}); err != nil {
		t.Fatal(err)
	}
	r.st.Close()
	disk, err := os.ReadFile(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if wal, err := os.ReadFile(r.path + "-wal"); err == nil {
		disk = append(disk, wal...)
	}
	if bytes.Contains(disk, []byte(value)) {
		t.Fatal("the value is in the database file in the clear")
	}
}

func TestPutRefusesAnExistingNameAndBadInput(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	if err := r.vault.Put(ctx, "a", "K", []byte("v"), expiry.Dates{}); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"duplicate":       r.vault.Put(ctx, "a", "K", []byte("w"), expiry.Dates{}),
		"empty value":     r.vault.Put(ctx, "a", "E", nil, expiry.Dates{}),
		"bad name":        r.vault.Put(ctx, "a", "has space", []byte("v"), expiry.Dates{}),
		"remind > expiry": r.vault.Put(ctx, "a", "D", []byte("v"), expiry.Dates{Expires: day("2027-01-01"), Remind: day("2027-02-01")}),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := r.vault.Put(ctx, "a", "K", []byte("w"), expiry.Dates{}); !errors.Is(err, secrets.ErrExists) {
		t.Errorf("duplicate: %v", err)
	}
	if got, _ := r.vault.Open(ctx, "a", "K"); string(got) != "v" {
		t.Errorf("a refused Put changed the value to %q", got)
	}
}

func TestRotate(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	if err := r.vault.Put(ctx, "a", "K", []byte("old"), expiry.Dates{Expires: day("2026-12-01")}); err != nil {
		t.Fatal(err)
	}
	if err := r.vault.Rotate(ctx, "a", "K", []byte("new"), expiry.Dates{Expires: day("2027-12-01"), Remind: day("2027-11-01")}); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.vault.Open(ctx, "a", "K"); string(got) != "new" {
		t.Errorf("after rotation: %q", got)
	}
	list, err := r.vault.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v", list, err)
	}
	if in := list[0]; in.RotatedAt == nil || !in.RotatedAt.Equal(epoch) || !in.Expiry.Expires.Equal(*day("2027-12-01")) || !in.Expiry.Remind.Equal(*day("2027-11-01")) {
		t.Errorf("rotation did not record its dates: %+v", in)
	}
	if err := r.vault.Rotate(ctx, "a", "MISSING", []byte("x"), expiry.Dates{}); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("rotating a missing secret: %v", err)
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.vault.Put(ctx, "a", "K", []byte("v"), expiry.Dates{})
	if err := r.vault.Delete(ctx, "a", "K"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.vault.Open(ctx, "a", "K"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("Open after delete: %v", err)
	}
	if err := r.vault.Delete(ctx, "a", "K"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
	if list, _ := r.vault.List(ctx); len(list) != 0 {
		t.Errorf("List after delete: %v", list)
	}
}

func TestAWrongKeyOpensNothing(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.vault.Put(ctx, "a", "K", []byte(value), expiry.Dates{})
	other := make([]byte, 32)
	rand.Read(other)
	wrong, err := secrets.New(r.st.DB(), other, r.st, func() time.Time { return epoch })
	if err != nil {
		t.Fatal(err)
	}
	got, err := wrong.Open(ctx, "a", "K")
	if !errors.Is(err, secrets.ErrCannotOpen) || got != nil {
		t.Fatalf("Open under a wrong key = %q, %v", got, err)
	}
	if _, err := wrong.Verify(ctx); !errors.Is(err, secrets.ErrCannotOpen) || !strings.Contains(err.Error(), "K") {
		t.Errorf("Verify under a wrong key: %v", err)
	}
	if n, err := r.vault.Verify(ctx); err != nil || n != 1 {
		t.Errorf("Verify under the right key = %d, %v", n, err)
	}
}

func TestASealedValueMovedToAnotherNameDoesNotOpen(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.vault.Put(ctx, "a", "A", []byte("alpha"), expiry.Dates{})
	r.vault.Put(ctx, "a", "B", []byte("beta"), expiry.Dates{})
	if _, err := r.st.DB().ExecContext(ctx, `UPDATE secrets SET sealed = (SELECT sealed FROM secrets WHERE name = 'A') WHERE name = 'B'`); err != nil {
		t.Fatal(err)
	}
	if got, err := r.vault.Open(ctx, "a", "B"); !errors.Is(err, secrets.ErrCannotOpen) || got != nil {
		t.Fatalf("a swapped value opened: %q, %v", got, err)
	}
}

func TestATamperedValueDoesNotOpen(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.vault.Put(ctx, "a", "A", []byte("alpha"), expiry.Dates{})
	for _, junk := range []string{"", "not base64!", "AAAA"} {
		if _, err := r.st.DB().ExecContext(ctx, `UPDATE secrets SET sealed = ? WHERE name = 'A'`, junk); err != nil {
			t.Fatal(err)
		}
		if _, err := r.vault.Open(ctx, "a", "A"); !errors.Is(err, secrets.ErrCannotOpen) {
			t.Errorf("sealed = %q: %v", junk, err)
		}
	}
}

func TestNoSecretInTheAuditLogOrInErrors(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	var errs []error
	errs = append(errs,
		r.vault.Put(ctx, "founder", "K", []byte(value), expiry.Dates{Expires: day("2027-01-01")}),
		r.vault.Put(ctx, "founder", "K", []byte(value), expiry.Dates{}),
		r.vault.Rotate(ctx, "founder", "K", []byte(value+"2"), expiry.Dates{}),
		r.vault.Rotate(ctx, "founder", "NOPE", []byte(value), expiry.Dates{}),
	)
	_, err := r.vault.Open(ctx, "runner", "K")
	errs = append(errs, err)
	wrong, _ := secrets.New(r.st.DB(), bytes.Repeat([]byte{7}, 32), r.st, func() time.Time { return epoch })
	_, err = wrong.Open(ctx, "runner", "K")
	errs = append(errs, err)
	errs = append(errs, r.vault.Delete(ctx, "founder", "K"))
	for _, e := range errs {
		if e != nil && strings.Contains(e.Error(), value) {
			t.Errorf("an error carries the value: %v", e)
		}
	}
	log := r.auditText(t)
	if strings.Contains(log, value) {
		t.Errorf("the audit log carries the value:\n%s", log)
	}
	for _, want := range []string{"founder|secret.put|K|", "founder|secret.rotate|K|", "runner|secret.open|K|", "runner|secret.open_failed|K|", "founder|secret.delete|K|"} {
		if !strings.Contains(log, want) {
			t.Errorf("the audit log lacks %q:\n%s", want, log)
		}
	}
}

func TestListShowsNoValue(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.vault.Put(ctx, "a", "CF_API_TOKEN", []byte(value), expiry.Dates{})
	r.vault.Put(ctx, "a", "MIRROR_GH_TOKEN", []byte(value), expiry.Dates{Expires: day("2027-10-17"), Remind: day("2027-09-17")})
	list, err := r.vault.List(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "CF_API_TOKEN" {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if got := list[1].Expiry.At(*day("2027-09-20")); got != expiry.Remind {
		t.Errorf("the mirror token on 2027-09-20: %s, want remind", got)
	}
}

func TestMasterKey(t *testing.T) {
	dir := t.TempDir()
	good := strings.Repeat("ab", 32)
	write := func(name, content string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		os.Chmod(filepath.Join(dir, name), mode)
	}
	write("ok", good+"\n", 0o400)
	write("open", good, 0o644)
	write("short", "abcd", 0o400)
	write("nothex", strings.Repeat("zz", 32), 0o400)

	if key, err := secrets.LoadMasterKey(dir, "ok"); err != nil || len(key) != 32 || key[0] != 0xab {
		t.Errorf("good key: %x, %v", key, err)
	}
	for _, name := range []string{"open", "short", "nothex", "missing"} {
		_, err := secrets.LoadMasterKey(dir, name)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), good) || strings.Contains(err.Error(), "abab") {
			t.Errorf("%s: the error quotes the key: %v", name, err)
		}
	}
	if _, err := secrets.LoadMasterKey("", "ok"); err == nil {
		t.Error("an empty credentials directory was accepted")
	}
	if _, err := secrets.New(nil, make([]byte, 16), nil, time.Now); !errors.Is(err, secrets.ErrInvalid) {
		t.Errorf("a 16-byte key: %v", err)
	}
}
