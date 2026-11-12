# cmd/runner

Composition root for the [runner](../../runner/README.md): reads `operator.json`
and the routing table, binds the coder, the reasoner and the code host it
names (the same adapters, built the same way, as [cmd/control](../control/README.md):
`cmd/internal/wire`), opens the control plane's database for the run records
and its secret store for the code host's key, loads one task contract from the
project's `tasks/` and runs it for an issue, every command in the offline build
worker (`docker run --network none`).

```text
runner/container/build-tools.sh <tools-dir>
runner/container/seed-modcache.sh <modcache-dir>
go build -o bin/runner ./cmd/runner
bin/runner [-config operator.json] [-repo .] -task <id> -issue <n>
           [-tools <tools-dir>] [-modcache <modcache-dir>] [-cache <dir>]
           [-image briolink-build-worker] [-base <ref>] [-tasks <dir>]
           [-remote origin] [-git-name <name>] [-git-email <email>] [-merge-into main]
```

It reads the master key from `$CREDENTIALS_DIRECTORY/<master_key_credential>`
like the control plane (root-only file, same refusals), and refuses a database
whose secrets do not open under it.

- `-task` is a contract's id: `<tasks>/<id>.json`, `<repo>/tasks` by default.
  `-issue` is the key of the run records.
- `adapters.coder` is `fake` or `ollama`; `ollama` runs the model the routing
  table gives the `pure_function` job, on `127.0.0.1:11434`.
  `adapters.reasoner` is `fake` or `claudecode`: escalation (step 6) asks it at
  the tiers of the `escalation.first` and `escalation.second` rows.
  `adapters.codehost` is `fake` or `github`: the bot (A22) reads its App key
  from the secret store (`github.key_secret`).
- A pass is pushed to `-remote` as `task/<issue>-<task id>` under the identity
  `-git-name`/`-git-email` (git's own config when empty: set them to the bot's),
  then opened as a pull request into `-merge-into` that closes the issue; the
  attempt summary goes on the issue. A task nobody passes ends with
  `state:needs-founder` and the summary, with the link to its runs in the
  control plane (`<listen>/api/v1/runs?task=<issue>`).
- `-image` is built by `seed-modcache.sh` from
  [runner/container/Containerfile](../../runner/container/Containerfile)
  (base pinned by digest). `-tools` holds the linux `generate` and `mutate`
  the checks call; `-modcache` is read-only, `-cache` the build cache.
- Each attempt is a run record in `operator.json`'s `database`, listed by the
  control plane's `/api/v1`.
- Exit status 1 when the task does not pass; the attempts are printed.

Not wired here: a project's tier override
([#107](https://github.com/briolink/briolink/issues/107)). The worktree is
removed when the run ends.

`go test ./cmd/runner` runs the whole command on a fake coder, reasoner and code
host and a fake container engine against a real temporary git repository, bare
remote and database: a pass lands on the remote and in a pull request, a forced
failure ends in the reasoner and then the founder. Nothing there has run against
a live model, GitHub or the real image; the live run is described in
[#111](https://github.com/briolink/briolink/issues/111).
