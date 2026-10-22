#!/bin/sh
# Seeds a Go module cache from this repo's go.sum (A6, #41): the admitted
# exception to vendoring, for a dependency whose vendored tree would bloat
# every clone (modernc.org/sqlite, #13). Run this once, with network,
# whenever go.sum changes, using the same pinned image the per-task
# container runs (so the cache matches its Go version); mount its output
# read-only at /go/pkg/mod in the no-network per-task container (README
# § Recipe). The per-task container itself never reaches the network.
set -eu
cd "$(dirname "$0")"
dest="${1:?usage: seed-modcache.sh <dest-dir>}"
image=briolink-build-worker
docker build -q -f Containerfile -t "$image" .
mkdir -p "$dest"
docker run --rm \
  -v "$(cd ../.. && pwd)":/workspace -w /workspace \
  -v "$dest":/go/pkg/mod \
  "$image" go mod download -x
echo "seeded $dest"
