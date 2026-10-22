# control

The control plane's own code, wired by [cmd/control](../cmd/control/README.md).
The API's types, handlers and queries are generated from [spec/](../spec/README.md)
into `gen/`; what is here is the rest.

| Package | Does |
| --- | --- |
| `operator` | `operator.json` v0: listen address (private, never a wildcard; TLS unless loopback), database, routing file, the master key's credential name, adapter per port |
| `routing` | `spec/routing.json` v0: strict decoder and validation of the tier and job table |
| `logic` | The pure functions of `spec/domain/control.json` (`control.Funcs`): sorting the routing view, reading a task from an issue, clamping a page size, the credential-expiry alerts |
| `store` | The control plane's SQLite database: run records and the append-only audit log (the database refuses an update or delete); applies `migrations/` at open; publishes an event after each write |
| `events` | The in-process event broker: numbered events, the last 256 kept for reconnecting clients, a subscriber that falls 64 behind is cut off |
| `service` | The generated `api.Service`, hand-written during the bootstrap (M9): the routes read, except the credential routes, which write through the vault as the token that called |
| `vault` | The secret store and API tokens (A20), in the control plane's database. Secrets: sealed with AES-256-GCM under the master key, bound to their name; put, rotate, delete and use, never listed or displayed. Tokens: random, only their SHA-256 kept, each acting as one role; revoked or expired ones are refused. Both may carry an expiry and a reminder date (`Expiring` lists those due) |
| `server` | `/api/v1` (the generated handler) and `/api/v1/events` (Server-Sent Events, the one hand-written route, because streaming does not fit the generated shape), bearer-token auth against the vault, `Cache-Control: no-store` |

## Secrets and tokens

- **Master key:** 64 hexadecimal digits (`openssl rand -hex 32`) in a root-only
  systemd credential named by `master_key_credential`
  (`LoadCredentialEncrypted=` in the unit), also kept in the founder's password
  manager. The control plane will not start if the file is readable by other
  users or is not a key, nor if the key fails to open any stored secret
  (`vault: 1 of the stored secrets do not open (NAME)`, names only), and
  `control secret put|rotate` refuse the same way, so nothing is sealed under
  a key the rest were not. Deleting, revoking and listing need no key, so a
  lost key does not lock the operator out. Changing the master key (re-sealing
  every secret) is not built.
- **Tokens** are created and revoked with `control token ...`
  ([cmd/control](../cmd/control/README.md)) or the routes below; the token is
  printed (or answered) once and cannot be recovered. A token acts as one role, `operator` or `admin`; a route
  of the other role answers it with 403. `operator` reads (everything under
  API v0 below, and lists credentials); `admin` only changes credentials and
  reaches no other route, so the runner's token (an `operator`) cannot mint
  tokens or touch secrets. Only the founder holds an `admin` token.
- **Audit:** every secret put, rotate, delete and use, every token creation and
  revocation, every use of a token (`GET /path`, the query left out) and every
  refusal of a known token (revoked, expired) is a line, named by the token's
  name. A token nobody issued leaves no line, so a stranger cannot grow the
  log. If the line cannot be written, the use is refused. Nothing logs or
  returns a secret, a token or the key.
- **Expiry:** `control credentials -expiring` lists what the control center
  will list: reminder date reached, or expiry within 30 days (or past).
  `control credentials -check` is the daily credential-expiry check
  ([3-hosting](../docs/3-hosting.md#checks)): one line per credential that is
  `expired` (expiry before today, so a token is already refused) or `remind`
  (reminder date reached or, without one, expiry within 30 days), with its
  name and expiry date, never a value; revoked ones and ones with no expiry
  are left out. The rule is `credential_alerts` in `logic`; the ops host runs
  the command daily and sends its lines to the notifier.

## API v0 (roles `operator` and `admin`)

Reads, all `GET`, role `operator`:

| Path | Returns |
| --- | --- |
| `/api/v1/routing` | The routing table, sorted (read-only: the founder changes it by pull request, M13) |
| `/api/v1/tasks?state=&limit=` | The open issues of the code host as tasks, ascending number; `state` is a TaskState (`queued`, `running`, `needs_founder`, `needs_reasoner`, `blocked`, `waiting`, `unlabelled` or `conflict`), any other value selects none |
| `/api/v1/tasks/{number}` | One task, open or closed; 404 when the issue does not exist |
| `/api/v1/runs?task=&limit=` | Run records, newest first |
| `/api/v1/audit?limit=` | Audit entries, newest first |
| `/api/v1/events` | `text/event-stream`: `audit` and `run` events whose data is the JSON the endpoints above serve for a record; new events only, or those after the `Last-Event-ID` header (the last 256 are kept); writes `: ping` when idle |

Credentials: the same operations as `control secret|token|credentials`, as
routes (`POST` and `PUT` take a JSON body). A secret or token is named by 1 to
100 letters, digits, `_`, `.` or `-`, starting with a letter or digit (the
routes reach no other name; the commands accept any printable name). Dates are
`YYYY-MM-DD`. Listing is role `operator`; every route that changes a secret or a
token is role `admin`.

| Route | Does | Answers |
| --- | --- | --- |
| `GET /api/v1/secrets?expiring=` | Lists the secrets, by name | `200`, `{secrets: [...]}`: kind, name, dates, never a value |
| `POST /api/v1/secrets` | Stores a new secret: `name`, `value` (1 to 65536 characters), optional `expires_on`, `remind_on` | `204`; `409` if the name exists |
| `PUT /api/v1/secrets/{name}` | Replaces the value and the dates: `value`, optional dates; **a date left out is cleared**, not kept | `204`; `404` if there is none |
| `DELETE /api/v1/secrets/{name}` | Deletes it | `204`; `404` if there is none |
| `GET /api/v1/tokens?expiring=` | Lists the tokens, revoked ones marked | `200`, `{tokens: [...]}`: kind, name, role, dates, `revoked`, never a token |
| `POST /api/v1/tokens` | Issues a token: `name`, `role`, optional dates | `201`, `{token}`: the only answer that holds it; `409` if the name exists; `422` for an unknown role |
| `POST /api/v1/tokens/{name}/revoke` | Revokes it | `204`; `404` if there is none |

`expiring=true` lists only what `control credentials -expiring` lists. A
refused date, reminder or name is a `422`; an answer, an error or an audit
line never repeats a secret's value. Each change is audited under the name of
the token that made it (`token:<name>`). A token of another role is refused
with a `403`, as on every route.

`limit` defaults to 50 and is at most 200. A task's state is the one `state:`
label of its issue; none is `unlabelled`, more than one is `conflict`.

## Not yet

- **Limits on who may issue what**: an `admin` token may issue a token of any
  role (itself an `admin`) and revoke any token, itself included.
- **Master key rotation**: not built.
- **Other writes**: only the credential routes write through the API. The runner
  ([#60](https://github.com/briolink/briolink/issues/60)) will add run
  records through `store.AddRun`; the audit log is written by `store.AddAudit`.
- **Real adapters**: the code host can be bound to `github` (`cmd/control`); the coder and the reasoner only to `fake`.

See [docs/2-architecture.md § Control plane](../docs/2-architecture.md#control-plane).
