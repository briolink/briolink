#!/usr/bin/env bash
# Rebuilds Briolink's Oracle A1 shared-compute box from scratch via the OCI CLI
# (E9, docs/4-choices.md). Run from the tower, by hand, with your own
# `oci setup config` already done — this is the "setup script" the runbook
# (ops/RUNBOOK.md) and docs/3-hosting.md's Backups table point at, not the
# provisioning tool (E18/OpenTofu): that stays for after the first client,
# when this gets adopted with `import` blocks instead of replaced by it.
#
# UNVERIFIED: written without a local OCI CLI to test against. Review each
# resource it reports creating against the console before trusting it,
# especially on the first run.
#
# Idempotent-ish: each step checks for an existing resource by name first.
# Requires: oci CLI, jq.
set -euo pipefail

: "${COMPARTMENT_OCID:?Set COMPARTMENT_OCID to the briolink (root) compartment OCID}"
REGION="${REGION:-ca-toronto-1}"
VCN_NAME="${VCN_NAME:-briolink-vcn}"
VCN_CIDR="${VCN_CIDR:-10.0.0.0/16}"
SUBNET_NAME="${SUBNET_NAME:-public subnet-briolink-vcn}"
SUBNET_CIDR="${SUBNET_CIDR:-10.0.0.0/24}"
INSTANCE_NAME="${INSTANCE_NAME:-briolink-a1}"
SHAPE="${SHAPE:-VM.Standard.A1.Flex}"
OCPUS="${OCPUS:-2}"
MEMORY_GB="${MEMORY_GB:-12}"
SSH_PUBLIC_KEY_FILE="${SSH_PUBLIC_KEY_FILE:-$HOME/.ssh/briolink_ops_ed25519.pub}"
BUDGET_NAME="${BUDGET_NAME:-FreeTierTripwire}"
BUDGET_AMOUNT="${BUDGET_AMOUNT:-1}"
MONITORING_GROUP_NAME="${MONITORING_GROUP_NAME:-Monitoring-Group}"
MONITORING_POLICY_NAME="${MONITORING_POLICY_NAME:-Monitoring-Policy}"
BASTION_NAME="${BASTION_NAME:-briolink-bastion}"

command -v oci >/dev/null || { echo "oci CLI not found; run 'oci setup config' first" >&2; exit 1; }
command -v jq >/dev/null || { echo "jq not found; install it first" >&2; exit 1; }
[ -f "$SSH_PUBLIC_KEY_FILE" ] || { echo "SSH key not found at $SSH_PUBLIC_KEY_FILE" >&2; exit 1; }

log() { echo "==> $*" >&2; }

ensure_vcn() {
  local id
  id=$(oci network vcn list --compartment-id "$COMPARTMENT_OCID" --display-name "$VCN_NAME" \
    --query "data[0].id" --raw-output 2>/dev/null || true)
  if [ -n "${id:-}" ] && [ "$id" != "null" ]; then
    log "VCN $VCN_NAME exists: $id"
  else
    log "Creating VCN $VCN_NAME ($VCN_CIDR)"
    id=$(oci network vcn create --compartment-id "$COMPARTMENT_OCID" \
      --display-name "$VCN_NAME" --cidr-block "$VCN_CIDR" \
      --query "data.id" --raw-output)
  fi
  echo "$id"
}

ensure_internet_gateway() {
  local vcn_id="$1" id
  id=$(oci network internet-gateway list --compartment-id "$COMPARTMENT_OCID" --vcn-id "$vcn_id" \
    --query "data[0].id" --raw-output 2>/dev/null || true)
  if [ -n "${id:-}" ] && [ "$id" != "null" ]; then
    log "Internet gateway exists: $id"
  else
    log "Creating internet gateway"
    id=$(oci network internet-gateway create --compartment-id "$COMPARTMENT_OCID" \
      --vcn-id "$vcn_id" --is-enabled true --display-name "${VCN_NAME}-igw" \
      --query "data.id" --raw-output)
  fi
  echo "$id"
}

ensure_route_table() {
  local vcn_id="$1" igw_id="$2" id
  id=$(oci network route-table list --compartment-id "$COMPARTMENT_OCID" --vcn-id "$vcn_id" \
    --query "data[0].id" --raw-output 2>/dev/null || true)
  # Default route table is created with the VCN; point its default route at the IGW.
  oci network route-table update --rt-id "$id" --force \
    --route-rules "[{\"destination\":\"0.0.0.0/0\",\"destinationType\":\"CIDR_BLOCK\",\"networkEntityId\":\"$igw_id\"}]" \
    >/dev/null
  echo "$id"
}

ensure_security_list() {
  local vcn_id="$1" id
  id=$(oci network security-list list --compartment-id "$COMPARTMENT_OCID" --vcn-id "$vcn_id" \
    --query "data[0].id" --raw-output 2>/dev/null || true)
  # No inbound SSH: access goes through the Bastion resource, not an open port
  # (matches the manual change already made in the console).
  oci network security-list update --security-list-id "$id" --force \
    --ingress-security-rules '[
      {"source":"0.0.0.0/0","protocol":"1","icmpOptions":{"type":3,"code":4}},
      {"source":"'"$VCN_CIDR"'","protocol":"1","icmpOptions":{"type":3}}
    ]' \
    --egress-security-rules '[{"destination":"0.0.0.0/0","protocol":"all"}]' \
    >/dev/null
  echo "$id"
}

