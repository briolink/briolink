// Package spec is format v0 of spec/queries/*.sql: statements with header
// comments giving each one's name, cardinality, typed parameters and typed
// result columns (docs/2-architecture.md § Specs and generators):
//
//	-- name: get_invoice :one
//	-- param: id string
//	-- column: id string
//	-- column: number ?string
//	SELECT id, number FROM invoices WHERE id = ?;
//
// Params are listed in placeholder order and only bare ? placeholders are
// allowed; columns are listed in result order; "?" before a type means the
// value may be NULL. Checking the statement against the schema is the
// generator's job; this package checks the headers and their shape.
package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/briolink/briolink/core/check"
)

// Types are the column and parameter types.
var Types = map[string]bool{"string": true, "int": true, "float": true, "bool": true, "time": true, "bytes": true}

// Cardinalities: one row, any number of rows, or no rows.
var Cardinalities = map[string]bool{":one": true, ":many": true, ":exec": true}

// Query is one annotated statement.
type Query struct {
	File        string
	Line        int
	Name        string
	Cardinality string
	Params      []Column
	Columns     []Column
	SQL         string
}

// Column is a typed name; Type may start with "?" (nullable).
type Column struct {
	Name string
	Type string
}

// Parse reads every query in one file's source. Header lines are strict: an
// unknown "-- key:" directive, a malformed one, or text before the first
// header is an error.
func Parse(file, src string) ([]Query, error) {
	var l check.List
	var qs []Query
	var cur *Query
	inHeader := false
	var body []string
	flush := func() {
		if cur != nil {
			cur.SQL = strings.TrimSpace(strings.Join(body, "\n"))
			qs = append(qs, *cur)
		}
		body = nil
	}
	for i, line := range strings.Split(src, "\n") {
		n := i + 1
		at := fmt.Sprintf("%s:%d", file, n)
		trimmed := strings.TrimSpace(line)
		key, rest, isDirective := directive(trimmed)
		if isDirective && key == "name" {
			flush()
			cur = &Query{File: file, Line: n}
			inHeader = true
			fields := strings.Fields(rest)
			if len(fields) != 2 {
				l.Addf(at, "want \"-- name: <name> <:one|:many|:exec>\", got %q", trimmed)
				continue
			}
			cur.Name, cur.Cardinality = fields[0], fields[1]
			continue
		}
		if cur == nil {
			if trimmed != "" {
				l.Addf(at, "text before the first -- name: header")
			}
			continue
		}
		if isDirective {
			if !inHeader {
				l.Addf(at, "-- %s: must be in the header, before the statement", key)
				continue
			}
			col, ok := column(rest)
			switch {
			case key != "param" && key != "column":
				l.Addf(at, "unknown header directive %q (want name, param or column)", key)
			case !ok:
				l.Addf(at, "want \"-- %s: <name> <type>\", got %q", key, trimmed)
			case key == "param":
				cur.Params = append(cur.Params, col)
			default:
				cur.Columns = append(cur.Columns, col)
			}
			continue
		}
		if inHeader && strings.HasPrefix(trimmed, "--") {
			l.Addf(at, "header lines must be name, param or column directives")
			continue
		}
		inHeader = false
		body = append(body, line)
	}
	flush()
	for _, q := range qs {
		q.validate(&l)
	}
	return qs, l.Err()
}

// directive recognises "-- key: rest".
func directive(line string) (key, rest string, ok bool) {
	after, found := strings.CutPrefix(line, "-- ")
	if !found {
		return "", "", false
	}
	key, rest, found = strings.Cut(after, ":")
	if !found || key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	return key, strings.TrimSpace(rest), true
}

func column(rest string) (Column, bool) {
	f := strings.Fields(rest)
	if len(f) != 2 {
		return Column{}, false
	}
	return Column{Name: f[0], Type: f[1]}, true
}

