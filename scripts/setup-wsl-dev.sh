#!/usr/bin/env bash
# Installs the Briolink development tools in WSL (Ubuntu): Go, Docker + Compose,
# Podman, buildx/QEMU (to build and run arm64 images), sqlite3, rclone, WireGuard
# tools (the tower is the WireGuard hub), Litestream.
# Safe to re-run. Review before running; it uses sudo.
#
#   bash ~/projects/briolink/scripts/setup-wsl-dev.sh
#
# Not installed here: Garage (runs as a container in compose.yaml) and wrangler
# (used through npx inside the project).

set -euo pipefail

log() { printf '\n==> %s\n' "$*"; }

if [ "$(ps -p 1 -o comm=)" != "systemd" ]; then
  echo "systemd is not running in this WSL distro (Docker needs it)."
  echo "Add to /etc/wsl.conf:  [boot]  systemd=true   then run 'wsl --shutdown' from Windows."
  exit 1
fi

case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)"; exit 1 ;;
esac

log "Ubuntu packages"
sudo apt-get update
sudo apt-get install -y \
  ca-certificates curl jq build-essential \
  sqlite3 rclone wireguard-tools \
  docker.io docker-compose-v2 docker-buildx \
  podman \
  qemu-user qemu-user-binfmt

log "Docker service and group"
sudo systemctl enable --now docker
if ! id -nG "$USER" | grep -qw docker; then
  sudo usermod -aG docker "$USER"
  echo "Added $USER to the docker group (takes effect in a new shell)."
fi

log "Go (latest stable, checksum verified)"
GO_VERSION="$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -n1)"
GO_FILE="${GO_VERSION}.linux-${ARCH}.tar.gz"
if [ -x /usr/local/go/bin/go ] && [ "$(/usr/local/go/bin/go env GOVERSION)" = "$GO_VERSION" ]; then
  echo "Go $GO_VERSION already installed."
else
  GO_SHA="$(curl -fsSL 'https://go.dev/dl/?mode=json' \
    | jq -r --arg f "$GO_FILE" '.[].files[] | select(.filename == $f) | .sha256' | head -n1)"
  [ -n "$GO_SHA" ] || { echo "No checksum found for $GO_FILE"; exit 1; }
  TMP="$(mktemp -d)"
  curl -fsSL -o "$TMP/$GO_FILE" "https://go.dev/dl/$GO_FILE"
  echo "$GO_SHA  $TMP/$GO_FILE" | sha256sum -c -
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf "$TMP/$GO_FILE"
  rm -rf "$TMP"
fi
if ! grep -q '/usr/local/go/bin' "$HOME/.profile"; then
  printf '\nexport PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"\n' >> "$HOME/.profile"
  echo "Added Go to PATH in ~/.profile (open a new shell)."
fi

log "Litestream (latest release, checksum verified, installed to ~/.local/bin)"
case "$ARCH" in amd64) LS_ARCH=x86_64 ;; arm64) LS_ARCH=arm64 ;; esac
LS_TAG="$(curl -fsSL https://api.github.com/repos/benbjohnson/litestream/releases/latest | jq -r .tag_name)"
LS_FILE="litestream-${LS_TAG#v}-linux-${LS_ARCH}.tar.gz"
if command -v litestream >/dev/null && [ "$(litestream version 2>/dev/null)" = "${LS_TAG#v}" ]; then
  echo "Litestream $LS_TAG already installed."
else
  TMP="$(mktemp -d)"
  curl -fsSL -o "$TMP/$LS_FILE" "https://github.com/benbjohnson/litestream/releases/download/$LS_TAG/$LS_FILE"
  curl -fsSL -o "$TMP/checksums.txt" "https://github.com/benbjohnson/litestream/releases/download/$LS_TAG/checksums.txt"
  (cd "$TMP" && grep " $LS_FILE\$" checksums.txt | sha256sum -c -)
  tar -C "$TMP" -xzf "$TMP/$LS_FILE"
  mkdir -p "$HOME/.local/bin"
  install -m 755 "$TMP/litestream" "$HOME/.local/bin/litestream"
  rm -rf "$TMP"
fi

log "Versions"
/usr/local/go/bin/go version
docker --version
docker compose version
docker buildx version
podman --version
sqlite3 --version
rclone version | head -n1
command -v litestream >/dev/null && litestream version || true

echo
echo "Done. Open a new shell (or run 'newgrp docker') before using docker without sudo."
