#!/usr/bin/env bash
# Keeps the DNS-only A record wg.briolink.ca equal to the tower's public IPv4
# (A21: spokes find the hub by name). Run by wg-ddns.timer as root. Per M15 it
# parses CF_API_TOKEN from the repo-root .env (path in DDNS_ENV_FILE) and never
# sources it; the zone is found by name, so no zone ID is kept. Creates the
# record if it is missing. Logs every change with a UTC timestamp.
set -euo pipefail

NAME=${DDNS_NAME:-wg.briolink.ca}
ZONE=${DDNS_ZONE:-briolink.ca}
get() { grep -m1 "^$1=" "$DDNS_ENV_FILE" | cut -d= -f2- | tr -d "\"'"; }
CF_API_TOKEN=$(get CF_API_TOKEN)
[ -n "$CF_API_TOKEN" ] || { echo "CF_API_TOKEN missing from $DDNS_ENV_FILE" >&2; exit 1; }
AUTH=(-H "Authorization: Bearer $CF_API_TOKEN" -H "Content-Type: application/json")
zone=$(curl -fsS --max-time 10 "${AUTH[@]}" "https://api.cloudflare.com/client/v4/zones?name=$ZONE" | jq -r '.result[0].id // empty')
[ -n "$zone" ] || { echo "zone $ZONE not visible to the token" >&2; exit 1; }
API=https://api.cloudflare.com/client/v4/zones/$zone/dns_records

ip=$(curl -fsS --max-time 10 https://api.ipify.org)
[[ $ip =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "bad address from lookup: $ip" >&2; exit 1; }

rec=$(curl -fsS --max-time 10 "${AUTH[@]}" "$API?type=A&name=$NAME")
id=$(jq -r '.result[0].id // empty' <<<"$rec")
cur=$(jq -r '.result[0].content // empty' <<<"$rec")

[ "$cur" = "$ip" ] && exit 0

body=$(jq -n --arg n "$NAME" --arg ip "$ip" '{type:"A",name:$n,content:$ip,ttl:60,proxied:false}')
if [ -n "$id" ]; then
  curl -fsS --max-time 10 -X PUT "${AUTH[@]}" -d "$body" "$API/$id" >/dev/null
else
  curl -fsS --max-time 10 -X POST "${AUTH[@]}" -d "$body" "$API" >/dev/null
fi
echo "$(date -u +%FT%TZ) $NAME ${cur:-none} -> $ip"
