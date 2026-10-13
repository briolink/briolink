# generate

The generator host: runs the project's selected plugins' generators, in
dependency order, over the spec files into `gen/`. The host itself carries
no generator; each spec kind's generator ships with the plugin that owns
that kind.

See [docs/2-architecture.md § Specs and generators](../docs/2-architecture.md#specs-and-generators).
