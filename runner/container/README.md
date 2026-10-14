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

The repo has no external module (A6, stdlib only: no `go.sum`), so there is
nothing to vendor today — `go mod vendor` reports "no dependencies to
vendor". The open part of the question is what a dependency would need:
`go mod download` cannot run inside the container (no network), so a future
dependency has to be vendored into the repo (`go mod vendor`, committed) or
its module cache pre-seeded on the ops host and mounted read-only into the
container; either way the fetch happens outside the no-network container,
once, with network, not per task.

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

The two cache mounts are optional (cold runs pass without them, ~10s) but
cut a warm task to well under a second; they are the same path a vendored
or pre-seeded module cache would use once a dependency exists.
