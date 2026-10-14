# plugins/sqlite

SQLite access for a client app (`modernc.org/sqlite`, pure Go, E2): the
`queries` spec kind's generator, which turns `spec/queries/*.sql` into typed
Go functions.

- `spec/`: format v0 for `spec/queries/*.sql` — parses and validates the
  header comments (name, cardinality, typed params and columns).
- `gen/`: the generator plugin.json points `spec_kinds[].generator` at.
  `Generate` applies a project's migrations to an in-memory database,
  checks every query against that schema without running it, and emits
  `queries.go` plus a `queries_check_test.go` that repeats the check
  (docs/2-architecture.md § Specs and generators). It fails closed: any
  problem and nothing is written.
- `spike/`: the measurement that answered [#13](https://github.com/briolink/briolink/issues/13)
  (what the check can and can't catch); kept for the record, not built on.
- `plugin.json`: the real manifest. No migrations of its own yet; the
  migration runner (core) concatenates every plugin's `migrations/` in
  dependency order, and that schema is `gen.Input.Schema`.

See [docs/2-architecture.md § Specs and generators](../../docs/2-architecture.md#specs-and-generators)
and [plugins/sqlite/spike/README.md](spike/README.md) for what the check
does and doesn't catch.
