# port

One small interface per role the core depends on (`ObjectStore`, `CodeHost`,
`Coder`, `Reasoner`, …), each with contract tests an adapter must pass
before it can be bound. Core code imports only these interfaces, never a
provider package.

See [docs/2-architecture.md § Roles and adapters](../docs/2-architecture.md#roles-and-adapters).

## v0 ports

| Package | Role | Fake |
| --- | --- | --- |
| `port/objectstore` | S3 subset: put with metadata, conditional get, head, delete, list, presigned get and put | `NewFake()` |
| `port/codehost` | Issues, labels, milestones, branches, pull requests, comments (no merge: only the founder merges, A22) | `NewFake()` |
| `port/coder` | Request parts in, files out | `&Fake{Answer: …}` |
| `port/reasoner` | Tier per call, structured output, cost and session reported | `&Fake{Tiers: …}` |

Each package exports `Contract(t, factory, …)`, the suite an adapter's own
test calls before it can be bound; the package's `fake_test.go` runs the
same suite against the fake, and the runner and control-plane tests use the
fakes. `imports_test.go` fails if any port imports anything but the standard
library and other ports.
