package gen

import (
	"context"
	_ "embed"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/briolink/briolink/plugins/sqlite/spec"
)

//go:embed testdata/schema.sql
var pilotSchema string

func pilotQueries(t *testing.T) []spec.Query {
	t.Helper()
	qs, err := spec.LoadDir("../spec/testdata/queries")
	if err != nil {
		t.Fatal(err)
	}
	return qs
}

// TestGenerate_Pilot: the spec package's own fixture queries generate
// clean, compilable Go source and a check test, against the matching
// schema.
func TestGenerate_Pilot(t *testing.T) {
	files, err := Generate(context.Background(), Input{
		Package: "queries",
		Schema:  pilotSchema,
		Queries: pilotQueries(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"queries.go", "queries_check_test.go"} {
		src, ok := files[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if _, err := parser.ParseFile(token.NewFileSet(), name, src, parser.AllErrors); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, src)
		}
	}
	code := string(files["queries.go"])
	for _, want := range []string{
		"func (q *Queries) GetInvoice(ctx context.Context, id string) (GetInvoiceRow, error)",
		"func (q *Queries) ListInvoicesByCustomer(ctx context.Context, customer string, limit int64) ([]ListInvoicesByCustomerRow, error)",
		"func (q *Queries) SaveInvoice(ctx context.Context, state string, paid int64, id string) error",
		"Number", "sql.NullString",
		"IssuedAt", "sql.NullTime",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("queries.go doesn't contain %q:\n%s", want, code)
		}
	}
}

// TestGenerate_ImportsTimeOnlyWhenUsed: a file that imports "time" without
// using it does not compile, so the import follows the types the queries use.
func TestGenerate_ImportsTimeOnlyWhenUsed(t *testing.T) {
	gen := func(queries []spec.Query) string {
		t.Helper()
		files, err := Generate(context.Background(), Input{Package: "queries", Schema: pilotSchema, Queries: queries})
		if err != nil {
			t.Fatal(err)
		}
		return string(files["queries.go"])
	}
	var withTime, withoutTime []spec.Query
	for _, q := range pilotQueries(t) {
		if q.Name == "list_lines" {
			withoutTime = append(withoutTime, q)
		}
		if q.Name == "get_invoice" {
			withTime = append(withTime, q) // issued_at is a ?time column
		}
	}
	if len(withTime) != 1 || len(withoutTime) != 1 {
		t.Fatalf("fixture queries missing: %d, %d", len(withTime), len(withoutTime))
	}
	if code := gen(withoutTime); strings.Contains(code, `"time"`) {
		t.Errorf("imports time without using it:\n%s", code)
	}
	if code := gen(withTime); !strings.Contains(code, `"time"`) {
		t.Errorf("does not import time:\n%s", code)
	}
}

// TestGenerate_RejectsBadQuery: a query that doesn't match the schema
// fails generation with the same problem the check would report, and
// nothing is returned.
func TestGenerate_RejectsBadQuery(t *testing.T) {
	bad := []spec.Query{{
		File: "q.sql", Line: 1, Name: "q", Cardinality: ":one",
		Params:  []spec.Column{{Name: "id", Type: "string"}},
		Columns: []spec.Column{{Name: "id", Type: "string"}},
		SQL:     "SELECT id FROM invoice WHERE id = ?;",
	}}
	_, err := Generate(context.Background(), Input{Package: "queries", Schema: pilotSchema, Queries: bad})
	if err == nil || !strings.Contains(err.Error(), "no such table: invoice") {
		t.Fatalf("got %v", err)
	}
}

// TestGenerate_RejectsNonStrictTable: the generator requires every table
// in the schema to be STRICT, not only the ones a query touches.
func TestGenerate_RejectsNonStrictTable(t *testing.T) {
	schema := pilotSchema + "\nCREATE TABLE loose (id TEXT NOT NULL PRIMARY KEY);\n"
	_, err := Generate(context.Background(), Input{Package: "queries", Schema: schema, Queries: nil})
	if err == nil || !strings.Contains(err.Error(), "table loose is not STRICT") {
		t.Fatalf("got %v", err)
	}
}

// TestGenerate_RejectsDuplicateNames: two queries with the same name,
// possibly from different files, are rejected before any SQL runs.
func TestGenerate_RejectsDuplicateNames(t *testing.T) {
	dup := []spec.Query{
		{File: "a.sql", Line: 1, Name: "q", Cardinality: ":exec", SQL: "DELETE FROM invoices;"},
		{File: "b.sql", Line: 1, Name: "q", Cardinality: ":exec", SQL: "DELETE FROM lines;"},
	}
	_, err := Generate(context.Background(), Input{Package: "queries", Schema: pilotSchema, Queries: dup})
	if err == nil || !strings.Contains(err.Error(), `query "q" is already declared`) {
		t.Fatalf("got %v", err)
	}
}

// TestGoType covers every spec type, plain and nullable.
func TestGoType(t *testing.T) {
	cases := map[string]string{
		"string": "string", "?string": "sql.NullString",
		"int": "int64", "?int": "sql.NullInt64",
		"float": "float64", "?float": "sql.NullFloat64",
		"bool": "bool", "?bool": "sql.NullBool",
		"time": "time.Time", "?time": "sql.NullTime",
		"bytes": "[]byte", "?bytes": "[]byte",
	}
	for in, want := range cases {
		if got := goType(in); got != want {
			t.Errorf("goType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPascal(t *testing.T) {
	cases := map[string]string{"id": "Id", "void_reason": "VoidReason", "a_b_c": "ABC"}
	for in, want := range cases {
		if got := pascal(in); got != want {
			t.Errorf("pascal(%q) = %q, want %q", in, got, want)
		}
	}
}
