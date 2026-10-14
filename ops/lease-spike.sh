#!/usr/bin/env bash
# Spike for #21 (E7): does a conditional write on one R2 object give a clean
# compare-and-swap for the failover lease (3-hosting.md § Failover)?
#
# Measures, against a throwaway key under lease-spike/<timestamp>/:
#   1. create-if-absent: PUT If-None-Match: * wins once, then 412
#   2. swap: PUT If-Match: <current etag> wins; a stale etag gets 412
#   3. race: CONCURRENCY writers PUT with the same If-Match, ROUNDS times;
#      exactly one must win each round (losers: 412, or 409 under contention)
#   4. read-after-write: a GET right after each win returns the winner's epoch
#   5. latency of conditional PUTs and GETs (median, p95, max), from this host
# then deletes its own keys. Run it from the tower and from the A1 instance.
#
# Needs an R2 API token scoped to one spike bucket (Object Read & Write on
# that bucket only), created by the founder — never an account-wide token.
# The secret is passed to curl through a config file descriptor, never argv.
#
# Requires: curl >= 7.75 (--aws-sigv4).
set -euo pipefail

: "${R2_ACCOUNT_ID:?Set R2_ACCOUNT_ID}"
: "${R2_BUCKET:?Set R2_BUCKET (the spike bucket)}"
: "${R2_ACCESS_KEY_ID:?Set R2_ACCESS_KEY_ID}"
: "${R2_SECRET_ACCESS_KEY:?Set R2_SECRET_ACCESS_KEY}"
CONCURRENCY="${CONCURRENCY:-8}"
ROUNDS="${ROUNDS:-20}"
SAMPLES="${SAMPLES:-30}"

BASE="https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com/${R2_BUCKET}"
PREFIX="lease-spike/$(date -u +%Y%m%dT%H%M%SZ)-$(hostname -s)"
LEASE="$PREFIX/lease.json"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
FAIL=0

# r2 <method> <key> [curl args...] — prints "<status> <etag> <seconds>",
# the body goes to $WORK/body.
r2() {
  local method=$1 key=$2; shift 2
  curl -sS -K <(printf 'user = "%s:%s"\n' "$R2_ACCESS_KEY_ID" "$R2_SECRET_ACCESS_KEY") \
    --aws-sigv4 "aws:amz:auto:s3" -X "$method" "$@" \
    -D "$WORK/hdr.$BASHPID" -o "$WORK/body.$BASHPID" \
    -w '%{http_code} %{time_total}\n' "$BASE/$key" >"$WORK/out.$BASHPID"
  local code time etag
  read -r code time <"$WORK/out.$BASHPID"
  etag=$(grep -i '^etag:' "$WORK/hdr.$BASHPID" | tr -d '\r' | cut -d' ' -f2- || true)
  mv -f "$WORK/body.$BASHPID" "$WORK/body"
  echo "$code ${etag:--} $time"
}

body() { printf '{"epoch":%d,"holder":"%s"}' "$1" "$2" >"$WORK/put.$BASHPID"; echo "$WORK/put.$BASHPID"; }
put() { r2 PUT "$1" -H 'Content-Type: application/json' --data-binary @"$(body "$2" "$3")" "${@:4}"; }
check() { if [ "$2" = "$3" ]; then echo "  ok    $1 ($2)"; else echo "  FAIL  $1 (got $2, want $3)"; FAIL=1; fi; }
stats() { sort -n | awk '{a[NR]=$1} END {printf "n=%d median=%.0fms p95=%.0fms max=%.0fms\n", NR, a[int((NR+1)/2)]*1000, a[int(NR*0.95+0.5)]*1000, a[NR]*1000}'; }

echo "==> $BASE/$PREFIX (concurrency $CONCURRENCY, $ROUNDS rounds)"

echo "1. create-if-absent"
read -r code etag _ < <(put "$LEASE" 1 first -H 'If-None-Match: *')
check "first create" "$code" 200
read -r code _ _ < <(put "$LEASE" 1 second -H 'If-None-Match: *')
check "second create refused" "$code" 412

echo "2. swap"
stale=$etag
read -r code etag _ < <(put "$LEASE" 2 swap -H "If-Match: $stale")
check "swap on current etag" "$code" 200
read -r code _ _ < <(put "$LEASE" 3 stale -H "If-Match: $stale")
check "swap on stale etag refused" "$code" 412

echo "3-4. race and read-after-write"
epoch=2 bad_rounds=0 stale_reads=0
for round in $(seq 1 "$ROUNDS"); do
  epoch=$((epoch + 1))
  for w in $(seq 1 "$CONCURRENCY"); do
    ( put "$LEASE" "$epoch" "w$w" -H "If-Match: $etag" >"$WORK/race.$w" ) &
  done
  wait
  wins=0 winner_etag=
  for w in $(seq 1 "$CONCURRENCY"); do
    read -r code e t <"$WORK/race.$w"
    echo "$t" >>"$WORK/lat.put"
    case $code in
      200) wins=$((wins + 1)); winner_etag=$e ;;
      412|409) ;;
      *) echo "  round $round writer $w: unexpected $code" ;;
    esac
    echo "$code" >>"$WORK/codes"
  done
  [ "$wins" -eq 1 ] || { echo "  round $round: $wins winners"; bad_rounds=$((bad_rounds + 1)); }
  read -r code e t < <(r2 GET "$LEASE")
  echo "$t" >>"$WORK/lat.get"
  if [ "$code" != 200 ] || [ "$e" != "$winner_etag" ] || ! grep -q "\"epoch\":$epoch," "$WORK/body"; then
    stale_reads=$((stale_reads + 1))
  fi
  etag=${winner_etag:-$e}
done
check "rounds with exactly one winner" "$((ROUNDS - bad_rounds))/$ROUNDS" "$ROUNDS/$ROUNDS"
check "reads returning the winner" "$((ROUNDS - stale_reads))/$ROUNDS" "$ROUNDS/$ROUNDS"
echo "  loser codes: $(grep -v '^200$' "$WORK/codes" | sort | uniq -c | xargs)"

echo "5. latency from $(hostname -s)"
for _ in $(seq 1 "$SAMPLES"); do
  read -r code etag t < <(put "$LEASE" 0 renew -H "If-Match: $etag")
  [ "$code" = 200 ] && echo "$t" >>"$WORK/lat.renew"
  read -r _ _ t < <(r2 GET "$LEASE"); echo "$t" >>"$WORK/lat.get"
done
echo "  renew (uncontended conditional PUT): $(stats <"$WORK/lat.renew")"
echo "  contended conditional PUT:           $(stats <"$WORK/lat.put")"
echo "  GET:                                 $(stats <"$WORK/lat.get")"

read -r code _ _ < <(r2 DELETE "$LEASE")
echo "==> cleanup: DELETE $LEASE → $code"
[ "$FAIL" -eq 0 ] && echo "==> PASS" || { echo "==> FAIL"; exit 1; }
