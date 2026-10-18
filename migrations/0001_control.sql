CREATE TABLE runs (
  id         INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
  task       INTEGER NOT NULL,
  started_at TEXT    NOT NULL,
  tier       TEXT    NOT NULL,
  job        TEXT    NOT NULL,
  model      TEXT    NOT NULL,
  outcome    TEXT    NOT NULL
) STRICT;

CREATE INDEX runs_by_task ON runs (task, id);

-- Append-only: the store has no statement that updates or deletes a row,
-- and these triggers refuse one from anywhere else.
CREATE TABLE audit (
  id      INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
  at      TEXT    NOT NULL,
  actor   TEXT    NOT NULL,
  action  TEXT    NOT NULL,
  subject TEXT    NOT NULL,
  detail  TEXT    NOT NULL
) STRICT;

CREATE TRIGGER audit_no_update BEFORE UPDATE ON audit
BEGIN SELECT RAISE(ABORT, 'the audit log is append-only'); END;

CREATE TRIGGER audit_no_delete BEFORE DELETE ON audit
BEGIN SELECT RAISE(ABORT, 'the audit log is append-only'); END;
