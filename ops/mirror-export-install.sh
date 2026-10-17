#!/usr/bin/env bash
# Installs the repo mirror and nightly issue export (briolink-mirror.timer,
# briolink-export.timer), or removes them with `remove` (the data stays).
# Run with sudo; safe to rerun. The jobs read MIRROR_GH_TOKEN (read-only) from
# the repo-root .env (M15, permanent section); the line stays while they run.
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "run with sudo" >&2; exit 1; }
HERE=$(cd "$(dirname "$0")" && pwd)
ENV_FILE=$(cd "$HERE/.." && pwd)/.env

if [ "${1:-}" = remove ]; then
  for u in mirror export; do
    systemctl disable --now briolink-$u.timer 2>/dev/null || true
    rm -f /etc/systemd/system/briolink-$u.service /etc/systemd/system/briolink-$u.timer
  done
  rm -f /usr/local/sbin/briolink-mirror-export.sh
  systemctl daemon-reload
  echo "removed (/var/lib/briolink-mirror is left in place)"
  exit 0
fi

grep -q '^MIRROR_GH_TOKEN=.' "$ENV_FILE" || { echo "MIRROR_GH_TOKEN must be set in $ENV_FILE" >&2; exit 1; }

install -m 0755 "$HERE/mirror-export.sh" /usr/local/sbin/briolink-mirror-export.sh
install -d -m 0750 /var/lib/briolink-mirror
for u in mirror export; do
  sed "s|@ENV_FILE@|$ENV_FILE|" "$HERE/briolink-$u.service" > /etc/systemd/system/briolink-$u.service
  chmod 0644 /etc/systemd/system/briolink-$u.service
  install -m 0644 "$HERE/briolink-$u.timer" /etc/systemd/system/
done
systemctl daemon-reload
systemctl enable --now briolink-mirror.timer briolink-export.timer

systemctl start briolink-mirror.service briolink-export.service
journalctl -u briolink-mirror.service -u briolink-export.service -n 20 --no-pager
