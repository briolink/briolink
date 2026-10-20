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
  "adapters": { "codehost": "fake", "coder": "fake", "reasoner": "fake" } }
```

- `listen` is an IP address and port. A wildcard address is refused (nothing
  listens publicly, A20); `tls` may be `null` only on a loopback address.
- It refuses to start when the master key is missing, readable by other users or
  not 64 hexadecimal digits, an adapter name is unknown, or any file is missing.
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
| `secret rotate NAME [-expires D] [-remind D]` | Replaces the value and the dates |
| `secret delete NAME` | Deletes it |
| `token create NAME [-role operator] [-expires D] [-remind D]` | Prints a new API token, once |
| `token revoke NAME` | Stops the token authenticating |
| `credentials [-expiring]` | Lists secrets and tokens with their dates, never values |

```text
systemd-creds ... # master key, once
echo "$VALUE" | bin/control -config operator.json secret put MIRROR_GH_TOKEN -expires 2027-10-17 -remind 2027-09-17
bin/control -config operator.json token create ci -expires 2027-10-17
```

See [docs/2-architecture.md § Control plane](../../docs/2-architecture.md#control-plane).
