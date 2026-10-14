CREATE TABLE invoices (
  id          TEXT    NOT NULL PRIMARY KEY,
  customer    TEXT    NOT NULL,
  currency    TEXT    NOT NULL,
  state       TEXT    NOT NULL,
  number      TEXT,
  issued_at   TEXT,
  paid        INTEGER NOT NULL DEFAULT 0,
  void_reason TEXT,
  note        TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE lines (
  invoice_id  TEXT    NOT NULL REFERENCES invoices(id),
  position    INTEGER NOT NULL,
  description TEXT    NOT NULL,
  quantity    INTEGER NOT NULL,
  unit_price  INTEGER NOT NULL,
  PRIMARY KEY (invoice_id, position)
) STRICT;

CREATE TABLE loose (
  id    TEXT NOT NULL PRIMARY KEY,
  count INTEGER NOT NULL
);
