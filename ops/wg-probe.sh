#!/usr/bin/env bash
# Probes TCP reachability across the private network and prints a UTC-stamped
# verdict per target (#49). Run on a spoke: wg-probe.sh <host:port>...
#   reached-open    connected: the packet crossed the hub and something listens
#   reached-closed  connection refused: the packet crossed, nothing listens
#   dropped         timed out: a firewall silently discarded it
# Compare with what A21 allows; a "reached-*" on a forbidden path is a failure.
set -u

for target in "$@"; do
  host=${target%:*}; port=${target##*:}
  out=$(timeout 4 bash -c "exec 3<>/dev/tcp/$host/$port" 2>&1); rc=$?
  if [ $rc -eq 0 ]; then v=reached-open
  elif [ $rc -eq 124 ]; then v=dropped
  elif grep -qi "refused" <<<"$out"; then v=reached-closed
  else v="unknown($rc: $out)"; fi
  echo "$(date -u +%FT%TZ) $host:$port $v"
done
