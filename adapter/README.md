# adapter

Provider implementations of the ports, one per `adapter/<role>/<name>/`.
Only the composition root (`cmd/*`) imports an adapter package, wired from
the installation's operator config; secrets are kept apart. A deep binding
lists its relied-on features, fallback and exit in
[docs/4-choices.md](../docs/4-choices.md#bindings-in-detail).

See [docs/2-architecture.md § Roles and adapters](../docs/2-architecture.md#roles-and-adapters).
