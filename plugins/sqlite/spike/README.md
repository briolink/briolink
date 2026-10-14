# plugins/sqlite/spike

The measurement for [issue #13](https://github.com/briolink/briolink/issues/13):
does preparing every `spec/queries` statement against the migrated schema
catch a wrong table, column, parameter count and type, with the declared
result columns checked against the database's?

A separate module (its own `go.mod`), so the root module stays standard
library only until the generator lands; `go test ./...` at the root skips it.
`check.go` is the shape the generator's check takes; `probe_test.go` is the
measurement, one mistake per case. Run with network once to fetch
`modernc.org/sqlite`: `cd plugins/sqlite/spike && go test -v ./...`.

## Answer

Measured 2026-10-14 with `modernc.org/sqlite` v1.60.1 (SQLite 3.53.4, built with
`SQLITE_ENABLE_COLUMN_METADATA`), Go 1.27.2, on `schema.sql` (the pilot's
tables as `STRICT` tables, plus one non-`STRICT` table).

**Yes for the schema, the parameter count and the result columns; no for
parameter types.** SQLite has no parameter types to check against.

| Mistake | Caught at generation? | How |
| --- | --- | --- |
| Wrong table | Yes | prepare: `no such table` |
| Wrong column: select list, `WHERE`, `ORDER BY`, subquery, `UPDATE SET`, `INSERT` list | Yes | prepare: `no such column` / `has no column named` |
| `INSERT` value count, ambiguous column, unknown function | Yes | prepare |
| Misspelt column in double quotes (`WHERE "customr" = ?`) | Only with `_dqs=0` | SQLite's default reads it as the string `'customr'` and passes |
| Parameter count | Yes | `EXPLAIN` with the declared number of `NULL`s: too few declared fails (`missing argument with index n`), too many shows as a smaller highest `Variable` number. `database/sql` doesn't check it: the driver's `NumInput` is -1 |
| Parameter type (`WHERE paid = ?` with a `string`) | **No** | SQLite infers no parameter types; the comparison just never matches |
| Parameter type stored in a column | At run time only | a `STRICT` table rejects `'x'` in an `INTEGER` column (`cannot store TEXT value`), but converts `'3'`; a non-`STRICT` table stores anything |
| Result column count, names, order | Yes | `ColumnInfo` (prepare only, never executes) |
| Result column type | Yes, for a direct column reference | the column's declared type against the spec type (table below) |
| Result column type of an expression (`count(*)`, `CAST`) | No | no declared type; reported as unchecked |
| `NOT NULL` declared for a nullable column | Yes, for a direct column reference | origin table and column, then `pragma_table_xinfo` |
| `NOT NULL` column made nullable by a `LEFT JOIN` | **No** | the origin column is `NOT NULL`; the join isn't visible |
| `RETURNING` on an `:exec` | Yes | the database returns columns |
| `NOT NULL` column left out of an `INSERT` | At run time only | constraint failure on execution |
| Non-`STRICT` table | Yes, as a schema rule | `pragma_table_list.strict` |

Spec type to declared column type (`STRICT` tables allow only `INT`,
`INTEGER`, `REAL`, `TEXT`, `BLOB`, `ANY`):

| Spec type | Column |
| --- | --- |
| `string` | `TEXT` |
| `int` | `INTEGER` or `INT` |
| `float` | `REAL` |
| `bool` | `INTEGER` or `INT` (0 or 1) |
| `time` | `TEXT` (RFC 3339, UTC) |
| `bytes` | `BLOB` |

Cost: the four pilot queries check in about 0.5 ms on an in-memory database
(about 0.14 ms each), so checking every query on every generation is free.

## What else it showed

- **Size of the driver.** `go mod vendor` of `modernc.org/sqlite` and its
  dependencies is 137 MB (`sqlite` 57 MB, `libc` 71 MB: every OS and
  architecture), 1,908 Go files. A6 says admitted libraries are vendored;
  [#4](https://github.com/briolink/briolink/issues/4) left the choice open
  between vendoring and a pre-seeded read-only module cache. Open decision
  [#41](https://github.com/briolink/briolink/issues/41) (O12).
- **Arguments at run time.** The driver rejects too few arguments but
  silently ignores extra ones, so the generated functions' fixed signatures
  are what keep calls right.

## Not verified

- Views, `WITH` (CTE) queries, `UPSERT` and window functions weren't probed.
- Only `linux/amd64`.
- The column-metadata build flag is checked for this version only; the
  generator should fail loudly if `ColumnInfo` returns no origin for a plain
  column reference.
