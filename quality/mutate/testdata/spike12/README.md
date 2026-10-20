# Spike #12: prototypes

The prototypes behind [#12](https://github.com/briolink/briolink/issues/12), kept for [#85](https://github.com/briolink/briolink/issues/85) and [#86](https://github.com/briolink/briolink/issues/86) to start from. This branch is never merged as is.

- `quality/mutate/mutants.go`: statement deletion (the `*ast.BlockStmt` case), for #85.
- `generate/testdata/acme/gen/domain/acme/acmetest/derived.go`: what the `domain` generator would emit (`Want`, `InvoiceInvariants`, `Violate`), edited by hand, so regenerate-and-diff fails on this branch. For #86.
- `generate/testdata/acme/samples/` (`samples.go`, `pure.go`) and `reference/derived_test.go`: the samples, expected entities, violation cases and pure-function examples, for #86.

Here:

- `result.md`, `test.md`: the two measurements, as posted on #12.
- `faults.py`: the hand-made faults tried through `go test -overlay`. Run it with a folder holding two copies of `generate/testdata/acme`: `base/` from `main` and `new/` from this branch (`python3 -I faults.py <folder>`).
- `noop.go.txt`: the reference with every action doing nothing (the first fault in #12).

The mutation counts: on `generate/testdata/acme`, build `cmd/mutate` from this branch and run `mutate ./reference`. `main`'s tests leave 13 of 61 mutants alive; this branch's leave 0.
