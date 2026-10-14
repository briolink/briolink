package spike

import (
	"context"
	"database/sql"
	_ "embed"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/plugins/sqlite/spec"
)

//go:embed schema.sql
var schema string

func open(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(context.Background(), schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func parse(t *testing.T, src string) spec.Query {
	t.Helper()
	qs, err := spec.Parse("q.sql", src)
	if err != nil {
		t.Fatalf("header check: %v", err)
	}
	return qs[0]
}

// TestPilotPasses: the spec package's own fixture queries check clean.
func TestPilotPasses(t *testing.T) {
	db := open(t)
	qs, err := spec.LoadDir("../spec/testdata/queries")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for _, q := range qs {
		if p := Check(context.Background(), db, q); len(p) > 0 {
			t.Errorf("%s: %v", q.Name, p)
		}
	}
	t.Logf("%d queries checked in %v", len(qs), time.Since(start))
}

// TestCases is the measurement: each case is one mistake, and want is what
// Check reports ("" means it reports nothing: the mistake gets through).
func TestCases(t *testing.T) {
	const get = "-- name: q :one\n-- param: id string\n"
	cases := []struct {
		name, src, want string
	}{
		// Prepare: the schema.
		{"wrong table", get + "-- column: id string\nSELECT id FROM invoice WHERE id = ?;", "no such table: invoice"},
		{"wrong column in select", get + "-- column: id string\nSELECT idd AS id FROM invoices WHERE id = ?;", "no such column: idd"},
		{"wrong column in WHERE", get + "-- column: id string\nSELECT id FROM invoices WHERE customr = ?;", "no such column: customr"},
		{"wrong column in ORDER BY", get + "-- column: id string\nSELECT id FROM invoices WHERE id = ? ORDER BY numbr;", "no such column: numbr"},
		{"wrong column in subquery", get + "-- column: id string\nSELECT id FROM invoices WHERE id IN (SELECT invoice FROM lines WHERE invoice_id = ?);", "no such column: invoice"},
		{"wrong column in UPDATE SET", "-- name: q :exec\n-- param: s string\n-- param: id string\nUPDATE invoices SET stat = ? WHERE id = ?;", "no such column: stat"},
		{"wrong column in INSERT", "-- name: q :exec\n-- param: id string\n-- param: s string\nINSERT INTO invoices (id, stat) VALUES (?, ?);", "has no column named stat"},
		{"INSERT value count", "-- name: q :exec\n-- param: id string\nINSERT INTO invoices (id, state) VALUES (?);", "1 values for 2 columns"},
		{"ambiguous column", "-- name: q :many\n-- param: p int\n-- column: invoice_id string\nSELECT invoice_id FROM lines a JOIN lines b USING (position) WHERE position = ?;", "ambiguous column name: invoice_id"},
		{"double-quoted misspelt column", get + "-- column: id string\nSELECT id FROM invoices WHERE \"customr\" = ?;", `no such column: "customr"`},
		{"unknown function", get + "-- column: id string\nSELECT lowr(id) AS id FROM invoices WHERE id = ?;", "no such function: lowr"},
		{"NOT NULL column left out of INSERT", "-- name: q :exec\n-- param: id string\nINSERT INTO invoices (id) VALUES (?);", ""},

		// Parameters.
		{"param type (string compared with INTEGER)", "-- name: q :many\n-- param: paid string\n-- column: id string\nSELECT id FROM invoices WHERE paid = ?;", ""},
		{"param type (string stored in STRICT INTEGER)", "-- name: q :exec\n-- param: q string\nUPDATE lines SET quantity = ?;", ""},
		{"params in LIMIT, IN and subquery counted", "-- name: q :many\n-- param: a string\n-- param: b string\n-- param: c int\n-- param: n int\n-- column: id string\nSELECT id FROM invoices WHERE customer IN (?, ?) AND id IN (SELECT invoice_id FROM lines WHERE quantity > ?) LIMIT ?;", ""},

		// Result columns.
		{"too few columns declared", get + "-- column: id string\nSELECT id, state FROM invoices WHERE id = ?;", "the database returns 2 columns, 1 are declared"},
		{"columns swapped", get + "-- column: state string\n-- column: id string\nSELECT id, state FROM invoices WHERE id = ?;", `column 1: the database names it "id", declared "state"`},
		{"alias keeps the column's type", get + "-- column: invoice_id string\nSELECT id AS invoice_id FROM invoices WHERE id = ?;", ""},
		{"wrong column type", get + "-- column: paid string\nSELECT paid FROM invoices WHERE id = ?;", "column paid: declared string, the database column is INTEGER"},
		{"NOT NULL declared for a nullable column", get + "-- column: number string\nSELECT number FROM invoices WHERE id = ?;", "column number: declared string but invoices.number may be NULL"},
		{"nullable declared for a NOT NULL column", get + "-- column: state ?string\nSELECT state FROM invoices WHERE id = ?;", ""},
		{"time is TEXT", get + "-- column: issued_at ?time\nSELECT issued_at FROM invoices WHERE id = ?;", ""},
		{"expression column", "-- name: q :one\n-- param: c string\n-- column: n int\nSELECT count(*) AS n FROM invoices WHERE customer = ?;", "column n: an expression, its type int can't be checked"},
		{"CAST column", get + "-- column: paid int\nSELECT CAST(paid AS INTEGER) AS paid FROM invoices WHERE id = ?;", "column paid: an expression"},
		{"LEFT JOIN makes a NOT NULL column nullable", get + "-- column: id string\n-- column: description string\nSELECT i.id, l.description FROM invoices i LEFT JOIN lines l ON l.invoice_id = i.id WHERE i.id = ?;", ""},
		{"RETURNING on an :exec", "-- name: q :exec\n-- param: id string\nDELETE FROM invoices WHERE id = ? RETURNING id;", "the database returns 1 columns, 0 are declared"},
		{"non-STRICT table, odd declared type", "-- name: q :one\n-- param: id string\n-- column: count int\nSELECT count FROM loose WHERE id = ?;", ""},
	}
	db := open(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strings.Join(Check(context.Background(), db, parse(t, c.src)), "; ")
			if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
				t.Fatalf("got %q\nwant %q", got, c.want)
			}
			if got == "" {
				t.Log("not reported")
			} else {
				t.Log(got)
			}
		})
	}
}

