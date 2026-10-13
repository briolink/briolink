# Operations: network, control plane security, monitoring, provisioning

Provider-neutral. Today's providers are in [bindings.md](bindings.md), and their specifics are in [adapters/](adapters/).

## Private network

The current binding is plain WireGuard (`wg-quick`), with no coordination service and no third-party account (decision 44).

**Topology: hub and spoke, with the hub on the ops host.** Servers dial out to the hub, so they keep **zero inbound ports**. The hub exposes one UDP port, and WireGuard doesn't answer unauthenticated packets.

| Peer | Address | Connects to | Inbound |
| --- | --- | --- | --- |
| Ops host (hub) | 10.88.0.1 | — | One UDP port; a DNS-only record kept current by DDNS ([adapters/tower.md](adapters/tower.md#network-private-network-hub)) |
| Servers | 10.88.0.10–49 | The hub, `PersistentKeepalive = 25` | None |
| Founder devices | 10.88.0.100–119 | The hub | None |

**Hub forwarding policy** (nftables):

| From → to | Allowed |
| --- | --- |
| Devices → servers | SSH |
| Devices → ops host | Control center (443), SSH |
| Servers → ops host | Litestream replica endpoint, metrics push |
| Server ↔ server, servers → devices | Dropped |

**Running it:**
- **Endpoint changes:** spokes re-resolve the hub's address every 2 minutes, since the hub's public IP can change.
- **Keys:** generated on each host; the private key never leaves it. The setup script keeps the peer inventory. To remove a lost device, delete its peer on the hub. Rotate yearly.
- **SSH:** listens on the private interface only; keys only, no passwords, no root login.
- **If the hub is down:** use the providers' web consoles. Public sites and failover don't depend on the private network.

## Control plane

The control plane is headless ([architecture.md](architecture.md#headless-control-plane)), and the control center is its browser UI ([platform.md](platform.md#control-center-the-ui)).

**Hosting:**
- It runs **on the ops host**, reachable only over the private network, so it survives the loss of the shared server and can drive the failover.
- The external pinger watches it: the control plane pings every 5 minutes, and silence raises an alert.

### Security model

- **Network:**
  - binds to the hub's private address only;
  - `ops.<brand domain>` is a DNS-only record pointing to that private address;
  - the certificate comes from an ACME DNS-01 challenge, run by an ACME client tool with a DNS token limited to the brand zone.
  Nothing listens publicly.
- **Login:** password (argon2id) + TOTP, even inside the private network. Sessions last 12 hours. Passkeys are open decision O10.
- **Secrets at rest:**
  - each credential is encrypted with AES-256-GCM (standard library) under a master key, with a fresh random nonce per value;
  - the master key comes from a root-only systemd credential and is also kept in the founder's password manager;
  - secrets can be rotated or deleted, never displayed.
- **Audit log:** every action goes to an append-only table, copied daily to Briolink's ops bucket.
- **No production write keys.** A full compromise could read metrics, trigger re-bakes and run workers. It couldn't delete data, change DNS, read client databases or deploy to production.
- **Failover credentials** live in the separate failover component, limited to exactly three things: writing the lease, repointing each client's API hostname, and stopping the shared server.

| Key | Can | Cannot |
| --- | --- | --- |
| Edge, per client | Read analytics, DNS, tunnel and site status | Change anything |
| Offsite backup, per client | List buckets, read replica metadata | Read data, write or delete |
| Compute provider | Read metrics, inspect instances | Stop, change or terminate anything |
| Code host, monitoring | Read metadata and tags | Push, tag or change settings |
| Code host, build workers | Push to task branches and open pull requests on one repo | Push to `main` or `production` (protected), change settings |
| Instance ops API | Read status; request a re-bake or a restore test | Read business data, change config or deploy |

### Checks

| Check | Source | Every | Alert when |
| --- | --- | --- | --- |
| Site and API up | GET the home page, one content page and `/v1/health` (database and a storage write) per client | 1 min | 2 failures in a row |
| Shared server reachable | Health checks from the ops host and the external pinger (feeds the failover) | 30 s | 3 failures in a row: takeover |
| Tunnel connected | Edge API | 5 min | Not healthy |
| Replication fresh | Latest Litestream position, offsite and on the ops host | 5 min | Older than 5 min |
| Restore test | Monthly job result | Daily | Last good test older than 35 days |
| Bake queue | Instance ops API | 1 min | Pending over 10 min, or any failure |
| Deploys | Running digest vs latest tag, servers and ops host | On change | Failed deploy, rollback, ops host out of sync |
| Provider quotas | Each adapter's usage API (storage, operations, function requests, backup storage) | 1–6 h | Over 70% of a free limit ([adapters/](adapters/)) |
| Provider-specific rules | For example the shared server's idle-reclaim line ([adapters/oracle.md](adapters/oracle.md#idle-reclaim-line)) | 1 h | Per adapter |
| Server health | Agent pushing over the private network: disk, memory per pod, pending updates, reboot required | 5 min | Disk over 80%, a pod at its cap, updates older than 7 days |
| Private network | Latest handshake per peer | 5 min | A server's handshake older than 5 min |
| Promote signals | 7-day trends per client | Daily | A client should move up a tier |
| Trial pool | Trials at once, disk used | 1 h | Over 80% |
| Domain expiry | RDAP | Daily | Under 30 days, or auto-renew off |
| Edge certificate | Edge API + the certificate actually served | Daily | Under 14 days |
| Credential expiry | Stored expiry dates | Daily | Under 30 days |

Alerts go to the notifier, with a fallback channel. They are de-duplicated, send a recovery message, and are summed up in a daily digest.

## Infrastructure as code

**Until the first client launches, everything is created by hand and logged in a runbook.** In H2, the provisioning tool adopts those resources with `import` blocks, and the runbook becomes the setup script.

**How provisioning is run:**
- The tool must encrypt its state client-side, because state holds tunnel secrets and storage keys.
- Each client's state is stored encrypted in the client's own offsite bucket, and a sealed copy of the passphrase goes to the client at hand-over.
- Provider versions are pinned exactly and the lock file is committed. Upgrades go to preview first, with a clean plan.
- **Provisioning is split by owner:**
  - a `platform` stack for Briolink's own resources;
  - a `client` module per client (edge, storage, backup buckets, repo and branch protection);
  - `add-client.sh <client>` for everything inside a server: user, pod, Quadlet units, secrets, Litestream entry, timers.
  Each adapter doc lists its resources.
- **Stays manual:** account sign-ups, 2FA, cards, domain purchases, bootstrap credentials. The control plane tracks credential expiry.
- **The control plane never writes infrastructure.** It can generate a client's `infra/` folder and run a plan. Applying needs a passphrase the founder enters each time.

## Runbook outline (`ops/RUNBOOK.md` in each client repo)

1. Setup log (every manual step, with its date)
2. Deploy and rollback
3. Restore a database (from the ops host, from the offsite backup, to a point in time)
4. Move to another server (tier change)
5. Failover drill and real takeover
6. Launch (domain switch, full re-bake, DNS)
7. Hand-over (client exit: accounts, final export, deletion within 30 days)

## Providers' terms to check

Each adapter doc records its provider's terms. Open items:
- [ ] Shared compute: client hosting is a grey zone; the risk is accepted ([adapters/oracle.md](adapters/oracle.md#terms-decision-20)).
- [ ] Pooled and dedicated VPS providers allow hosting for third parties.
- [ ] The ops host's ISP allows a short-term fallback server and a network hub.
- [x] Reasoner model: unattended use on the subscription checked, with an editor hand-off fallback ([adapters/models.md](adapters/models.md#reasoner-claude-through-headless-claude-code)).
- [ ] AI model providers' terms for processing client code and data ([security-and-data.md](security-and-data.md#ai-models-and-client-data)).
