#!/usr/bin/env bash
# Installs the userspace WireGuard hub as a systemd service on the tower and
# puts it back on UDP 51820. Run with sudo, once; safe to rerun.
# Needs /etc/wireguard/wg0.conf already written (hub keys, peers).
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "run with sudo" >&2; exit 1; }
HERE=$(cd "$(dirname "$0")" && pwd)

apt-get install -y wireguard wireguard-go >/dev/null

# Stop any hand-started hub (wireguard-go or kernel) and clear the interface.
systemctl stop wg-hub 2>/dev/null || true
ip link delete dev wg0 2>/dev/null || true
pkill -x wireguard-go 2>/dev/null || true

sed -i 's/^ListenPort = .*/ListenPort = 51820/' /etc/wireguard/wg0.conf

install -m 0644 "$HERE/wg-hub.service" /etc/systemd/system/wg-hub.service
systemctl daemon-reload
systemctl enable --now wg-hub

sleep 2
systemctl is-active wg-hub
wg show wg0 | grep -E 'listening port|peer|handshake'