// TestParamCountMismatch: a declared list that disagrees with the statement
// is caught from the compiled program, independently of the header parser.
func TestParamCountMismatch(t *testing.T) {
	db := open(t)
	for _, c := range []struct {
		params []spec.Column
		want   string
	}{
		{[]spec.Column{{Name: "id", Type: "string"}}, "explain: missing argument with index 2"},
		{[]spec.Column{{Name: "s", Type: "string"}, {Name: "id", Type: "string"}, {Name: "x", Type: "int"}}, "the database counts 2 parameters, 3 are declared"},
	} {
		q := spec.Query{Name: "q", Cardinality: ":exec", Params: c.params, SQL: "UPDATE invoices SET state = ? WHERE id = ?;"}
		got := strings.Join(Check(context.Background(), db, q), "; ")
		if !strings.Contains(got, c.want) {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

// TestDQSDefault: with SQLite's default, a misspelt double-quoted column is
// silently a string literal; Open turns that off.
func TestDQSDefault(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	q := parse(t, "-- name: q :many\n-- param: id string\n-- column: id string\nSELECT id FROM invoices WHERE \"customr\" = ?;")
	if p := Check(context.Background(), db, q); len(p) != 0 {
		t.Fatalf("expected the default to let it through, got %v", p)
	}
}

// TestExecution: what only running the statement shows, in a rolled-back
// transaction on the empty migrated schema.
func TestExecution(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	run := func(query string, args ...any) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, "INSERT INTO invoices (id, customer, currency, state) VALUES ('i1', 'c', 'EUR', 'draft')"); err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(ctx, query, args...)
		return err
	}
	for _, c := range []struct {
		name, query string
		args        []any
		wantErr     string
	}{
		{"STRICT rejects a string in INTEGER", "INSERT INTO lines VALUES ('i1', 1, 'd', ?, 1)", []any{"x"}, "cannot store TEXT value in INTEGER column lines.quantity"},
		{"STRICT converts a numeric string", "INSERT INTO lines VALUES ('i1', 1, 'd', ?, 1)", []any{"3"}, ""},
		{"non-STRICT stores a string in INTEGER", "INSERT INTO loose VALUES ('a', ?)", []any{"x"}, ""},
		{"missing NOT NULL column", "INSERT INTO invoices (id) VALUES (?)", []any{"i2"}, "NOT NULL constraint failed: invoices.customer"},
		{"too few arguments", "UPDATE invoices SET state = ? WHERE id = ?", []any{"paid"}, "missing argument with index 2"},
		{"too many arguments", "UPDATE invoices SET state = ? WHERE id = ?", []any{"paid", "i1", "extra"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := run(c.query, c.args...)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if (c.wantErr == "") != (got == "") || !strings.Contains(got, c.wantErr) {
				t.Fatalf("got %q\nwant %q", got, c.wantErr)
			}
			if got == "" {
				t.Log("no error")
			} else {
				t.Log(got)
			}
		})
	}
}

// TestStrictFlag: the generator can require STRICT tables from the schema.
func TestStrictFlag(t *testing.T) {
	db := open(t)
	rows, err := db.Query("SELECT name, strict FROM pragma_table_list WHERE schema = 'main' AND type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var name string
		var strict int
		if err := rows.Scan(&name, &strict); err != nil {
			t.Fatal(err)
		}
		got[name] = strict
	}
	if got["invoices"] != 1 || got["lines"] != 1 || got["loose"] != 0 {
		t.Fatalf("got %v", got)
	}
}

// TestScanNull: a declared non-null column scanned into a plain Go value
// fails at run time on a NULL, which covers what the LEFT JOIN case hides.
func TestScanNull(t *testing.T) {
	db := open(t)
	if _, err := db.Exec("INSERT INTO invoices (id, customer, currency, state) VALUES ('i1', 'c', 'EUR', 'draft')"); err != nil {
		t.Fatal(err)
	}
	var id, description string
	err := db.QueryRow("SELECT i.id, l.description FROM invoices i LEFT JOIN lines l ON l.invoice_id = i.id WHERE i.id = 'i1'").Scan(&id, &description)
	if err == nil || !strings.Contains(err.Error(), "converting NULL to string is unsupported") {
		t.Fatalf("got %v", err)
	}
	t.Log(err)
}
