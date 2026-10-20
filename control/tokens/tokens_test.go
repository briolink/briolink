package tokens_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/expiry"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/control/tokens"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

type rig struct {
	st  *store.Store
	svc *tokens.Service
	clk *clock
}

func newRig(t *testing.T) rig {
	t.Helper()
	clk := &clock{time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "c.db"), events.New(), clk.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return rig{st, tokens.New(st.DB(), st, clk.now, []string{"operator", "reader"}), clk}
}

func day(s string) *time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return &t
}

func (r rig) audit(t *testing.T) string {
	t.Helper()
	entries, err := r.st.ListAudit(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		b.WriteString(e.Actor + "|" + e.Action + "|" + e.Subject + "|" + e.Detail + "\n")
	}
	return b.String()
}

func TestIssueAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	tok, err := r.svc.Issue(ctx, "founder", "ci", "operator", expiry.Dates{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.svc.Authenticate(ctx, tok, "GET /api/v1/runs")
	if err != nil || p.Name != "ci" || p.Scope != "operator" {
		t.Fatalf("Authenticate = %+v, %v", p, err)
	}
	other, _ := r.svc.Issue(ctx, "founder", "viewer", "reader", expiry.Dates{})
	if p, err := r.svc.Authenticate(ctx, other, "GET /api/v1/runs"); err != nil || p.Scope != "reader" {
		t.Fatalf("a token keeps its own scope: %+v, %v", p, err)
	}
}

func TestEveryUseIsInTheAuditLog(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	tok, _ := r.svc.Issue(ctx, "founder", "ci", "operator", expiry.Dates{})
	r.svc.Authenticate(ctx, tok, "GET /api/v1/runs")
	r.svc.Authenticate(ctx, tok, "GET /api/v1/audit")
	log := r.audit(t)
	for _, want := range []string{
		"founder|token.issue|ci|scope operator",
		"token:ci|token.use|ci|scope operator; GET /api/v1/runs",
		"token:ci|token.use|ci|scope operator; GET /api/v1/audit",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the audit log lacks %q:\n%s", want, log)
		}
	}
}

