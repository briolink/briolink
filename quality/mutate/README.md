# quality/mutate

The mutation checker (M5): mutates operators, boundaries, constants and statements in a
pure function, reruns the package's tests, and reports every surviving mutant
with its position. Built on `go/ast`, standard library only (A6). The command
is [cmd/mutate](../../cmd/mutate/main.go):

```text
mutate ./domain/acme -func settles_balance     # one function
mutate ./domain/acme                           # every function in the package
```

Exit status: `0` every mutant caught, `1` survivors (each printed as
`SURVIVED file:line:col func: original -> replacement`), `2` the check could
not run (tests failing before any mutation, unknown function, no `go` tool).

## Mutants

| Site | Becomes |
| --- | --- |
| `+` `-` `*` `/` `%` | `-` `+` `/` `*` `*` |
| `==` `!=` | `!=` `==` |
| `<` `<=` `>` `>=` | the other side of the boundary, and the negation |
| `&&` `\|\|` | `\|\|` `&&` |
| `+=` `-=` `*=` `/=`, `++` `--` | the opposite |
| `!x`, `-x` | `x` |
| `true`, `false` | the other |
| integer `n` | `n+1` and `n-1` |
| string `"s"`; `""` | `""`; `"mutant"` |
| an assignment (not `:=`), a call statement, `x++`, `x--` | the statement removed, reported `statement -> deleted` |

A deletion that leaves a variable unused does not compile and is invalid. It
is what gives an action with no operator (an assignment or call with nothing
to flip) a mutant at all.

Not mutated: shifts, bit operators, raw strings, floats, other statements
(`return`, `:=`, `go`, `defer`, `break`), no `return` changes.

## How a mutant is tried

The package's tests must pass unmutated, or the run stops with exit `2`. Each
mutant is printed to a temporary file and laid over the real one with
`go test -overlay`, so the working tree is never written. Several mutants run
at once (`-workers`).

- **Killed:** the tests failed.
- **Timed out:** the tests did not finish (`-timeout`, default 5x the
  unmutated run, at least 10 s), a loop the mutant made endless. Counted as
  killed.
- **Survived:** every test passed. A missing test, which goes back to the
  reasoner.
- **Invalid:** the mutant does not compile (`"a" - "b"`). Reported, counted
  neither way.

There is no way to mark a mutant equivalent: a survivor is fixed by a test or
by changing the function.

## Compared with Gremlins

On the `testdata` fixtures (`strong`, `weak`), run with Gremlins' default
mutators: every mutant Gremlins kills is killed here, and every one that lives
there lives here. This checker also tries mutants Gremlins leaves out by
default (`&&`/`||`, constants, strings, `true`/`false`, unary operators), which
is where `weak` shows two more survivors (`0 -> 1`, `0 -> -1`). Not yet
compared on the P0 acceptance set: its functions don't exist until the
`domain` generator does.
