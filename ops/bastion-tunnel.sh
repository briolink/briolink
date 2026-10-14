#!/usr/bin/env bash
# Opens a Bastion port-forwarding session to a Briolink compute instance via
# the OCI CLI, then runs the SSH tunnel in the foreground until Ctrl-C or the
# session's TTL expires (max 3h — an OCI Bastion hard limit, not ours).
#
# Needs a scoped OCI CLI profile that can only manage bastion-session (see
# ops/RUNBOOK.md's Bastion-automation entry for the IAM setup) — never the
# founder's own/admin profile.
#
# Requires: oci CLI (configured profile), the ops SSH key's public half.
set -euo pipefail

: "${BASTION_OCID:?Set BASTION_OCID (the target Bastion resource's OCID)}"
TARGET_IP="${TARGET_IP:-10.0.0.226}"
TARGET_PORT="${TARGET_PORT:-22}"
LOCAL_PORT="${LOCAL_PORT:-18822}"
SSH_KEY="${SSH_KEY:-$HOME/.ssh/briolink_ops_ed25519}"
PROFILE="${OCI_PROFILE:-BASTION}"
SESSION_TTL="${SESSION_TTL:-10800}" # 3h, the OCI max

command -v oci >/dev/null || { echo "oci CLI not found" >&2; exit 1; }
[ -f "${SSH_KEY}.pub" ] || { echo "SSH public key not found at ${SSH_KEY}.pub" >&2; exit 1; }

echo "==> Creating port-forwarding session ($TARGET_IP:$TARGET_PORT, profile $PROFILE)" >&2
SESSION_ID=$(oci bastion session create-port-forwarding-session \
  --profile "$PROFILE" \
  --bastion-id "$BASTION_OCID" \
  --display-name "tower-$(date +%s)" \
  --ssh-public-key-file "${SSH_KEY}.pub" \
  --target-private-ip "$TARGET_IP" --target-port "$TARGET_PORT" \
  --session-ttl "$SESSION_TTL" \
  --query 'data.id' --raw-output)

echo "==> Session $SESSION_ID created, waiting for ACTIVE..." >&2
oci bastion session get --profile "$PROFILE" --session-id "$SESSION_ID" \
  --wait-for-state ACTIVE --max-wait-seconds 120 >/dev/null

SSH_CMD=$(oci bastion session get --profile "$PROFILE" --session-id "$SESSION_ID" \
  --query 'data."ssh-metadata"."command"' --raw-output)
RESOLVED=$(echo "$SSH_CMD" \
  | sed -e "s#<privateKey>#$SSH_KEY#" -e "s#<localPort>#$LOCAL_PORT#")

echo "==> Tunnel: localhost:$LOCAL_PORT -> $TARGET_IP:$TARGET_PORT (session $SESSION_ID, up to ${SESSION_TTL}s)" >&2
echo "==> Running: $RESOLVED" >&2
exec $RESOLVED