func TestARevokedTokenIsRefusedAndLogged(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	tok, _ := r.svc.Issue(ctx, "founder", "ci", "operator", expiry.Dates{})
	if err := r.svc.Revoke(ctx, "founder", "ci"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Authenticate(ctx, tok, "GET /api/v1/runs"); !errors.Is(err, tokens.ErrDenied) {
		t.Fatalf("revoked token: %v", err)
	}
	if err := r.svc.Revoke(ctx, "founder", "ci"); !errors.Is(err, tokens.ErrNotFound) {
		t.Errorf("revoking twice: %v", err)
	}
	if err := r.svc.Revoke(ctx, "founder", "nobody"); !errors.Is(err, tokens.ErrNotFound) {
		t.Errorf("revoking an unknown token: %v", err)
	}
	if log := r.audit(t); !strings.Contains(log, "token:ci|token.denied|ci|revoked; GET /api/v1/runs") || !strings.Contains(log, "founder|token.revoke|ci|") {
		t.Errorf("the audit log:\n%s", log)
	}
	list, _ := r.svc.List(ctx)
	if len(list) != 1 || list[0].RevokedAt == nil {
		t.Errorf("List = %+v", list)
	}
}

func TestAnExpiredTokenIsRefusedFromItsExpiry(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	tok, _ := r.svc.Issue(ctx, "founder", "ci", "operator", expiry.Dates{Expires: day("2026-11-01"), Remind: day("2026-10-25")})
	r.clk.t = time.Date(2026, 10, 31, 23, 59, 0, 0, time.UTC)
	if _, err := r.svc.Authenticate(ctx, tok, "x"); err != nil {
		t.Errorf("the day before: %v", err)
	}
	r.clk.t = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if _, err := r.svc.Authenticate(ctx, tok, "x"); !errors.Is(err, tokens.ErrDenied) {
		t.Errorf("on the expiry: %v", err)
	}
	if log := r.audit(t); !strings.Contains(log, "token:ci|token.denied|ci|expired; x") {
		t.Errorf("the audit log:\n%s", log)
	}
}

func TestAnUnknownTokenIsRefusedAndLoggedWithoutIt(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.svc.Issue(ctx, "founder", "ci", "operator", expiry.Dates{})
	guess := "blt_not-a-real-token-0123456789abcdef"
	if _, err := r.svc.Authenticate(ctx, guess, "GET /api/v1/runs"); !errors.Is(err, tokens.ErrDenied) {
		t.Fatalf("unknown token: %v", err)
	}
	log := r.audit(t)
	if !strings.Contains(log, "token:unknown|token.denied|unknown|unknown token; GET /api/v1/runs") {
		t.Errorf("the audit log:\n%s", log)
	}
	if strings.Contains(log, guess) {
		t.Errorf("a presented token is in the audit log:\n%s", log)
	}
}

func TestNoTokenInTheAuditLogOrAnError(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	tok, err := r.svc.Issue(ctx, "founder", "ci", "operator", expiry.Dates{})
	if err != nil {
		t.Fatal(err)
	}
	_, e1 := r.svc.Issue(ctx, "founder", "ci", "operator", expiry.Dates{})
	r.svc.Authenticate(ctx, tok, "GET /api/v1/runs")
	r.svc.Revoke(ctx, "founder", "ci")
	_, e2 := r.svc.Authenticate(ctx, tok, "GET /api/v1/runs")
	for _, e := range []error{e1, e2} {
		if e != nil && strings.Contains(e.Error(), tok) {
			t.Errorf("an error carries the token: %v", e)
		}
	}
	if log := r.audit(t); strings.Contains(log, tok) {
		t.Errorf("the audit log carries the token:\n%s", log)
	}
	// Only the hash is kept.
	var n int
	if err := r.st.DB().QueryRow(`SELECT count(*) FROM tokens WHERE hash = ? OR name = ?`, tok, tok).Scan(&n); err != nil || n != 0 {
		t.Errorf("the token itself is stored (%d rows), %v", n, err)
	}
}

func TestIssueRefusesBadInput(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.svc.Issue(ctx, "founder", "ci", "operator", expiry.Dates{})
	for name, c := range map[string]struct {
		name, scope string
		d           expiry.Dates
	}{
		"unknown scope":  {"a", "root", expiry.Dates{}},
		"empty scope":    {"a", "", expiry.Dates{}},
		"bad name":       {"has space", "operator", expiry.Dates{}},
		"duplicate name": {"ci", "operator", expiry.Dates{}},
		"late reminder":  {"b", "operator", expiry.Dates{Expires: day("2027-01-01"), Remind: day("2027-02-01")}},
	} {
		if tok, err := r.svc.Issue(ctx, "founder", c.name, c.scope, c.d); err == nil || tok != "" {
			t.Errorf("%s: accepted (%q)", name, tok)
		}
	}
	if list, _ := r.svc.List(ctx); len(list) != 1 {
		t.Errorf("a refused Issue left a token: %+v", list)
	}
}

func TestTwoTokensAreNotAlike(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	a, _ := r.svc.Issue(ctx, "f", "a", "operator", expiry.Dates{})
	b, _ := r.svc.Issue(ctx, "f", "b", "operator", expiry.Dates{})
	if a == b || len(a) < 40 {
		t.Errorf("tokens %q and %q", a, b)
	}
	if pa, _ := r.svc.Authenticate(ctx, a, "x"); pa.Name != "a" {
		t.Errorf("token a acts as %q", pa.Name)
	}
}

func TestBearer(t *testing.T) {
	for in, want := range map[string]string{"Bearer abc": "abc", "Bearer ": "", "abc": "", "": "", "bearer abc": ""} {
		got, ok := tokens.Token(in)
		if got != want || ok != (want != "") {
			t.Errorf("Token(%q) = %q, %v", in, got, ok)
		}
	}
}
