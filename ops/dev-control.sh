#!/usr/bin/env bash
# Dev setup of the control plane's secret store (cmd/control): a throwaway-grade
# install under ~/.local/share/briolink-dev, loopback only, fake adapters (the github
# section is there for cmd/control's live test; the key's secret name is
# $BRIOLINK_BOT_KEY_SECRET, default BRIOLINK_BOT_KEY), with a
# real master key. NOT the production install (systemd-creds, /var/lib/briolink).
# Creates the folder, the master key (once, never overwritten), operator.json and
# builds bin/control. Prints the command to run; never prints the key.
# usage: ops/dev-control.sh            then: ops/dev-control.sh run <control args...>
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
D=${BRIOLINK_DEV_DIR:-$HOME/.local/share/briolink-dev}
export PATH=$PATH:/usr/local/go/bin
umask 077
mkdir -p "$D/creds"; chmod 700 "$D" "$D/creds"
[ -e "$D/creds/briolink-master-key" ] || openssl rand -hex 32 | tr -d '\n' > "$D/creds/briolink-master-key"
chmod 600 "$D/creds/briolink-master-key"
cat > "$D/operator.json" <<JSON
{ "format": "v0", "listen": "127.0.0.1:8443", "tls": null,
  "database": "$D/control.db",
  "routing_file": "$ROOT/spec/routing.json",
  "master_key_credential": "briolink-master-key",
  "adapters": { "codehost": "fake", "coder": "fake", "reasoner": "fake" },
  "github": { "repo": "briolink/briolink", "app_id": 5307371, "installation_id": 171151281,
    "key_secret": "${BRIOLINK_BOT_KEY_SECRET:-BRIOLINK_BOT_KEY}" } }
JSON
(cd "$ROOT" && go build -o bin/control ./cmd/control)
if [ "${1:-}" = run ]; then
  shift
  CREDENTIALS_DIRECTORY="$D/creds" exec "$ROOT/bin/control" -config "$D/operator.json" "$@"
fi
echo "ready: $D (back up $D/creds/briolink-master-key in your password manager)"
