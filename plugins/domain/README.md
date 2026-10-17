# plugins/domain

Bounded contexts and statecharts: the `domain` spec kind's generator, which
turns `spec/domain/<context>.json` into Go types, transition tables,
dispatchers, pure-function stubs and the tests derived from each statechart.

- `spec/`: format v0 for `spec/domain/<context>.json`: decoding, `Validate`
  and the statechart checks.
- `gen/`: the generator `plugin.json` points `spec_kinds[].generator` at.
  `Host` loads the files and calls `Generate`, which fails with every
  problem listed or returns the files.
- `plugin.json`: the manifest.

## What a context generates

For context `acme`, under `gen/domain/acme/` (package name: the context
without underscores):

| File | Holds |
| --- | --- |
| `types.go` | Enums, values, entities as Go types (`int` is `int64`, `?T` is `*T`) |
| `funcs.go` | `Funcs`: one method per guard, action, invariant and pure function, with the spec's rules as comments |
| `errors.go` | `ErrForbidden`, `ErrNoTransition`, `InvariantError` |
| `<entity>_machine.go` | States, events, the transition table (`Holds`/`Apply` closures call `Funcs`), the forbidden pairs, `Select` (which transition fires) and `Step` (select, apply the action, check the invariants) |
| `acmetest/derived.go` | The derived tests, `acmetest.Invoice(t, funcs, samples)` |
| `stubs/` | `Impl`, one file per function, each panicking `not implemented: <name>` |

## Derived tests

For each entity with a statechart, against any `Funcs` implementation:

- every transition fires, selects its own row, reaches its `To` state, returns
  its action's result and leaves the invariants true;
- every guard is false at the boundaries given by `Block`, and the
  transition does not fire;
- every forbidden pair (including every event in a final state) returns
  `ErrForbidden` and changes nothing.

The boundary values are not in the spec, whose rules are prose, so a
hand-written `Samples` value supplies them (the interface is generated, with
the transitions numbered in its comment). `Fire` and `Block` return several
cases, one per boundary side, and must return at least one.

What the derived tests cannot check is an action's exact effect on the
entity: the spec states it as a rule, and the tests that pin rules down are
written separately (`kind:test`). Derived tests catch an action that breaks
an invariant, a wrong guard, a wrong table and a wrong dispatcher.

## Filling the stubs

`stubs/` is generated, so it is never edited in place. Copy its files to the
project's own package (`domain/<context>/`), rename the package, drop the
`DO NOT EDIT` line and fill the bodies. Then a test there runs the derived
tests on the filled `Impl`:

```go
func TestInvoice(t *testing.T) { acmetest.Invoice(t, Impl{}, samples.Invoice{}) }
```

## The acme fixture

`generate/testdata/acme` is its own Go module (`example.com/acme`) so the
generated code can be built and run: `reference/` is a hand-written `Funcs`
the derived tests pass on, `stubscheck/` runs them on the stubs, `samples/`
holds the boundary values. `plugins/domain/gen/generate_test.go` runs both, and runs the derived tests
against deliberately wrong references to show they fail.

See [docs/2-architecture.md § Specs and generators](../../docs/2-architecture.md#specs-and-generators).
