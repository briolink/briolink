#!/usr/bin/env bash
# Installs the hub's DDNS job (wg-ddns.timer), or removes it with `remove`.
# Run with sudo; safe to rerun. The job reads CF_API_TOKEN from the
# repo-root .env (M15, permanent section); the line stays while the job runs.
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "run with sudo" >&2; exit 1; }
HERE=$(cd "$(dirname "$0")" && pwd)
ENV_FILE=$(cd "$HERE/.." && pwd)/.env

if [ "${1:-}" = remove ]; then
  systemctl disable --now wg-ddns.timer 2>/dev/null || true
  rm -f /etc/systemd/system/wg-ddns.service /etc/systemd/system/wg-ddns.timer /usr/local/sbin/wg-ddns.sh
  systemctl daemon-reload
  echo "removed (the DNS record wg.briolink.ca is left in place)"
  exit 0
fi

grep -q '^CF_API_TOKEN=.' "$ENV_FILE" || { echo "CF_API_TOKEN must be set in $ENV_FILE" >&2; exit 1; }

install -m 0755 "$HERE/wg-ddns.sh" /usr/local/sbin/wg-ddns.sh
sed "s|@ENV_FILE@|$ENV_FILE|" "$HERE/wg-ddns.service" > /etc/systemd/system/wg-ddns.service
chmod 0644 /etc/systemd/system/wg-ddns.service
install -m 0644 "$HERE/wg-ddns.timer" /etc/systemd/system/wg-ddns.timer
systemctl daemon-reload
systemctl enable --now wg-ddns.timer

systemctl start wg-ddns.service
journalctl -u wg-ddns.service -n 5 --no-pager