func (q Query) validate(l *check.List) {
	at := fmt.Sprintf("%s:%d", q.File, q.Line)
	if q.Name == "" {
		return
	}
	if !check.Snake(q.Name) {
		l.Addf(at, "query name must be snake_case, got %q", q.Name)
	}
	if !Cardinalities[q.Cardinality] {
		l.Addf(at, "%s: cardinality must be :one, :many or :exec, got %q", q.Name, q.Cardinality)
	}
	checkColumns(l, at, q.Name, "param", q.Params)
	checkColumns(l, at, q.Name, "column", q.Columns)
	if q.Cardinality == ":exec" && len(q.Columns) > 0 {
		l.Addf(at, "%s: an :exec query returns no columns", q.Name)
	}
	if q.Cardinality != ":exec" && len(q.Columns) == 0 {
		l.Addf(at, "%s: a %s query must declare its result columns", q.Name, q.Cardinality)
	}
	if q.SQL == "" {
		l.Addf(at, "%s: has no statement", q.Name)
		return
	}
	n, statements, err := scan(q.SQL)
	if err != "" {
		l.Addf(at, "%s: %s", q.Name, err)
		return
	}
	if statements != 1 || !strings.HasSuffix(q.SQL, ";") {
		l.Addf(at, "%s: must hold exactly one statement ending in ;", q.Name)
	}
	if n != len(q.Params) {
		l.Addf(at, "%s: the statement has %d ? placeholders but %d params are declared", q.Name, n, len(q.Params))
	}
}

func checkColumns(l *check.List, at, query, what string, cols []Column) {
	seen := map[string]bool{}
	for _, c := range cols {
		if !check.Snake(c.Name) {
			l.Addf(at, "%s: %s name must be snake_case, got %q", query, what, c.Name)
		}
		if seen[c.Name] {
			l.Addf(at, "%s: %s %q is declared twice", query, what, c.Name)
		}
		seen[c.Name] = true
		if !Types[strings.TrimPrefix(c.Type, "?")] {
			l.Addf(at, "%s: %s %s has unknown type %q", query, what, c.Name, c.Type)
		}
	}
}

// scan counts ? placeholders and ;-terminated statements outside string
// literals, quoted identifiers and comments, and rejects other placeholder
// styles.
func scan(sql string) (placeholders, statements int, problem string) {
	sawCode := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch {
		case ch == '\'' || ch == '"' || ch == '`':
			j := i + 1
			for ; j < len(sql); j++ {
				if sql[j] != ch {
					continue
				}
				if j+1 < len(sql) && sql[j+1] == ch {
					j++ // doubled quote is an escaped quote
					continue
				}
				break
			}
			if j >= len(sql) {
				return 0, 0, "unterminated quote"
			}
			i = j
			sawCode = true
		case ch == '-' && strings.HasPrefix(sql[i:], "--"):
			if nl := strings.IndexByte(sql[i:], '\n'); nl >= 0 {
				i += nl
			} else {
				i = len(sql)
			}
		case ch == '/' && strings.HasPrefix(sql[i:], "/*"):
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				return 0, 0, "unterminated /* comment"
			}
			i += end + 3
		case ch == '?':
			if i+1 < len(sql) && sql[i+1] >= '0' && sql[i+1] <= '9' {
				return 0, 0, "numbered placeholders (?1) are not allowed; use bare ?"
			}
			placeholders++
			sawCode = true
		case (ch == ':' || ch == '@' || ch == '$') && i+1 < len(sql) && isIdentStart(sql[i+1]):
			return 0, 0, fmt.Sprintf("named placeholders (%c%s) are not allowed; use bare ?", ch, ident(sql[i+1:]))
		case ch == ';':
			if sawCode {
				statements++
			}
			sawCode = false
		case ch > ' ':
			sawCode = true
		}
	}
	if sawCode {
		statements++
	}
	return placeholders, statements, ""
}

func isIdentStart(c byte) bool { return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z') }

func ident(s string) string {
	n := 0
	for n < len(s) && (isIdentStart(s[n]) || (s[n] >= '0' && s[n] <= '9')) {
		n++
	}
	return s[:n]
}

// LoadDir parses every *.sql file in dir and checks that query names are
// unique across them.
func LoadDir(dir string) ([]Query, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var all []Query
	var l check.List
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		qs, err := Parse(f, string(src))
		if err != nil {
			l.Addf("", "%v", err)
		}
		all = append(all, qs...)
	}
	if err := Unique(all); err != nil {
		l.Addf("", "%v", err)
	}
	return all, l.Err()
}

// Unique reports query names declared more than once.
func Unique(qs []Query) error {
	var l check.List
	first := map[string]Query{}
	for _, q := range qs {
		if q.Name == "" {
			continue
		}
		if f, dup := first[q.Name]; dup {
			l.Addf(fmt.Sprintf("%s:%d", q.File, q.Line), "query %q is already declared at %s:%d", q.Name, f.File, f.Line)
			continue
		}
		first[q.Name] = q
	}
	return l.Err()
}
