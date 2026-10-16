#!/usr/bin/env bash
# Installs the hub's wg0 firewall and turns on IP forwarding for good, or
# removes the firewall with `remove`. Run with sudo; safe to rerun. It does
# not touch the WireGuard hub itself.
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "run with sudo" >&2; exit 1; }
HERE=$(cd "$(dirname "$0")" && pwd)

if [ "${1:-}" = remove ]; then
  systemctl disable --now wg-hub-fw.service 2>/dev/null || true
  nft delete table inet briolink_hub 2>/dev/null || true
  iptables -D DOCKER-USER -i wg0 -o wg0 -j ACCEPT 2>/dev/null || true
  rm -f /etc/systemd/system/wg-hub-fw.service /etc/briolink/wg-hub.nft
  systemctl daemon-reload
  echo "removed (IP forwarding left on)"
  exit 0
fi

nft -c -f "$HERE/wg-hub.nft"

install -d -m 0755 /etc/briolink
install -m 0644 "$HERE/wg-hub.nft" /etc/briolink/wg-hub.nft
install -m 0644 "$HERE/wg-hub-fw.service" /etc/systemd/system/wg-hub-fw.service
echo 'net.ipv4.ip_forward = 1' > /etc/sysctl.d/90-briolink-forward.conf
sysctl -q --system
systemctl daemon-reload
systemctl enable wg-hub-fw.service
systemctl restart wg-hub-fw.service

sysctl net.ipv4.ip_forward
nft list table inet briolink_hub
iptables -S DOCKER-USER 2>/dev/null || true
