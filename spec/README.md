# spec

The control plane's own specification. The founder owns this directory as
code owner. `go run ./cmd/generate` turns it into `gen/`.

| File | Holds | Format |
| --- | --- | --- |
| `project.json` | The plugins this repo's generated code uses (`domain`, `http` with base path `/api/v1`, `sqlite`) | `core/project` |
| `domain/control.json` | The control plane's read model (routing, tasks, run records, audit entries) and the pure functions behind the routes | `plugins/domain/spec` |
| `routes.json` | The `/api/v1` routes | `plugins/http/spec` |
| `queries/*.sql` | The statements on the control plane's database, checked against `migrations/` | `plugins/sqlite/spec` |
| `routing.json` | Which model fills each tier and which tier takes each job; the control plane serves it read-only | `control/routing` |

See [docs/2-architecture.md § Routing](../docs/2-architecture.md#routing)
and [docs/2-architecture.md § Specs and generators](../docs/2-architecture.md#specs-and-generators).
