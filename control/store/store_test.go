package store

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/gen/domain/control"
	"github.com/briolink/briolink/migrations"
)

var t0 = time.Date(2026, 10, 17, 9, 30, 0, 123456789, time.UTC)

func open(t *testing.T) (*Store, *events.Broker, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.db")
	b := events.New()
	s, err := Open(context.Background(), path, b, func() time.Time { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, b, path
}

func TestRunsRoundTripAndPublish(t *testing.T) {
	ctx := context.Background()
	s, b, _ := open(t)
	_, live, stop := b.Subscribe(0)
	defer stop()

	a, err := s.AddRun(ctx, NewRun{Task: 7, Tier: "coder", Job: "pure_function", Model: "qwen3-coder:30b", Outcome: control.RunOutcomeRunning})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRun(ctx, NewRun{Task: 8, Tier: "top", Job: "escalation.second", Model: "m", Outcome: control.RunOutcomePassed}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRun(ctx, NewRun{Task: 7, Tier: "routine", Job: "escalation.first", Model: "m", Outcome: control.RunOutcomeEscalated}); err != nil {
		t.Fatal(err)
	}
	if a.Id != 1 || !a.StartedAt.Equal(t0) {
		t.Errorf("record = %+v", a)
	}

	all, err := s.ListRuns(ctx, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].Id != 3 || all[2].Id != 1 {
		t.Fatalf("newest first expected: %+v", all)
	}
	if all[2] != a {
		t.Errorf("stored %+v, added %+v", all[2], a)
	}
	seven := int64(7)
	of7, err := s.ListRuns(ctx, &seven, 10)
	if err != nil || len(of7) != 2 || of7[0].Id != 3 || of7[1].Id != 1 {
		t.Fatalf("task 7: %+v, %v", of7, err)
	}
	if two, _ := s.ListRuns(ctx, nil, 2); len(two) != 2 {
		t.Errorf("limit 2 gave %d", len(two))
	}
	none := int64(99)
	if got, err := s.ListRuns(ctx, &none, 10); err != nil || got == nil || len(got) != 0 {
		t.Errorf("an empty list must be an empty slice, got %#v, %v", got, err)
	}

	wantData := `{"id":1,"task":7,"started_at":"2026-10-17T09:30:00.123456789Z","tier":"coder","job":"pure_function","model":"qwen3-coder:30b","outcome":"running"}`
	if e := <-live; e.Type != "run" || string(e.Data) != wantData {
		t.Errorf("event = %s %s, want %s", e.Type, e.Data, wantData)
	}
}

func TestRunsAreRefusedWhenIncomplete(t *testing.T) {
	s, b, _ := open(t)
	good := NewRun{Task: 1, Tier: "coder", Job: "j", Model: "m", Outcome: control.RunOutcomeRunning}
	for name, mod := range map[string]func(*NewRun){
		"task":    func(r *NewRun) { r.Task = 0 },
		"tier":    func(r *NewRun) { r.Tier = "" },
		"job":     func(r *NewRun) { r.Job = "" },
		"model":   func(r *NewRun) { r.Model = "" },
		"outcome": func(r *NewRun) { r.Outcome = "done" },
	} {
		in := good
		mod(&in)
		if _, err := s.AddRun(context.Background(), in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if missed, _, stop := b.Subscribe(0); len(missed) != 0 {
		t.Errorf("a refused write published %+v", missed)
	} else {
		stop()
	}
}

func TestAuditAppendsNewestFirstAndRefusesChange(t *testing.T) {
	ctx := context.Background()
	s, b, _ := open(t)
	_, live, stop := b.Subscribe(0)
	defer stop()

	e1, err := s.AddAudit(ctx, "control", "start", "control plane", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddAudit(ctx, "operator", "read", "routing", ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListAudit(ctx, 10)
	if err != nil || len(got) != 2 || got[0].Action != "read" || got[1] != e1 {
		t.Fatalf("audit = %+v, %v", got, err)
	}
	wantData := `{"id":1,"at":"2026-10-17T09:30:00.123456789Z","actor":"control","action":"start","subject":"control plane","detail":"v1"}`
	if e := <-live; e.Type != "audit" || string(e.Data) != wantData {
		t.Errorf("event = %s %s, want %s", e.Type, e.Data, wantData)
	}

	for _, stmt := range []string{`UPDATE audit SET actor = 'x'`, `DELETE FROM audit`} {
		_, err := s.db.ExecContext(ctx, stmt)
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s: err = %v", stmt, err)
		}
	}
	if got, _ := s.ListAudit(ctx, 10); len(got) != 2 {
		t.Errorf("rows changed: %+v", got)
	}
	for _, in := range [][2]string{{"", "a"}, {"a", ""}} {
		if _, err := s.AddAudit(ctx, in[0], in[1], "", ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: err = %v", in, err)
		}
	}
}

func TestReopenKeepsDataAndAppliesMigrationsOnce(t *testing.T) {
	ctx := context.Background()
	s, b, path := open(t)
	if _, err := s.AddAudit(ctx, "a", "b", "", ""); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2, err := Open(ctx, path, b, func() time.Time { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got, err := s2.ListAudit(ctx, 5); err != nil || len(got) != 1 {
		t.Fatalf("after reopen: %+v, %v", got, err)
	}
	files, _ := fs.Glob(migrations.FS, "*.sql")
	var n int
	if err := s2.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != len(files) {
		t.Errorf("migrations recorded = %d, want one per file (%d), %v", n, len(files), err)
	}
}

func TestOpenFailsOnAnUnusablePath(t *testing.T) {
	if _, err := Open(context.Background(), filepath.Join(t.TempDir(), "no", "such", "dir", "c.db"), events.New(), time.Now); err == nil {
		t.Fatal("opened a database in a missing directory")
	}
}
