**The proposed changes, tested.** All of it was prototyped by hand on scratch copies of `generate/testdata/acme` and `quality/mutate` (nothing in the repo changed), on `main` at 12926be.

**What was built:**

- **Mutation check:** statement deletion (an assignment, a call or an increment is dropped), 17 lines in `funcMutants`. The checker's own tests still pass.
- **`acmetest` (what the generator would emit):** `InvoiceCase.Want`, compared with the step's entity in place of `tr.Apply`; `InvoiceInvariants`; `Violate(name)` in `InvoiceSamples`, and a `violate/<name>` test that asserts the invariant is false and every other one holds (so a sample breaks one invariant at a time). 133 → 163 lines.
- **Samples (the reasoner's part):** a `Want` for each of the 9 fire cases; Fire 3 at a 1-cent balance; Fire 4 and Block 4 with 400 paid; 2 violation cases per invariant; examples for `new_invoice` (1) and `total` (3). 104 → 131 lines, plus 34 for the examples.

The new tests pass on the reference and still fail on the stubs (`stubscheck`).

**Mutation check, `mutate ./reference`:**

| Tests | Operator mutants only | With deletions |
| --- | --- | --- |
| Today | 8 of 55 survive | 13 of 61 survive (all 5 action deletions survive, `ApplyPayment` included) |
| Proposed | — | **0 of 61 survive** (1 invalid, 12 s) |

**Faults no mutator makes**, each tried alone through `go test -overlay`:

| Fault | Today | Proposed |
| --- | --- | --- |
| `add_line`, `issue`, `void` do nothing; `new_invoice`, `total` return zero (together) | missed | caught |
| `add_line` prepends | missed | caught |
| `add_line` replaces the lines | missed | caught |
| `add_line` writes into the caller's slice | missed | caught |
| `issue` sets `issued_at` only | missed | caught |
| `issue` also changes the customer | missed | caught |
| `apply_payment` sets paid to the amount | missed | caught |
| `apply_payment` pays the whole balance | missed | caught |
| `void` uses a fixed reason | missed | caught |
| `void` also clears paid | missed | missed |
| `total` ignores the quantity | missed | caught |
| `new_invoice` drops the currency | missed | caught |
| `settles_balance` ignores paid | caught | caught |

The one miss is equivalent on every reachable state: `void` only fires with nothing paid (`unpaid` guards it in `issued`, and a draft can't be paid), so clearing paid changes nothing.

**What it costs:** about 30 more generated lines per entity, about 25% more samples plus the expected entities, and 6 more mutants (61 instead of 55).

**Limits:** one entity (`Invoice`) and faults I made by hand, which is no random sample. The generator template, the `stubscheck` expectation in the `domain` plugin's test and the methodology text are not written yet.
