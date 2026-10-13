# port

One small interface per role the core depends on (`ObjectStore`, `CodeHost`,
`Coder`, `Reasoner`, …), each with contract tests an adapter must pass
before it can be bound. Core code imports only these interfaces, never a
provider package.

See [docs/2-architecture.md § Roles and adapters](../docs/2-architecture.md#roles-and-adapters).