ensure_subnet() {
  local vcn_id="$1" id
  id=$(oci network subnet list --compartment-id "$COMPARTMENT_OCID" --vcn-id "$vcn_id" \
    --display-name "$SUBNET_NAME" --query "data[0].id" --raw-output 2>/dev/null || true)
  if [ -n "${id:-}" ] && [ "$id" != "null" ]; then
    log "Subnet $SUBNET_NAME exists: $id"
  else
    log "Creating subnet $SUBNET_NAME ($SUBNET_CIDR)"
    id=$(oci network subnet create --compartment-id "$COMPARTMENT_OCID" --vcn-id "$vcn_id" \
      --display-name "$SUBNET_NAME" --cidr-block "$SUBNET_CIDR" \
      --prohibit-public-ip-on-vnic false \
      --query "data.id" --raw-output)
  fi
  echo "$id"
}

ensure_budget() {
  if oci budgets budget list --compartment-id "$COMPARTMENT_OCID" \
      --query "data[?displayName=='$BUDGET_NAME'] | [0].id" --raw-output 2>/dev/null | grep -qv '^null$'; then
    log "Budget $BUDGET_NAME exists"
  else
    log "Creating budget $BUDGET_NAME (\$$BUDGET_AMOUNT/month)"
    oci budgets budget create --compartment-id "$COMPARTMENT_OCID" --target-compartment-id "$COMPARTMENT_OCID" \
      --display-name "$BUDGET_NAME" --amount "$BUDGET_AMOUNT" --reset-period MONTHLY \
      --target-type COMPARTMENT >/dev/null
  fi
}

ensure_instance() {
  local subnet_id="$1" ad id
  id=$(oci compute instance list --compartment-id "$COMPARTMENT_OCID" --display-name "$INSTANCE_NAME" \
    --lifecycle-state RUNNING --query "data[0].id" --raw-output 2>/dev/null || true)
  if [ -n "${id:-}" ] && [ "$id" != "null" ]; then
    log "Instance $INSTANCE_NAME already running: $id"
    return
  fi
  ad=$(oci iam availability-domain list --compartment-id "$COMPARTMENT_OCID" \
    --query "data[0].name" --raw-output)
  local image_id
  image_id=$(oci compute image list --compartment-id "$COMPARTMENT_OCID" \
    --operating-system "Canonical Ubuntu" --operating-system-version "24.04" --shape "$SHAPE" \
    --sort-by TIMECREATED --sort-order DESC --query "data[0].id" --raw-output)
  log "Launching $INSTANCE_NAME ($SHAPE, $OCPUS OCPU / ${MEMORY_GB}GB, Ubuntu 24.04) in $ad"
  oci compute instance launch --compartment-id "$COMPARTMENT_OCID" --availability-domain "$ad" \
    --display-name "$INSTANCE_NAME" --shape "$SHAPE" \
    --shape-config "{\"ocpus\": $OCPUS, \"memoryInGBs\": $MEMORY_GB}" \
    --image-id "$image_id" --subnet-id "$subnet_id" --assign-public-ip true \
    --ssh-authorized-keys-file "$SSH_PUBLIC_KEY_FILE" >/dev/null
}

ensure_bastion() {
  local subnet_id="$1"
  if oci bastion bastion list --compartment-id "$COMPARTMENT_OCID" \
      --query "data[?name=='$BASTION_NAME'] | [0].id" --raw-output 2>/dev/null | grep -qv '^null$'; then
    log "Bastion $BASTION_NAME exists"
  else
    log "Creating Bastion $BASTION_NAME"
    oci bastion bastion create --compartment-id "$COMPARTMENT_OCID" --name "$BASTION_NAME" \
      --bastion-type STANDARD --target-subnet-id "$subnet_id" \
      --client-cidr-block-allow-list '["0.0.0.0/0"]' >/dev/null
  fi
}

main() {
  local vcn_id igw_id subnet_id
  vcn_id=$(ensure_vcn)
  igw_id=$(ensure_internet_gateway "$vcn_id")
  ensure_route_table "$vcn_id" "$igw_id" >/dev/null
  ensure_security_list "$vcn_id" >/dev/null
  subnet_id=$(ensure_subnet "$vcn_id")
  ensure_budget
  ensure_instance "$subnet_id"
  ensure_bastion "$subnet_id"
  log "Done. Log the result in ops/RUNBOOK.md, dated."
  log "Monitoring IAM group/policy (read-only metrics) and lingering/cpu-keeper on the instance are not covered here — do those separately (the monitoring group was already created by hand; cpu-keeper is ops/a1-cpu-keeper.sh, run on the instance)."
}

main "$@"
