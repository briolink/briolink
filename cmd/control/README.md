# cmd/control

Composition root for the control plane: reads `operator.json`, wires each port
to the adapter it names, opens the database, and serves the HTTP JSON API and
event stream ([control/](../../control/README.md)). The control center UI
lands later (P3) behind the same binary.

```text
go build -o bin/control ./cmd/control
bin/control [-config operator.json] [-version]
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

- `master_key_credential` is the name of a systemd credential
  (`LoadCredential=` in the unit): a file in `$CREDENTIALS_DIRECTORY` holding
  the secret store's master key as 64 hexadecimal characters, readable by its
  owner only. The founder's password manager holds a copy.
- `listen` is an IP address and port. A wildcard address is refused (nothing
  listens publicly, A20); `tls` may be `null` only on a loopback address.
- It refuses to start when the master key is missing, readable by others or
  does not open every sealed secret, an adapter name is unknown, or any file is
  missing. With no active API token it says so and starts: nobody gets in until
  one is issued. On start and on a clean stop it appends an
  audit line (the start line names the adapters bound).
- Callers authenticate with `Authorization: Bearer <token>`; see below.
- On `SIGINT`/`SIGTERM` it ends the open event streams, finishes the requests
  in flight (10 s at most) and exits.

## Secrets and tokens

Local commands on the ops host, on the same database (the API has no write
route yet). Each is audited as `local:<user>`; none prints a secret or a token
again once stored.

```text
control [-config operator.json] secret put NAME [-expires YYYY-MM-DD] [-remind YYYY-MM-DD]     # value on stdin
control [-config operator.json] secret rotate NAME [-expires …] [-remind …]                    # value on stdin
control [-config operator.json] secret delete NAME
control [-config operator.json] secret list                  # name, status, expiry, reminder
control [-config operator.json] token issue NAME [-scope operator] [-expires …] [-remind …]  # prints the token once
control [-config operator.json] token revoke NAME
control [-config operator.json] token list
```

A secret's value comes from stdin so it stays out of the shell history and the
process list: `control secret put MIRROR_GH_TOKEN -expires 2027-10-17 -remind
2027-09-17 < file`. `rotate` replaces the value and the dates. A token is
`blt_` and 43 characters; a revoked or expired one is refused with a 401.

See [docs/2-architecture.md § Control plane](../../docs/2-architecture.md#control-plane).
