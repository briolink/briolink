// Package gen is the generator plugins/sqlite ships for the "queries" spec
// kind (docs/2-architecture.md § Specs and generators): it applies a
// project's migrations to an in-memory database, checks every
// spec/queries/*.sql statement against that schema without running it, and
// emits typed Go functions plus a test that repeats the check.
//
// The check is the one measured for issue #13 (plugins/sqlite/spike); this
// package is its production home.
package gen

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/briolink/briolink/plugins/sqlite/spec"
	"modernc.org/sqlite"
)

// Open returns a one-connection in-memory database with double-quoted
// string literals off (_dqs=0), so a misspelt "column" is an error rather
// than a string, and schema applied.
func Open(ctx context.Context, schema string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file::memory:?_dqs=0")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Affinities maps each spec type to the declared column types it accepts.
// SQLite has no time or bool type: time is TEXT (RFC 3339, UTC) and bool is
// INTEGER (0 or 1).
var Affinities = map[string][]string{
	"string": {"TEXT"},
	"int":    {"INTEGER", "INT"},
	"float":  {"REAL"},
	"bool":   {"INTEGER", "INT"},
	"time":   {"TEXT"},
	"bytes":  {"BLOB"},
}

// RequireStrict reports every user table (excluding sqlite_* internal
// tables) that isn't declared STRICT.
func RequireStrict(ctx context.Context, db *sql.DB) []string {
	rows, err := db.QueryContext(ctx,
		`SELECT name, strict FROM pragma_table_list WHERE schema = 'main' AND type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return []string{fmt.Sprintf("listing tables: %v", err)}
	}
	defer rows.Close()
	var problems []string
	for rows.Next() {
		var name string
		var strict int
		if err := rows.Scan(&name, &strict); err != nil {
			return []string{fmt.Sprintf("listing tables: %v", err)}
		}
		if strict == 0 {
			problems = append(problems, fmt.Sprintf("table %s is not STRICT", name))
		}
	}
	return problems
}

// Check prepares q against db's schema and compares the result columns the
// database reports with the declared ones. It never executes q.
func Check(ctx context.Context, db *sql.DB, q spec.Query) []string {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	info, err := columnInfo(ctx, db, q.SQL)
	if err != nil {
		add("prepare: %v", err)
		return problems
	}
	if n, err := paramCount(ctx, db, q.SQL, len(q.Params)); err != nil {
		add("explain: %v", err)
	} else if n != len(q.Params) {
		add("the database counts %d parameters, %d are declared", n, len(q.Params))
	}
	if len(info) != len(q.Columns) {
		add("the database returns %d columns, %d are declared", len(info), len(q.Columns))
		return problems
	}
	for i, got := range info {
		want := q.Columns[i]
		if got.Name != want.Name {
			add("column %d: the database names it %q, declared %q", i+1, got.Name, want.Name)
		}
		base, nullable := strings.CutPrefix(want.Type, "?")
		if got.DeclType == "" {
			add("column %s: an expression, its type %s can't be checked", want.Name, want.Type)
			continue
		}
		if !accepts(base, got.DeclType) {
			add("column %s: declared %s, the database column is %s", want.Name, want.Type, got.DeclType)
		}
		if !nullable && got.TableName != "" {
			notNull, err := columnNotNull(ctx, db, got.TableName, got.OriginName)
			if err != nil {
				add("column %s: %v", want.Name, err)
			} else if !notNull {
				add("column %s: declared %s but %s.%s may be NULL", want.Name, want.Type, got.TableName, got.OriginName)
			}
		}
	}
	return problems
}

func accepts(specType, declType string) bool {
	for _, t := range Affinities[specType] {
		if strings.EqualFold(t, declType) {
			return true
		}
	}
	return false
}

func columnInfo(ctx context.Context, db *sql.DB, query string) (info []sqlite.ColumnInfo, err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	err = conn.Raw(func(dc any) error {
		ci, ok := dc.(interface {
			ColumnInfo(string) ([]sqlite.ColumnInfo, error)
		})
		if !ok {
			return fmt.Errorf("driver has no ColumnInfo")
		}
		info, err = ci.ColumnInfo(query)
		return err
	})
	return info, err
}

// paramCount is the highest parameter number the compiled program reads:
// with bare ? placeholders, sqlite3_bind_parameter_count, which the driver
// doesn't expose (its NumInput is -1). EXPLAIN runs with the declared number
// of NULLs: the driver rejects a missing argument ("missing argument with
// index n") and ignores extra ones, so too few declared is an error and too
// many shows in the count.
func paramCount(ctx context.Context, db *sql.DB, query string, declared int) (int, error) {
	rows, err := db.QueryContext(ctx, "EXPLAIN "+query, make([]any, declared)...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	maxN := 0
	for rows.Next() {
		var addr, p1, p2, p3 int
		var opcode string
		var p4, p5, comment any
		if err := rows.Scan(&addr, &opcode, &p1, &p2, &p3, &p4, &p5, &comment); err != nil {
			return 0, err
		}
		if opcode == "Variable" && p1 > maxN {
			maxN = p1
		}
	}
	return maxN, rows.Err()
}

func columnNotNull(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	var notNull, pk int
	err := db.QueryRowContext(ctx,
		`SELECT "notnull", pk FROM pragma_table_xinfo(?) WHERE name = ?`, table, column).Scan(&notNull, &pk)
	if err != nil {
		return false, fmt.Errorf("%s.%s: %w", table, column, err)
	}
	return notNull == 1, nil
}
