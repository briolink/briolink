# Servers: runtime, backups, failover, trials

Each client runs as **one rootless Podman pod under its own Linux user**, started by systemd through Quadlet. Every database is copied continuously to the client's own offsite backup and to the ops host. If the shared server fails, the ops host takes over: live features are down **~2–5 minutes**, **~1 second** of data is lost, and public sites never go down. The same image runs in development, on arm64 servers and on the amd64 ops host. Today's providers: [bindings.md](bindings.md).

## Pod contents

| Container | Role | Notes |
| --- | --- | --- |
| `app-prod` | Go binary: API, back office, bake worker | Memory limit (`GOMEMLIMIT` ~40 MB plus a container limit) |
| `app-prev` | The same binary, preview version | **On demand:** started by a deploy or from the control center, stopped after 24 h idle |
| `tunnel` | The edge's tunnel agent, serving production and preview hostnames | One per client, because each tunnel lives in the client's own edge account |

Backups are not in the pod: **one shared Litestream daemon** runs on the host under a dedicated `backup` user.

## Isolation

A compromised client can read only its own files, reach only its own database, and use only its own share of CPU and memory.

| Layer | Per client (example: `acme`) | Guard |
| --- | --- | --- |
| Linux user | `c-acme`: no shell, no sudo, its own subordinate UID range, lingering enabled. Home `/srv/acme`, mode 0750 | Rootless: root inside a container is an unprivileged UID on the host; no shared root daemon |
| Containers | Quadlet units in the user's `~/.config/containers/systemd/` | Read-only root file system, `--cap-drop=all`, `no-new-privileges`; only `var/` (database, logs) is writable |
| Resources | cgroup v2 delegated to the user, limits per unit | Memory, CPU and process-count caps |
| Network | One network namespace per pod; **no published ports** | The tunnel agent reaches the apps on localhost inside the pod. A host nftables rule drops traffic from the client's UID to cloud metadata (169.254.169.254), the private network and the provider's internal range |
| Tunnel | Config defined on the server, mounted read-only | A compromised client edge account can't point the tunnel elsewhere |
| Database | SQLite file per instance in the client's `var/`; `ATTACH` disabled | Other users can't read it; SQL injection can't open another file |
| Secrets | Podman secrets owned by the client's user, under `/run/secrets` | Never in the image, the repo or an unencrypted backup |
| Shell access | None for clients. The founder uses SSH over the private network only, key-only | No public ingress: provider and host firewalls drop inbound traffic |

## Deploys are pulled, not pushed

CI never holds a key to the servers. A compromised client repo can publish a bad image, but it runs inside its own isolated pod.

1. A tag (`v1.4.0`) on `main` runs the release workflow: tests, then one image for arm64 and amd64, pushed to the client's container registry with a build attestation.
2. On the server, a per-client user timer runs every 2 minutes with a read-only registry token. Preview takes the newest tag; production takes the tag at the head of `production`.
3. The timer pulls by digest, checks the attestation, and runs migrations in a one-off container.
4. It points the Quadlet units at the new digest, restarts them, and smoke-tests `/v1/health` inside the pod. On failure it goes back to the previous digest and raises an alert.
5. Migrations follow expand-then-contract, so rolling back code never needs a schema rollback.
6. The ops host pulls the same tags (amd64), so it is always ready.

## Capacity

Per-client memory (*estimate*, measured by the 150-pod spike):

| Part | RAM |
| --- | --- |
| Production Go app, average | ~30 MB |
| Tunnel agent | ~25 MB |
| Pod overhead | ~5 MB |
| Share of the Litestream daemon | ~1 MB |
| Preview app | 0 when stopped (~15 MB running) |
| **Per client** | **~60 MB** |

**A 12 GB server** holds **~150 clients** after these reservations:
- OS and tooling: ~0.8 GB;
- the trial system: ~0.4 GB;
- file cache: ~1 GB;
- spike margin: ~0.8 GB.

The current shared server's specifics (disk plan, free-tier rules) are in [adapters/oracle.md](adapters/oracle.md).

**Limits:**
- **CPU:** a Go API request takes a few milliseconds. The real risk is several live events at the same moment.
- **Blast radius:** a shared-server failure moves every client on it to the ops host at once.
- **Release valve:** the trial system is one instance and can move to a small VPS in an hour.

## Hosting tiers

Clients start on the shared tier and move up when they outgrow it. A move is routine, because every client is the same pod wherever it runs.

| Tier | Where | Who pays | Move up when |
| --- | --- | --- | --- |
| 1 · Shared | A free-tier server, up to ~150 clients + the trial system | Nobody | The client sits near its memory cap, or the server is full |
| 2 · Pooled | A low-cost VPS shared by many clients | Briolink, from hosting fees | The client's load justifies its own server |
| 3 · Dedicated | A VPS in the client's own provider account | The client | — |

The control plane raises a "promote" alert from 7-day trends in memory, CPU and disk.

