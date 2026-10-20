// Package store is the control plane's own SQLite database (E2): the run
// records and the append-only audit log. It applies the migrations at open,
// and publishes an event after each write that committed.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"time"

	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/gen/domain/control"
	sqlitegen "github.com/briolink/briolink/gen/sqlite"
	"github.com/briolink/briolink/migrations"

	_ "modernc.org/sqlite" // the database/sql driver
)

// Publisher receives the events a write produces. *events.Broker is one.
type Publisher interface {
	Publish(typ string, v any) events.Event
}

// ErrInvalid: a write was refused before it reached the database.
var ErrInvalid = errors.New("store: invalid argument")

var outcomes = map[control.RunOutcome]bool{
	control.RunOutcomeRunning: true, control.RunOutcomePassed: true, control.RunOutcomeFailed: true,
	control.RunOutcomeEscalated: true, control.RunOutcomeCancelled: true,
}

// Store is the database and what it publishes. It is safe for concurrent use.
type Store struct {
	db  *sql.DB
	q   *sqlitegen.Queries
	pub Publisher
	now func() time.Time
}

// Open opens (creating if needed) the database at path and applies the
// migrations not yet applied. Writes take the database's write lock when
// they begin and wait up to five seconds for it (A26).
func Open(ctx context.Context, path string, pub Publisher, now func() time.Time) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, q: sqlitegen.New(db), pub: pub, now: now}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return s, nil
}

// DB is the open database, for the control plane's other stores (secrets,
// tokens) that keep their tables in it.
func (s *Store) DB() *sql.DB { return s.db }

// Now is the store's clock.
func (s *Store) Now() time.Time { return s.now() }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT NOT NULL PRIMARY KEY, applied_at TEXT NOT NULL) STRICT`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var applied string
		err := s.db.QueryRowContext(ctx, `SELECT name FROM schema_migrations WHERE name = ?`, name).Scan(&applied)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, name, s.now().UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// runEvent and auditEvent are the data of the events: the same JSON the API
// serves for the record (snake_case keys, times in UTC), which the generated
// handlers keep to themselves.
type runEvent struct {
	ID        int64     `json:"id"`
	Task      int64     `json:"task"`
	StartedAt time.Time `json:"started_at"`
	Tier      string    `json:"tier"`
	Job       string    `json:"job"`
	Model     string    `json:"model"`
	Outcome   string    `json:"outcome"`
}

type auditEvent struct {
	ID      int64     `json:"id"`
	At      time.Time `json:"at"`
	Actor   string    `json:"actor"`
	Action  string    `json:"action"`
	Subject string    `json:"subject"`
	Detail  string    `json:"detail"`
}

// NewRun is what the runner knows when it starts a run.
type NewRun struct {
	Task    int64
	Tier    string
	Job     string // the routing row that chose the tier
	Model   string
	Outcome control.RunOutcome
}

// AddRun records a run, stamped with the store's clock, and publishes a "run"
// event with the record.
func (s *Store) AddRun(ctx context.Context, in NewRun) (control.RunRecord, error) {
	if in.Task < 1 || in.Tier == "" || in.Job == "" || in.Model == "" || !outcomes[in.Outcome] {
		return control.RunRecord{}, fmt.Errorf("%w: a run needs a task, a tier, a job, a model and a known outcome", ErrInvalid)
	}
	at := s.now().UTC()
	row, err := s.q.InsertRun(ctx, in.Task, formatTime(at), in.Tier, in.Job, in.Model, string(in.Outcome))
	if err != nil {
		return control.RunRecord{}, err
	}
	rec := control.RunRecord{Id: row.Id, Task: in.Task, StartedAt: at, Tier: in.Tier, Job: in.Job, Model: in.Model, Outcome: in.Outcome}
	s.pub.Publish("run", runEvent{rec.Id, rec.Task, rec.StartedAt, rec.Tier, rec.Job, rec.Model, string(rec.Outcome)})
	return rec, nil
}

// ListRuns returns up to limit runs, newest first; with task non-nil, only
// that task's.
func (s *Store) ListRuns(ctx context.Context, task *int64, limit int64) ([]control.RunRecord, error) {
	out := []control.RunRecord{}
	if task != nil {
		rows, err := s.q.ListRunsByTask(ctx, *task, limit)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			rec, err := run(r.Id, r.Task, r.StartedAt, r.Tier, r.Job, r.Model, r.Outcome)
			if err != nil {
				return nil, err
			}
			out = append(out, rec)
		}
		return out, nil
	}
	rows, err := s.q.ListRuns(ctx, limit)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		rec, err := run(r.Id, r.Task, r.StartedAt, r.Tier, r.Job, r.Model, r.Outcome)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func run(id, task int64, at, tier, job, model, outcome string) (control.RunRecord, error) {
	t, err := parseTime(at)
	if err != nil {
		return control.RunRecord{}, err
	}
	return control.RunRecord{Id: id, Task: task, StartedAt: t, Tier: tier, Job: job, Model: model, Outcome: control.RunOutcome(outcome)}, nil
}

// Times are stored as RFC 3339 text in UTC. The generated queries take and
// return them as strings: the driver does not read a time back from a STRICT
// TEXT column (#72).
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: bad time %q in the database: %w", s, err)
	}
	return t.UTC(), nil
}

// AddAudit appends a line to the audit log, stamped with the store's clock,
// and publishes an "audit" event with the entry. The log has no update and no
// delete (the database refuses both).
func (s *Store) AddAudit(ctx context.Context, actor, action, subject, detail string) (control.AuditEntry, error) {
	if actor == "" || action == "" {
		return control.AuditEntry{}, fmt.Errorf("%w: an audit entry needs an actor and an action", ErrInvalid)
	}
	at := s.now().UTC()
	row, err := s.q.InsertAudit(ctx, formatTime(at), actor, action, subject, detail)
	if err != nil {
		return control.AuditEntry{}, err
	}
	e := control.AuditEntry{Id: row.Id, At: at, Actor: actor, Action: action, Subject: subject, Detail: detail}
	s.pub.Publish("audit", auditEvent{e.Id, e.At, e.Actor, e.Action, e.Subject, e.Detail})
	return e, nil
}

// ListAudit returns up to limit entries, newest first.
func (s *Store) ListAudit(ctx context.Context, limit int64) ([]control.AuditEntry, error) {
	rows, err := s.q.ListAudit(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]control.AuditEntry, 0, len(rows))
	for _, r := range rows {
		at, err := parseTime(r.At)
		if err != nil {
			return nil, err
		}
		out = append(out, control.AuditEntry{Id: r.Id, At: at, Actor: r.Actor, Action: r.Action, Subject: r.Subject, Detail: r.Detail})
	}
	return out, nil
}
