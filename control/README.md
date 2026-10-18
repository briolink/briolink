# control

The control plane's own code, wired by [cmd/control](../cmd/control/README.md).
The API's types, handlers and queries are generated from [spec/](../spec/README.md)
into `gen/`; what is here is the rest.

| Package | Does |
| --- | --- |
| `operator` | `operator.json` v0: listen address (private, never a wildcard; TLS unless loopback), database, routing file, the master key's credential name, adapter per port |
| `routing` | `spec/routing.json` v0: strict decoder and validation of the tier and job table |
| `logic` | The pure functions of `spec/domain/control.json` (`control.Funcs`): sorting the routing view, reading a task from an issue, clamping a page size |
| `store` | The control plane's SQLite database: run records and the append-only audit log (the database refuses an update or delete); applies `migrations/` at open; publishes an event after each write |
| `events` | The in-process event broker: numbered events, the last 256 kept for reconnecting clients, a subscriber that falls 64 behind is cut off |
| `service` | The generated `api.Service`, hand-written during the bootstrap (M9): every method reads |
| `vault` | The secret store and API tokens (A20), in the control plane's database. Secrets: sealed with AES-256-GCM under the master key, bound to their name; put, rotate, delete and use, never listed or displayed. Tokens: random, only their SHA-256 kept, each acting as one role; revoked or expired ones are refused. Both may carry an expiry and a reminder date (`Expiring` lists those due) |
| `server` | `/api/v1` (the generated handler) and `/api/v1/events` (Server-Sent Events, the one hand-written route, because streaming does not fit the generated shape), bearer-token auth against the vault, `Cache-Control: no-store` |

## Secrets and tokens

- **Master key:** 64 hexadecimal digits (`openssl rand -hex 32`) in a root-only
  systemd credential named by `master_key_credential`
  (`LoadCredentialEncrypted=` in the unit), also kept in the founder's password
  manager. The control plane will not start if the file is readable by other
  users or is not a key. A wrong key starts fine and fails per secret
  (`vault: the master key does not open this secret`); changing the master key
  (re-sealing every secret) is not built.
- **Tokens** are created and revoked with `control token ...`
  ([cmd/control](../cmd/control/README.md)); the token is printed once and
  cannot be recovered. A token acts as one role (`operator` today); the routes
  of another role answer a token of the wrong role with 403.
- **Audit:** every secret put, rotate, delete and use, every token creation and
  revocation, every use of a token (`GET /path`, the query left out) and every
  refusal of a known token (revoked, expired) is a line, named by the token's
  name. A token nobody issued leaves no line, so a stranger cannot grow the
  log. If the line cannot be written, the use is refused. Nothing logs or
  returns a secret, a token or the key.
- **Expiry:** `control credentials -expiring` lists what the daily
  credential-expiry check and the control center will list: reminder date
  reached, or expiry within 30 days (or past).

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

- **API for the vault**: secrets and tokens are managed from the host with the
  `control` commands; nothing in `/api/v1` lists or changes them yet. Listing
  the expiring credentials over the API comes with the credential-expiry check
  and the control center (P3).
- **Master key rotation**: not built.
- **Writes**: nothing writes through the API yet. The runner
  ([#60](https://github.com/briolink/briolink/issues/60)) will add run
  records through `store.AddRun`; the audit log is written by `store.AddAudit`.
- **Real adapters**: only `fake` can be bound to a port today (`cmd/control`).

See [docs/2-architecture.md § Control plane](../docs/2-architecture.md#control-plane).
