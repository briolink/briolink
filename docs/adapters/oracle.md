# Adapter: Oracle Cloud A1 (shared compute)

Current binding for the **shared compute** tier ([bindings.md](../bindings.md)): one Always Free Ampere A1 VM (arm64, 2 OCPU, 12 GB, 200 GB block storage) on a Pay As You Go tenancy (decision 9). PAYG costs nothing within the free limits, avoids most capacity errors, and adds support tickets.

## Disk plan

Always Free gives 200 GB including system disks. Volumes can grow but never shrink.

| Volume | Size | Holds |
| --- | --- | --- |
| System disk | ~47 GB | OS, container images, logs |
| `prod-data` | ~100 GB | Client SQLite files, logs, Litestream state (~0.3 GB per client) |
| `trials-data` | ~30 GB | Trial storage and databases (~300 full trials) |
| Unallocated | ~20 GB | Growth |

10 TB of outbound traffic a month is included.

## Idle-reclaim line

Oracle may reclaim an Always Free VM when, over 7 days, **all three** of these are under 20%: CPU (95th percentile), network, and memory. Staying above **one** is enough. What would be lost is the scarce A1 capacity, not data.

**`cpu-keeper`, from the day the VM is created (decision 40).** Four 30-minute runs a day keep one core at 70%, at the lowest priority, so real work always comes first.

```ini
# /etc/systemd/system/cpu-keeper.service
[Unit]
Description=Keep the VM above Oracle's idle-reclaim CPU line
[Service]
Type=oneshot
Nice=19
CPUSchedulingPolicy=idle
ExecStart=/usr/bin/stress-ng --cpu 1 --cpu-load 70 --timeout 30m --quiet

# /etc/systemd/system/cpu-keeper.timer
[Unit]
Description=Run cpu-keeper four times a day
[Timer]
OnCalendar=*-*-* 00,06,12,18:00:00
RandomizedDelaySec=10m
Persistent=true
[Install]
WantedBy=timers.target
```

**Setup:**
1. Install with `apt-get install -y stress-ng`.
2. Enable with `systemctl daemon-reload && systemctl enable --now cpu-keeper.timer`.
3. Check the 7-day CPU p95 in OCI Monitoring after a week.

**Prefer useful work when there is some:** imports, load tests, builds, re-bakes, restore tests.

**Phase-out:**
1. Disable the timer once real load keeps the p95 above the line on its own.
2. Later, the control plane switches it on only when load drifts toward the line.

## Terms (decision 20)

- **The risk:** the Free Tier FAQ doesn't ban commercial use, but the Cloud Services Agreement limits use to "your internal business operations" and forbids "service bureau use". Client hosting is therefore a grey zone, with a termination risk.
- **The decision:** accept it, and rely on the failover takeover plus a replacement server within 24 h ([servers.md](../servers.md#failover)).
- **Client agreements** name the providers.

## Monitoring and failover keys

- **Monitoring:** `read metrics` and `inspect instances` only. It feeds the utilisation check in the control plane: CPU p95 and memory both under 25% over 7 days, or cpu-keeper inactive while natural load is under the line.
- **Failover:** a separate, narrowly scoped key that can only stop this instance.

## Provisioning (platform stack)

- network with no ingress;
- the A1 VM;
- a budget alert;
- the read-only monitoring user.

The host firewall also drops traffic from client UIDs to the VCN range.

## Setup and spikes

- [ ] Tenancy upgraded to Pay As You Go
- [ ] `cpu-keeper` enabled on the day the VM is created
- [ ] Spike: whether a PAYG tenancy is exempt from idle reclaim

## Swap notes

Any Linux arm64 or amd64 host with systemd, rootless Podman and cgroup v2 can replace it; the same image runs everywhere. Without the free tier, the shared tier costs ~$28/month at Oracle list price, or moves to a pooled VPS ([servers.md](../servers.md#hosting-tiers)).
