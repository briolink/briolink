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
  "token_env": "BRIOLINK_API_TOKEN",
  "adapters": { "codehost": "fake", "coder": "fake", "reasoner": "fake" } }
```

- `listen` is an IP address and port. A wildcard address is refused (nothing
  listens publicly, A20); `tls` may be `null` only on a loopback address.
- It refuses to start when the token variable is empty, an adapter name is
  unknown, or any file is missing. On start and on a clean stop it appends an
  audit line (the start line names the adapters bound).
- On `SIGINT`/`SIGTERM` it ends the open event streams, finishes the requests
  in flight (10 s at most) and exits.

See [docs/2-architecture.md § Control plane](../../docs/2-architecture.md#control-plane).
