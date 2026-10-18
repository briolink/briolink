# control

The control plane's own code, wired by [cmd/control](../cmd/control/README.md).
The API's types, handlers and queries are generated from [spec/](../spec/README.md)
into `gen/`; what is here is the rest.

| Package | Does |
| --- | --- |
| `operator` | `operator.json` v0: listen address (private, never a wildcard; TLS unless loopback), database, routing file, token variable, adapter per port |
| `routing` | `spec/routing.json` v0: strict decoder and validation of the tier and job table |
| `logic` | The pure functions of `spec/domain/control.json` (`control.Funcs`): sorting the routing view, reading a task from an issue, clamping a page size |
| `store` | The control plane's SQLite database: run records and the append-only audit log (the database refuses an update or delete); applies `migrations/` at open; publishes an event after each write |
| `events` | The in-process event broker: numbered events, the last 256 kept for reconnecting clients, a subscriber that falls 64 behind is cut off |
| `service` | The generated `api.Service`, hand-written during the bootstrap (M9): every method reads |
| `server` | `/api/v1` (the generated handler) and `/api/v1/events` (Server-Sent Events, the one hand-written route, because streaming does not fit the generated shape), bearer-token auth, `Cache-Control: no-store` |

## API v0 (all `GET`, role `operator`)

| Path | Returns |
| --- | --- |
| `/api/v1/routing` | The routing table, sorted (read-only: the founder changes it by pull request, M13) |
| `/api/v1/tasks?state=&limit=` | The open issues of the code host as tasks, ascending number; `state` is a TaskState (`queued`, `running`, `needs_founder`, `needs_reasoner`, `blocked`, `waiting`, `unlabelled` or `conflict`), any other value selects none |
| `/api/v1/tasks/{number}` | One task, open or closed; 404 when the issue does not exist |
| `/api/v1/runs?task=&limit=` | Run records, newest first |
| `/api/v1/audit?limit=` | Audit entries, newest first |
| `/api/v1/events` | `text/event-stream`: `audit` and `run` events whose data is the JSON the endpoints above serve for a record; new events only, or those after the `Last-Event-ID` header (the last 256 are kept); writes `: ping` when idle |

`limit` defaults to 50 and is at most 200. A task's state is the one `state:`
label of its issue; none is `unlabelled`, more than one is `conflict`.

## Not yet

- **Authentication** is one bearer token, read from the environment variable
  `operator.json` names, until the secret store and API tokens land
  ([#58](https://github.com/briolink/briolink/issues/58)). No token configured
  means nobody is let in, and the control plane refuses to start.
- **Writes**: nothing writes through the API yet. The runner
  ([#60](https://github.com/briolink/briolink/issues/60)) will add run
  records through `store.AddRun`; the audit log is written by `store.AddAudit`.
- **Real adapters**: only `fake` can be bound to a port today (`cmd/control`).

See [docs/2-architecture.md § Control plane](../docs/2-architecture.md#control-plane).
