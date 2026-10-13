# cmd/control

Composition root for the control plane: wires ports to their adapters from
`operator.json` and serves the HTTP JSON API and event stream. The control
center UI lands later (P3) behind the same binary.

See [docs/2-architecture.md § Control plane](../../docs/2-architecture.md#control-plane).