Planning prices for each tier: [bindings.md](bindings.md#compute-price-reference).

## Backups

Only what can't be rebuilt is backed up. Baked HTML and resized images are regenerated from the database and the masters.

| What | How | When | Kept |
| --- | --- | --- | --- |
| Each client database | Litestream → client's offsite backup + the ops host | Continuous (~1 s) | Point-in-time window (for example 30 days) + monthly snapshots for 12 months |
| Image masters and private files | Private buckets; copied to the offsite backup without deletes | Nightly | Hidden versions kept 60 days |
| Instance config and secrets | Encrypted (age) copy to the ops host and Briolink's ops bucket | On every change | Last 10 |
| Server | Rebuilt from the setup script and runbook | — | — |

**Litestream** ships every change within about a second, plus regular snapshots, and restores a database to any moment in the retention window. The app doesn't know it exists.
- One shared daemon per server, running as a `backup` user.
- Two replicas per database: the client's offsite backup, with the client's own key, and the ops host, over the private network.
- **Backup keys can list and write but not delete**, and lifecycle rules handle expiry. A compromised server can't erase a client's history.

**Checks:**
- **Heartbeat:** alert when a replica is older than 5 minutes.
- **Restore test:** monthly, from the offsite backup, with integrity and row-count checks. Alert when the last good test is older than 35 days.

**Service level offered to clients:** best effort on low-cost infrastructure. Recovery point ~1 s. Live features restored within minutes by the standby, and on a replacement server within 24 h.

## Moving a client to another server

1. Prepare the target with the same image tag (setup script, Quadlet units, secrets).
2. Turn on the app's **maintenance flag**: live actions show a notice; public pages keep serving from the edge.
3. Let Litestream catch up, restore the database on the target, copy secrets and tunnel credentials.
4. Stop the tunnel on the old server and start it on the new one. **Same tunnel ID, so no DNS change.**
5. Smoke-test, turn the flag off, remove the old pod after 7 days.

## Failover

Applies if the shared server fails for any reason, including a provider terminating the account.

**Ready in advance:**
- **Data:** the ops host holds a live copy of every database (Litestream's second replica) and every client's encrypted config.
- **Pods:** every client's pod is pre-created on the ops host and stopped, with images already pulled.
- **Tunnels:** the ops host has its **own tunnel per client**.
- **Primary lease:** one object in the lease store with an epoch number, updated with conditional writes. The primary renews it, and every scheduled job checks it before running.
- **Fallback provider:** a VPS account created in advance and kept unused.
- **CPU sharing:** on takeover, the ops host pauses assessment and build workers and lowers the coder model's priority ([adapters/tower.md](adapters/tower.md)).

**Takeover:**

| Step | How | Time |
| --- | --- | --- |
| Detect | Health checks every 30 s from the ops host and the external pinger; 3 failures in a row | ~1–1.5 min |
| Claim | The ops host takes the lease with a new epoch. The old primary **stops itself** (apps and jobs stopped, databases read-only) when it can't confirm the lease. Best effort: the ops host also stops the old instance through the compute provider's API, with a narrowly scoped key | Seconds |
| Start | Restore each database from the local copy and start the pods | ~1–2 min for 150 small databases |
| Reroute | The edge's DNS API points each client's API hostname at the ops host's tunnel | Seconds |
| **Total** | | **~2–5 min of live features down, ~1 s of data lost** |

The lease means **two servers can never write at the same time**. Failover depends neither on the private network nor on the failed server.

**After a takeover:**
1. Start a replacement server from the runbook **within 24 hours**. The ops host is a fallback for hours, not a long-term host.
2. Restore the databases there from the ops host's replicas.
3. Move clients over one by one.
4. Return the ops host to standby. **Failback is manual only.**

Rehearse once before the first client launch, then yearly.

## Trials

Prospects get a free trial on the brand's preview domain:
- **Creation:** sales (Briolink or a partner) creates one from a form in seconds, at no cost per trial. A trial can also be the "your site, rebuilt" step of an audit.
- **One system:** all trials share **one multi-tenant trial system** in a fixed resource pool on the shared server.
- **Conversion:** on payment, a trial becomes the client's preview environment, on their own domain and accounts, **with no code changes**.

**Setup (once, H2):**
- **Preview domain:** cheap and neutral (O4), separate from the brand domain, so a Safe Browsing flag on one trial can't touch the brand.
- **DNS:** one wildcard record → one tunnel → the trial system. Each trial uses `<tenant>.`, `<tenant>-api.` and `<tenant>-admin.`.
- **One instance:** a Go app (tenant chosen by hostname), self-hosted S3-compatible storage and a tunnel agent, in its own pod under its own user (~0.4 GB, low CPU priority).
- **Storage:** one bucket and key per tenant, valid for that bucket only, with a **100 MB quota**.
- **Nothing per trial at the edge provider.**

**Tenant isolation:**

| Shared path | Rule |
| --- | --- |
| Database | One SQLite file per tenant, opened only after the tenant is resolved from the hostname; `ATTACH` disabled |
| Open databases | Only recently used tenants stay open (for example the last 100) |
| Object storage | One bucket and key per tenant; secrets derived as `HMAC(master_secret, tenant_id)` |
| Sessions | In the tenant's own database; host-only cookie |
| Temp files, cache | One directory per tenant; every cache key prefixed with the tenant ID through one helper |
| Bake worker | The tenant's own storage key per job; no tenant data kept between jobs |
| Logs | Tenant ID on every line |
| Access | A signed invite link plus the app's own staff login; noindex everywhere |

The remaining risk is a bug in hostname-to-tenant resolution. Such a request would land in another trial without a valid session, so it would see only public pages. **Trials hold test data only**, which also keeps them outside data-protection filings.

**Lifecycle:**
1. **Create:** database from a template, bucket and key, demo data, invite links.
2. **Test:** 100 MB quota, item cap, trial image profile.
3. **Extend or expire:** 30 days by default, with a reminder at 7 days.
4. **Convert:**
   - create the client's accounts and pod;
   - copy the tenant's database;
   - sync the bucket to their object storage;
   - regenerate the derived images;
   - switch config to their domain, then re-bake.
5. **Delete:** an unconverted trial is deleted with its data **15 days after expiry**.

**Capacity:** at a 1-in-10 conversion rate, 100 clients in two years means ~60 trials at once. The system is sized for ~150 and alerts at 80%.
