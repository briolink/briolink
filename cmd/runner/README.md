# cmd/runner

Composition root for the [runner](../../runner/README.md): reads `operator.json`
and the routing table, binds the coder it names, opens the control plane's
database for the run records, loads one task contract from the project's
`tasks/` and runs it for an issue, every command in the offline build worker
(`docker run --network none`).

```text
runner/container/build-tools.sh <tools-dir>
runner/container/seed-modcache.sh <modcache-dir>
go build -o bin/runner ./cmd/runner
bin/runner [-config operator.json] [-repo .] -task <id> -issue <n>
           [-tools <tools-dir>] [-modcache <modcache-dir>] [-cache <dir>]
           [-image briolink-build-worker] [-base <ref>] [-tasks <dir>]
```

- `-task` is a contract's id: `<tasks>/<id>.json`, `<repo>/tasks` by default.
  `-issue` is the key of the run records.
- `adapters.coder` is `fake` or `ollama`, the same names as
  [cmd/control](../control/README.md); `ollama` runs the model the routing
  table gives the `pure_function` job, on `127.0.0.1:11434`.
- `-image` is built by `seed-modcache.sh` from
  [runner/container/Containerfile](../../runner/container/Containerfile)
  (base pinned by digest). `-tools` holds the linux `generate` and `mutate`
  the checks call; `-modcache` is read-only, `-cache` the build cache.
- Each attempt is a run record in `operator.json`'s `database`, listed by the
  control plane's `/api/v1`.
- Exit status 1 when the task does not pass; the attempts are printed.

Not wired here: the reasoner, the code host (no pull request, no issue
comment: [#61](https://github.com/briolink/briolink/issues/61)'s steps run
only when the `Runner` has a `Reasoner` and a `Host`) and a project's tier
override ([#107](https://github.com/briolink/briolink/issues/107)). A pass is
reported, and the worktree removed.

`go test ./cmd/runner` runs the whole command on a fake coder and a fake
container engine against a real temporary git repository and database.
