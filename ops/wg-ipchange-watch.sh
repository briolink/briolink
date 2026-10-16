#!/usr/bin/env bash
# Logs the private network's recovery from a hub address change (#49 step 3):
# one tab-separated line every INTERVAL seconds (default 2), flushed so a cut
# connection loses nothing. Run as root, on the spoke and on the hub, in
# parallel, before the change, as a transient unit (a backgrounded sudo stops on
# its password prompt, and sudo -E is refused on this host):
#   sudo systemd-run --unit=ipwatch --property=StandardOutput=append:$HOME/ipchange.log ops/wg-ipchange-watch.sh 2
# On the hub add --setenv=PUBIP=1 to log its public IPv4. Stop: sudo systemctl stop ipwatch.
#   utc  handshake-age-per-peer(s, "-" if none)  endpoint-per-peer  name->ip  [public ip]
# Recovery = the first line whose handshake age drops back below ~10 s after
# the outage; the time from the new address appearing to that line is the bound.
set -u
IFACE=${IFACE:-wg0}
NAME=${NAME:-wg.briolink.ca}
INTERVAL=${1:-2}

while :; do
  now=$(date +%s)
  ages=$(wg show "$IFACE" latest-handshakes 2>/dev/null | awk -v n="$now" '{print ($2==0)?"-":n-$2}' | paste -sd, -)
  eps=$(wg show "$IFACE" endpoints 2>/dev/null | awk '{print $2}' | paste -sd, -)
  dns=$(getent hosts "$NAME" 2>/dev/null | awk '{print $1}')
  pub=""
  [ "${PUBIP:-0}" = 1 ] && pub=$(curl -fsS --max-time 2 https://api.ipify.org 2>/dev/null || echo down)
  printf '%s\t%s\t%s\t%s\t%s\n' "$(date -u +%FT%TZ)" "${ages:-?}" "${eps:-?}" "${dns:-none}" "$pub"
  sleep "$INTERVAL"
done
