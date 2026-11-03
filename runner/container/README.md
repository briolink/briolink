# runner/container

The build worker image for [issue #4](https://github.com/briolink/briolink/issues/4):
one offline container per task, no network, running `gofmt`, `go vet`,
`go build` and `go test` against a git worktree mounted at `/workspace`.

## Answer

`go build` and `go test` run fully offline in a container built from
`Containerfile`, measured with Docker (`--network none`) against this
repo's worktree (2026-10-14, repo at commit `7923024`):

| Step | Time |
| --- | --- |
| Container start (`docker run --rm --network none … true`) | ~1.4s |
| Cold `gofmt -l . && go vet ./... && go build ./... && go test ./...`, no cache mount | ~10s |
| Same, with `GOCACHE`/`GOMODCACHE` mounted from the host, first pass | ~8.5s |
| Same, second pass (build cache hit) | ~0.5s |

`go mod download` cannot run inside the per-task container (no network), so
a dependency's fetch always happens outside it, once, with network, not per
task — either `go mod vendor`, committed, or a pre-seeded module cache
mounted read-only at `-v <host-gomodcache>:/go/pkg/mod` above. A6 picks
between them per dependency: vendor by default; a pre-seeded cache only for
one whose vendored tree would bloat every clone, and only in this repo and
its platform forks — a client project repo still vendors it. `modernc.org/sqlite`
(137 MB vendored, [#13](https://github.com/briolink/briolink/issues/13),
[#41](https://github.com/briolink/briolink/issues/41)) takes the cache path
here; its `go.sum` entry still pins the exact version the cache must hold.

**Seeding, measured (2026-10-14):** `seed-modcache.sh <dest>` runs `go mod
download` inside this same pinned image, with network, writing to a
host-mounted `<dest>`; that is the only step that touches the network, and
it runs on the ops host, not per task. For this repo's one dependency the
seeded cache is 298 MB (`go mod download`'s zip cache plus the extracted
source for every module in the build list — both are required: a cache
trimmed to only the extracted trees failed with `GOPROXY=off`, because Go
verifies the extracted source against the zip cache's hashes before
trusting it). Mounting that cache read-only and building with `GOPROXY=off`
(stronger than `--network none`: no proxy address is even consulted) —
`go build ./...`, `go vet ./...` and `go test ./...` all passed against the
`sqlite-generator` branch, using the Go 1.27.2 toolchain the image pins, no
network reachable. The per-task mount above is unaffected: the same
`-v <host-gomodcache>:/go/pkg/mod` already in the Recipe is where the
seeded cache goes.

## What breaks without network

The only network-shaped failure is the Go toolchain switch: if the pinned
image's Go version doesn't match `go.mod`'s `toolchain` line, the default
`GOTOOLCHAIN=auto` tries to fetch the matching toolchain from
`proxy.golang.org` and fails on DNS/network-unreachable — a real failure,
but one whose speed and clarity depend on how the sandbox's resolver fails.
Setting `GOTOOLCHAIN=local` (in `Containerfile`) removes that dependency:
a mismatch fails immediately and explicitly instead —
`go: go.mod requires go >= 1.27.0 (running go 1.26.9; GOTOOLCHAIN=local)` —
measured against `golang:1.26-bookworm` while this repo's `go.mod` pins
`toolchain go1.27.2`. Keeping the Containerfile's base digest in step with
`go.mod`'s `toolchain` line avoids the mismatch outright; `GOTOOLCHAIN=local`
is the fallback so a drift is loud rather than a network timeout.

No other step reached the network in testing: `gofmt`, `go vet`, `go build`
and `go test` are local-only once the toolchain matches.

## Recipe

`Containerfile` pins the base image by digest (E1) and sets
`GOTOOLCHAIN=local`. The runner (step 1, [2-architecture.md § Runner](../../docs/2-architecture.md#runner))
builds this image once, then per task:

```sh
docker run --rm --network none \
  -v <task-worktree>:/workspace -w /workspace \
  -v <host-gocache>:/root/.cache/go-build \
  -v <host-gomodcache>:/go/pkg/mod \
  <image> sh -c "gofmt -l . && go vet ./... && go build ./... && go test ./..."
```

The `go-build` cache mount is optional (cold runs pass without it, ~10s) but
cuts a warm task to well under a second. The `gomodcache` mount was
optional while the repo had no external module; now that `modernc.org/sqlite`
is a dependency (not vendored, per A6's exception above), it's required for
any task that builds or tests against it — run `seed-modcache.sh <dest>`
once (whenever `go.sum` changes) and pass that `<dest>` as `<host-gomodcache>`.

`build-tools.sh <dest>` builds the static linux `generate` and `mutate` the
checks call; pass `<dest>` as the runner's `Container.Tools`
(mounted at `/briolink/bin`, first on `PATH`). [cmd/runner](../../cmd/runner/README.md)
takes the three directories as flags.
