#!/usr/bin/env bash
# Enables cpu-keeper on an Oracle A1 Always Free instance (E9, docs/4-choices.md).
# Run ON THE INSTANCE itself (over SSH or the console), as a sudo-capable user —
# not from the tower. Idempotent: safe to re-run.
set -euo pipefail

if ! command -v stress-ng >/dev/null; then
  sudo apt-get update
  sudo apt-get install -y stress-ng
fi

sudo tee /etc/systemd/system/cpu-keeper.service >/dev/null <<'EOF'
[Unit]
Description=Keep CPU p95 above Oracle's Always Free idle-reclaim line (E9)

[Service]
Type=oneshot
Nice=19
CPUSchedulingPolicy=idle
ExecStart=/usr/bin/stress-ng --cpu 1 --cpu-load 70 --timeout 30m
EOF

sudo tee /etc/systemd/system/cpu-keeper.timer >/dev/null <<'EOF'
[Unit]
Description=Run cpu-keeper four times a day, 10 min jitter (E9)

[Timer]
OnCalendar=*-*-* 00,06,12,18:00:00
RandomizedDelaySec=10min
Persistent=true

[Install]
WantedBy=timers.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now cpu-keeper.timer

echo "--- cpu-keeper.timer ---"
systemctl status cpu-keeper.timer --no-pager || true
echo "--- next run ---"
systemctl list-timers cpu-keeper.timer --no-pager || true
