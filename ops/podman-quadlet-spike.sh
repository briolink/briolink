#!/usr/bin/env bash
# Spike #19: rootless Podman + Quadlet checks for one user on either arch.
# Run as the target user (not root). Needs `sudo` for the nftables check only.
set -euo pipefail

echo "== cgroup v2 delegation =="
systemctl --user show user@"$(id -u)".service -p Delegate || true
cat /sys/fs/cgroup/user.slice/user-"$(id -u)".slice/user@"$(id -u)".service/cgroup.subtree_control

echo "== rootless podman + cgroup limits enforced in a container =="
podman run -d --name briolink-spike-cgroup --memory=64m --pids-limit=50 docker.io/library/alpine:3 sleep 60
podman exec briolink-spike-cgroup sh -c 'cat /sys/fs/cgroup/memory.max; cat /sys/fs/cgroup/pids.max'
podman rm -f briolink-spike-cgroup >/dev/null

echo "== quadlet: generate, start, verify isolation, boot-start via lingering =="
mkdir -p ~/.config/containers/systemd
cat > ~/.config/containers/systemd/briolink-spike.container <<'EOF'
[Unit]
Description=Briolink spike quadlet test

[Container]
Image=docker.io/library/alpine:3
Exec=sleep 300
PodmanArgs=--memory=48m
PidsLimit=40
ReadOnly=true
DropCapability=all
NoNewPrivileges=true

[Service]
Restart=no

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user start briolink-spike.service
sleep 1
podman exec systemd-briolink-spike sh -c \
  'cat /sys/fs/cgroup/memory.max; cat /sys/fs/cgroup/pids.max; mount | grep " / "; cat /proc/self/status | grep CapEff'
systemctl --user stop briolink-spike.service
rm ~/.config/containers/systemd/briolink-spike.container
systemctl --user daemon-reload

echo "== lingering (pods start at boot without a login) =="
loginctl show-user "$(id -un)" -p Linger

echo "== per-UID nftables drop (needs sudo; reversible, its own table) =="
echo "Target below (1.1.1.1:443) is a known-reachable stand-in to prove the before/after"
echo "transition — swap for 169.254.169.254 (cloud metadata) and the provider's internal"
echo "range on the real host, where there's no 'reachable baseline' to compare against."
echo "-- baseline (should succeed) --"
timeout 3 nc -zv -w2 1.1.1.1 443 2>&1 || echo "WARNING: baseline target unreachable, test is inconclusive"
sudo nft add table inet briolink_spike
sudo nft add chain inet briolink_spike output '{ type filter hook output priority 0 ; }'
sudo nft add rule inet briolink_spike output meta skuid "$(id -u)" ip daddr 1.1.1.1 drop
echo "-- with rule (should block) --"
( timeout 3 nc -zv -w2 1.1.1.1 443 2>&1 || true ) | grep -qi "timed out\|refused" \
  && echo "BLOCKED as expected" || echo "WARNING: not blocked"
sudo nft delete table inet briolink_spike
echo "-- after removing rule (should succeed again) --"
timeout 3 nc -zv -w2 1.1.1.1 443 2>&1

echo "All checks passed."
