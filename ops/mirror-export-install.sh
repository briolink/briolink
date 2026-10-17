#!/usr/bin/env bash
# Installs the repo mirror and nightly issue export (briolink-mirror.timer,
# briolink-export.timer), or removes them with `remove` (the data stays).
# Run with sudo; safe to rerun. Needs a read-only token in
# /etc/briolink/mirror-token (root, 0600), outside the repo; see ops/RUNBOOK.md.
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "run with sudo" >&2; exit 1; }
HERE=$(cd "$(dirname "$0")" && pwd)

if [ "${1:-}" = remove ]; then
  for u in mirror export; do
    systemctl disable --now briolink-$u.timer 2>/dev/null || true
    rm -f /etc/systemd/system/briolink-$u.service /etc/systemd/system/briolink-$u.timer
  done
  rm -f /usr/local/sbin/briolink-mirror-export.sh
  systemctl daemon-reload
  echo "removed (/var/lib/briolink-mirror and the token are left in place)"
  exit 0
fi

[ -s /etc/briolink/mirror-token ] || { echo "put a read-only token in /etc/briolink/mirror-token (root, 0600)" >&2; exit 1; }
chmod 0600 /etc/briolink/mirror-token

install -m 0755 "$HERE/mirror-export.sh" /usr/local/sbin/briolink-mirror-export.sh
install -d -m 0750 /var/lib/briolink-mirror
for u in mirror export; do
  install -m 0644 "$HERE/briolink-$u.service" "$HERE/briolink-$u.timer" /etc/systemd/system/
done
systemctl daemon-reload
systemctl enable --now briolink-mirror.timer briolink-export.timer

systemctl start briolink-mirror.service briolink-export.service
journalctl -u briolink-mirror.service -u briolink-export.service -n 20 --no-pager
