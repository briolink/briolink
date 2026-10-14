# runner/acceptance

The P0 acceptance set: the project the runner must complete unattended
before P0 exits ([roadmap](../../docs/5-roadmap.md#stages)). Its run records
also feed the tier spike ([#16](https://github.com/briolink/briolink/issues/16)).

`acme/` is a self-contained project root, the way a client repo would be:
`spec/` holds its model (one `Booking` entity with a flat statechart, its
guards, actions and invariants, four pure functions, seven routes) and
`tasks/` its contracts. Its own `go.mod` keeps the code the coder writes
there out of the platform module. Its rules are examples, not a client's.

`acceptance_test.go` checks the set: every file decodes and validates,
routes resolve against the model, every `spec_ref` resolves, and every
function has one test task and one logic task that depends on it.

## Expected outcomes

Predictions, written before any run, for the spike to compare against.

| Tasks | Kind | Executor | Expected |
| --- | --- | --- | --- |
| 0001–0004 | test | reasoner (`routine`) | Pass; boundary tests from the rules |
| 0006–0010, 0012, 0015–0018 | logic | coder | First or second attempt: one comparison or one field set |
| 0005 `deposit_covers` | logic | coder | Possible escalation: rounding up a percentage |
| 0011 `quote` | logic | coder | Likely escalation: started blocks from a duration |
| 0013 `refund_due` | logic, medium | coder | Likely escalation: tiered time thresholds, inclusive bounds, rounding down |
| 0014 `overstay_charge` | logic, medium | coder | Likely escalation: a grace inside the first block, the rate shared with `quote` |

Tasks behind a route (0011, 0012, 0015–0018) also run the generated handler
tests; tasks behind a transition also run its generated transition tests.

## Open

- Storage: the handlers need load and save; the `sqlite` plugin and its
  queries join once the generator spike ([#13](https://github.com/briolink/briolink/issues/13)) settles them.
- The `mutate` argv follows the format fixture; `quality/mutate` fixes its
  real flags when it is built.
