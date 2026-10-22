#!/bin/sh
# Builds the generate and mutate binaries the checks run in the build worker
# (Container.Tools, mounted at /briolink/bin). Static linux binaries, so they
# run in the pinned image whatever the host is. Run it again when the
# platform's code changes.
set -eu
dest="${1:?usage: build-tools.sh <dest-dir>}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
mkdir -p "$dest"
dest="$(cd "$dest" && pwd)"
cd "$root"
for tool in generate mutate; do
  CGO_ENABLED=0 GOOS=linux go build -trimpath -o "$dest/$tool" "./cmd/$tool"
done
echo "built $dest"
