# generate

The generator host: runs the project's selected plugins' generators, in
dependency order, over the spec files into `gen/`. The host itself carries
no generator; each spec kind's generator ships with the plugin that owns
that kind, and `cmd/*` registers them.

- `Generator`: the one interface every plugin implements. It gets the
  project root, the project, its own plugin ref and manifest, and the spec
  kind; it returns files keyed by name, relative to its plugin's directory
  in `gen/`, and writes nothing.
- `Registry`: `"<plugin>/<kind>" → Generator` (`Key`). A spec kind without
  one fails with the plugin and kind named.
- `Order`: dependency order from the manifests' `requires`, then project
  order. A cycle, a missing manifest or an unpicked requirement fails with
  the names (via `project.Resolve`).
- `Run`: generates every file in memory, under `gen/<plugin>/`. Every file
  needs `DO NOT EDIT` in its first three lines, a clean relative path, and
  a path no other generator produced.
- `Diff` / `Check`: the regenerate-and-diff gate the runner uses. `Check`
  generates in memory (no scratch tree on disk is needed) and fails
  listing the paths in `gen/` that differ, are missing, or were not
  generated.
- `Write`: replaces `gen/` with the generated files.

`testdata/acme` is the pilot fixture: `plugins/sqlite/gen.Host` generates
its `gen/` through the host, and the test fails on any difference. After a
deliberate generator change, `go test ./generate -update` rewrites it.

See [docs/2-architecture.md § Specs and generators](../docs/2-architecture.md#specs-and-generators).
