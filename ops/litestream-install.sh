#!/usr/bin/env bash
# Installs the shared Litestream daemon on the host (#114): the pinned release, the `backup`
# user, /etc/litestream, and litestream.service. Run with sudo, once; safe to rerun. The daemon
# starts when the first client is added with ops/litestream-client.sh (it refuses an empty config).
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "run with sudo" >&2; exit 1; }
HERE=$(cd "$(dirname "$0")" && pwd)
VERSION=0.5.17

case "$(dpkg --print-architecture)" in
	arm64) ARCH=arm64 ;;
	amd64) ARCH=x86_64 ;;
	*) echo "unsupported architecture" >&2; exit 1 ;;
esac
DEB=litestream-$VERSION-linux-$ARCH.deb
BASE=https://github.com/benbjohnson/litestream/releases/download/v$VERSION

if ! litestream version 2>/dev/null | grep -q "$VERSION"; then
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	curl -fsSL -o "$tmp/$DEB" "$BASE/$DEB"
	curl -fsSL -o "$tmp/checksums.txt" "$BASE/checksums.txt"
	(cd "$tmp" && grep " $DEB\$" checksums.txt | sha256sum -c -)
	dpkg -i "$tmp/$DEB"
fi
# The package ships its own unit; ours replaces it.
systemctl disable --now litestream 2>/dev/null || true

id backup >/dev/null 2>&1 || useradd --system --home-dir /var/lib/litestream --shell /usr/sbin/nologin backup
apt-get install -y acl >/dev/null

install -d -m 0755 /etc/litestream /etc/litestream/clients
install -d -m 0750 -o backup -g backup /var/lib/litestream
[ -e /etc/litestream/env ] || install -m 0600 -o root -g root /dev/null /etc/litestream/env

install -m 0644 "$HERE/litestream.service" /etc/systemd/system/litestream.service
systemctl daemon-reload
systemctl enable litestream
litestream version
echo "installed; add a client with: sudo ops/litestream-client.sh <client> <entry.yml> <keys.env>"
