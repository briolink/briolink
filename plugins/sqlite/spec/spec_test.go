package spec

import (
	"os"
	"strings"
	"testing"
)

func TestPilotIsValid(t *testing.T) {
	qs, err := LoadDir("testdata/queries")
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 4 {
		t.Fatalf("got %d queries, want 4", len(qs))
	}
	q := qs[1]
	if q.Name != "list_invoices_by_customer" || q.Cardinality != ":many" || len(q.Params) != 2 || q.Columns[1] != (Column{"state", "string"}) {
		t.Fatalf("got %+v", q)
	}
	if qs[0].Columns[4].Type != "?string" {
		t.Fatalf("nullable type lost: %+v", qs[0].Columns[4])
	}
}

func TestRules(t *testing.T) {
	const ok = "-- name: q :one\n-- param: id string\n-- column: id string\nSELECT id FROM t WHERE id = ?;\n"
	cases := []struct {
		name, src, want string
	}{
		{"text before header", "SELECT 1;\n" + ok, "f.sql:1: text before the first -- name: header"},
		{"name line shape", "-- name: q\nSELECT 1;", `want "-- name: <name> <:one|:many|:exec>"`},
		{"name case", strings.Replace(ok, "name: q ", "name: GetQ ", 1), `query name must be snake_case, got "GetQ"`},
		{"cardinality", strings.Replace(ok, ":one", ":first", 1), `q: cardinality must be :one, :many or :exec, got ":first"`},
		{"unknown directive", strings.Replace(ok, "-- param:", "-- arg:", 1), `f.sql:2: unknown header directive "arg"`},
		{"free comment in header", strings.Replace(ok, "-- param: id string\n", "-- param: id string\n-- fetch one\n", 1), "f.sql:3: header lines must be name, param or column directives"},
		{"param shape", strings.Replace(ok, "id string\n-- column", "id\n-- column", 1), `want "-- param: <name> <type>"`},
		{"param name", strings.Replace(ok, "param: id", "param: Id", 1), `q: param name must be snake_case, got "Id"`},
		{"param type", strings.Replace(ok, "param: id string", "param: id text", 1), `q: param id has unknown type "text"`},
		{"param twice", strings.Replace(ok, "-- param: id string\n", "-- param: id string\n-- param: id int\n", 1), `q: param "id" is declared twice`},
		{"column twice", strings.Replace(ok, "-- column: id string\n", "-- column: id string\n-- column: id int\n", 1), `q: column "id" is declared twice`},
		{"column type", strings.Replace(ok, "column: id string", "column: id ??string", 1), `q: column id has unknown type "??string"`},
		{"directive after statement", ok + "-- param: x int\n", "f.sql:5: -- param: must be in the header"},
		{"exec with columns", strings.Replace(ok, ":one", ":exec", 1), "q: an :exec query returns no columns"},
		{"one without columns", strings.Replace(ok, "-- column: id string\n", "", 1), "q: a :one query must declare its result columns"},
		{"no statement", "-- name: q :exec\n", "q: has no statement"},
		{"two statements", strings.Replace(ok, "= ?;", "= ?; DELETE FROM t;", 1), "q: must hold exactly one statement ending in ;"},
		{"no semicolon", strings.Replace(ok, "= ?;", "= ?", 1), "q: must hold exactly one statement ending in ;"},
		{"placeholder count", strings.Replace(ok, "WHERE id = ?", "WHERE id = 1", 1), "q: the statement has 0 ? placeholders but 1 params are declared"},
		{"numbered placeholder", strings.Replace(ok, "= ?;", "= ?1;", 1), "numbered placeholders (?1) are not allowed"},
		{"named placeholder", strings.Replace(ok, "= ?;", "= :id;", 1), "named placeholders (:id) are not allowed"},
		{"dollar placeholder", strings.Replace(ok, "= ?;", "= $id;", 1), "named placeholders ($id) are not allowed"},
		{"unterminated quote", strings.Replace(ok, "= ?;", "= ? AND x = 'a;", 1), "q: unterminated quote"},
		{"unterminated comment", strings.Replace(ok, "= ?;", "= ? /* x;", 1), "q: unterminated /* comment"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse("f.sql", c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v\nwant error containing %q", err, c.want)
			}
		})
	}
	if _, err := Parse("f.sql", ok); err != nil {
		t.Fatalf("base case must be valid: %v", err)
	}
}

func TestNamesUniqueAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	q := "-- name: get_x :exec\nDELETE FROM x;\n"
	os.WriteFile(dir+"/a.sql", []byte(q), 0o600)
	os.WriteFile(dir+"/b.sql", []byte(q), 0o600)
	_, err := LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), `b.sql:1: query "get_x" is already declared at `+dir+`/a.sql:1`) {
		t.Fatalf("got %v", err)
	}
}
