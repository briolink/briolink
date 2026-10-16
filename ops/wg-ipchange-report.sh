#!/usr/bin/env bash
# Unattended end of #49 step 3. Start it in the background on the tower before
# cutting the connection. It waits for the hub watcher's log to show the public
# IP going "down" and coming back, waits SETTLE seconds, fetches the spoke's
# log, computes the recovery times, and asks headless Claude (no tools, run
# outside the repo) to draft the write-up. It writes files under $OUT only: no
# commit, no push, no issue comment, no config change.
set -uo pipefail
export PATH=$HOME/.local/bin:$PATH
HUB_LOG=${HUB_LOG:-$HOME/ipchange-hub.log}
OUT=${OUT:-$HOME/ipchange-run}
SPOKE=${SPOKE:-ubuntu@10.88.0.10}
KEY=${KEY:-$HOME/.ssh/briolink_ops_ed25519}
SETTLE=${SETTLE:-720}
HERE=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$OUT"
log() { echo "$(date -u +%FT%TZ) $*" >> "$OUT/progress.log"; }

log "waiting for an outage in $HUB_LOG"
for _ in $(seq 1 720); do
  grep -qP '\tdown$' "$HUB_LOG" 2>/dev/null && break
  sleep 30
done
down_ts=$(awk -F'\t' '$5=="down"{print $1; exit}' "$HUB_LOG")
[ -n "$down_ts" ] || { log "no outage seen in 6 h, giving up"; exit 1; }
ip_before=$(awk -F'\t' '$5=="down"{exit} $5!=""{ip=$5} END{print ip}' "$HUB_LOG")
log "outage from $down_ts, public IP before: $ip_before"

restore_ts=""
for _ in $(seq 1 720); do
  restore_ts=$(awk -F'\t' -v d="$down_ts" '$1>d && $5!="down" && $5!=""{print $1; exit}' "$HUB_LOG")
  [ -n "$restore_ts" ] && break
  sleep 30
done
[ -n "$restore_ts" ] || { log "connection never came back in 6 h"; exit 1; }
log "connection back at $restore_ts; settling ${SETTLE}s"
sleep "$SETTLE"

ip_after=$(awk -F'\t' '$5!="" && $5!="down"{ip=$5} END{print ip}' "$HUB_LOG")
if [ -n "${SPOKE_LOG_FILE:-}" ]; then cp "$SPOKE_LOG_FILE" "$OUT/spoke.log"
else ssh -i "$KEY" -o ConnectTimeout=15 -o StrictHostKeyChecking=accept-new "$SPOKE" 'cat ~/ipchange-spoke.log' > "$OUT/spoke.log" 2>"$OUT/ssh.err" || log "could not fetch the spoke log: see ssh.err"; fi

epoch() { date -u -d "$1" +%s; }
first() { awk -F'\t' -v after="$2" "$1 && \$1>after {print \$1; exit}" "$3"; }
rec_hub=$(first "\$4==\"$ip_after\"" "$restore_ts" "$HUB_LOG")
rec_spoke=$(first "\$4==\"$ip_after\"" "$restore_ts" "$OUT/spoke.log")
hs_back=$(first '$2 ~ /^[0-9]+$/ && $2+0 < 10' "$restore_ts" "$OUT/spoke.log")
max_age=$(awk -F'\t' -v a="$down_ts" -v b="$restore_ts" '$1>=a && $1<=b && $2 ~ /^[0-9]+$/ && $2+0>m{m=$2+0} END{print m+0}' "$OUT/spoke.log")
delta() { [ -n "$2" ] && echo "$(( $(epoch "$2") - $(epoch "$1") )) s" || echo "not seen"; }
if [ "$ip_before" = "$ip_after" ]; then changed="NO: same IP, the test is void (outage only)"; else changed="yes"; fi

{
  echo "public IP changed: $changed ($ip_before -> $ip_after)"
  echo "outage start (hub log): $down_ts"
  echo "connection back (hub log): $restore_ts"
  echo "spoke's largest handshake age during the outage: ${max_age} s"
  echo "new address visible in DNS on the hub: $rec_hub ($(delta "$restore_ts" "$rec_hub") after restore)"
  echo "new address visible in DNS on the spoke: $rec_spoke ($(delta "$restore_ts" "$rec_spoke") after restore)"
  echo "spoke handshake fresh again (<10 s): $hs_back ($(delta "$restore_ts" "$hs_back") after restore)"
  echo "A21 bound to compare with: 4 minutes (240 s) from the address change"
} > "$OUT/summary.txt"
log "summary written"

[ -n "${NO_CLAUDE:-}" ] && exit 0
cd "$OUT" || exit 1
{
  echo "You are drafting notes for an infrastructure runbook. Below is the measured result of a real ISP address change on a WireGuard hub-and-spoke network (computed by a script from log files). Do not invent numbers."
  echo "Context: the spoke re-resolves the hub from a systemd timer every 2 minutes and only acts when the last handshake is over 135 s old; the hub's DNS record (TTL 60) is updated by a timer every 60 s. The documented bound is 4 minutes, measured from the address change; the new address first appears when the connection returns, so \"connection back\" is the change time."
  echo; cat summary.txt; echo
  echo "Write, in plain markdown and under 250 words: (1) a verdict against the 4-minute bound, or why the test is void; (2) a one-paragraph dated runbook entry for 2026-10-16 in the style of: **2026-10-16** — #49 step 3 ... with the measured times; (3) if the bound was exceeded, the smallest change that would shrink it (timer interval or dropping the 135 s check) as a proposal only."
} > prompt.txt
claude -p "$(cat prompt.txt)" --model sonnet --tools "" --max-budget-usd 1 --no-session-persistence --output-format json > claude.json 2> claude.err \
  && jq -r '.result' claude.json > report.md || log "headless Claude failed: see claude.err; summary.txt still stands"
log "done"
