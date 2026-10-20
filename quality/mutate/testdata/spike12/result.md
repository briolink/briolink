**Result: no.** On the `acme` acceptance set the derived tests and the samples kill 47 of 55 mutants; 8 survive. A fault the mutator can't make goes through as well: actions that do nothing pass every derived test.

**How it was measured:** `mutate ./reference` from `generate/testdata/acme` (the reference implementation, its only tests being `acmetest.Invoice` with `samples.Invoice`), on `main` at 12926be.

```text
SURVIVED reference/reference.go:33:18 SettlesBalance: 0 -> 1
SURVIVED reference/reference.go:37:52 PartialPayment: - -> +
SURVIVED reference/reference.go:64:27 PaidWithinTotal: && -> ||
SURVIVED reference/reference.go:64:25 PaidWithinTotal: 0 -> (-1)
SURVIVED reference/reference.go:69:21 LinesValid: || -> &&
SURVIVED reference/reference.go:69:19 LinesValid: 1 -> 0
SURVIVED reference/reference.go:69:38 LinesValid: 0 -> (-1)
SURVIVED reference/reference.go:70:11 LinesValid: false -> true
55 mutants: 47 killed (0 timed out), 8 survived, 0 invalid
```

Then by hand, through `go test -overlay`: `AddLine`, `Issue` and `Void` each returning the invoice unchanged, `NewInvoice` returning a zero invoice and `Total` returning 0: **the derived tests still pass.**

**Why, in three gaps:**

1. **Samples (the reasoner's part), 2 survivors.** `settles_balance` is never fired at its lowest amount (1 cent on a 1-cent balance), and `partial_payment` is only sampled with nothing paid, so `total - paid` and `total + paid` look the same. With the cases added (Fire 3: balance 1, amount 1; Block 4: 400 paid, amount 600) both are killed: `mutate -func PartialPayment` gives 8 of 8. The rule they break: a boundary computed from several fields (balance = total - paid) needs each operand varied, on both sides.
2. **Invariants are only checked as true, 6 survivors.** The derived tests check that the invariants hold after each step but never that they fail, so any weakening survives, up to `LinesValid` always returning true.
3. **Actions are checked against themselves.** The fire test compares the step's entity with `tr.Apply(f, e, ev)`, the same implementation `InvoiceStep` calls, so it only proves determinism. An action is caught only when it breaks an invariant (`ApplyPayment: += -> -=` dies that way). The mutation check can't see this: `Issue`, `Void` and `AddLine` have no operator to flip, so they produce no mutants, and statements are never deleted.

`new_invoice` and `total` (kind `pure`) are outside the statechart and have no tests at all in the set. That is outside this question, but the do-nothing check shows it.

**Proposed changes, for the founder's decision** (1-methodology § Test-driven development, then the `domain` generator, the mutation checker and the acme fixture):

- **Fire cases carry the expected entity**, written by hand (`Want` in `InvoiceCase`); the test compares against it, not against `tr.Apply`. Each action's rules then have a test.
- **Invariants get violation samples:** for each invariant, entities just outside each boundary (`Violate(name)`), and the test asserts that it is false.
- **The reasoner's rule** in § Test-driven development: business values at each boundary, and for a computed boundary each operand varied on both sides; the expected entity for each fired case; examples for every `pure` function.
- **Mutation check:** add statement deletion (drop an assignment or an `append`) to `quality/mutate`, so an action with no operators still produces mutants.
- **Acme fixture:** add the samples above, the expected entities, the violation samples and tests for `new_invoice` and `total`; the spike passes when `mutate ./reference` reports 0 survivors, deletions included.

**Not verified:** how the proposed generator changes behave (none is built yet), and any entity other than `Invoice`.
