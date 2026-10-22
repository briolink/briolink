# cmd/control

Composition root for the control plane: reads `operator.json`, wires each port
to the adapter it names, opens the database, and serves the HTTP JSON API and
event stream ([control/](../../control/README.md)). The control center UI
lands later (P3) behind the same binary.

```text
go build -o bin/control ./cmd/control
bin/control [-config operator.json] [-version]
bin/control [-config operator.json] <command>
```

`operator.json` v0 (decoded strictly; see `control/operator`):

```json
{ "format": "v0", "listen": "10.77.0.1:8443",
  "tls": { "cert_file": "/etc/briolink/ops.pem", "key_file": "/etc/briolink/ops.key" },
  "database": "/var/lib/briolink/control.db",
  "routing_file": "/srv/briolink/spec/routing.json",
  "master_key_credential": "briolink-master-key",
  "adapters": { "codehost": "github", "coder": "fake", "reasoner": "fake" },
  "github": { "repo": "briolink/briolink", "app_id": 5307371,
    "installation_id": 171151281, "key_secret": "BRIOLINK_BOT_KEY" } }
```

- `adapters.codehost` is `fake` or `github`. `github` acts as the bot (A22) on
  `github.repo`; it reads the App's private key from the secret store
  (`key_secret`, audited as actor `codehost`) on each hourly token mint. The
  section may stay while another adapter is bound.

- `listen` is an IP address and port. A wildcard address is refused (nothing
  listens publicly, A20); `tls` may be `null` only on a loopback address.
- It refuses to start when the master key is missing, readable by other users or
  not 64 hexadecimal digits, a stored secret does not open under it, an adapter
  name is unknown, or any file is missing.
  The key is read from `$CREDENTIALS_DIRECTORY/<master_key_credential>`, which
  systemd sets for a unit with `LoadCredentialEncrypted=` (A20). On start and on a clean stop it appends an
  audit line (the start line names the adapters bound).
- On `SIGINT`/`SIGTERM` it ends the open event streams, finishes the requests
  in flight (10 s at most) and exits.

## Commands

Run on the host, with the same `-config` and credentials as the service; they
write to the same database and to the audit log (as actor `cli`). Dates are
`YYYY-MM-DD`. Values are read from standard input, never from an argument, so
they stay out of the shell history and the process list; one trailing newline
is dropped.

| Command | Does |
| --- | --- |
| `secret put NAME [-expires D] [-remind D]` | Seals a new secret |
| `secret rotate NAME [-expires D] [-remind D]` | Replaces the value and the dates (a date left out is cleared) |
| `secret delete NAME` | Deletes it |
| `token create NAME [-role operator\|admin] [-expires D] [-remind D]` | Prints a new API token, once |
| `token revoke NAME` | Stops the token authenticating |
| `credentials [-expiring]` | Lists secrets and tokens with their dates, never values |
| `credentials -check` | The daily credential-expiry check: one line per credential that is `remind` or `expired`, with its name and expiry date; `nothing to report` when none. Run it daily from the ops host's timer |

```text
systemd-creds ... # master key, once
echo "$VALUE" | bin/control -config operator.json secret put MIRROR_GH_TOKEN -expires 2027-10-17 -remind 2027-09-17
bin/control -config operator.json token create ci -expires 2027-10-17
```

## Live code host test

`TestGitHubLive` runs the CodeHost contract against GitHub as the bot, with
the key from an installation's secret store, one private throwaway repository
per case (made with the founder's token: the bot can't create repositories),
and checks that every issue, pull request, comment, milestone and branch
commit in each was written by `briolink-bot[bot]`. It is skipped unless both
variables are set. With the dev setup (`ops/dev-control.sh`):

```text
ops/dev-control.sh
CREDENTIALS_DIRECTORY=~/.local/share/briolink-dev/creds \
BRIOLINK_LIVE_GITHUB=~/.local/share/briolink-dev/operator.json \
BRIOLINK_LIVE_FOUNDER_TOKEN=$(gh auth token) \
go test -run TestGitHubLive -v -count=1 ./cmd/control
```

The repositories (`codehost-contract-<stamp>-NN`) are logged for deletion by
hand; with `BRIOLINK_LIVE_DELETE=1` and a token with `delete_repo`
(`gh auth refresh -s delete_repo`) the test deletes them.

See [docs/2-architecture.md § Control plane](../../docs/2-architecture.md#control-plane).
