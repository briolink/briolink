# Hosting

How the **Host** offer runs: serving, servers, backups, failover, trials and operations. Roles are in [2-architecture.md](2-architecture.md#roles-and-adapters); today's providers and the reasons are in [4-choices.md](4-choices.md).

**Fit:** a site that is mostly something to show, plus forms, orders, bookings or live features; a database up to ~10 GB; tolerance of ~1 s of data loss and a few minutes' outage of live features (public pages stay up); at most hundreds of writes a second. **Decline:** zero-downtime contracts needing several writing servers, very high write volume, regulated data (medical records, banking, stored card data), hosting that must stay in one country, clients keeping their old CMS and its plugins.

## Serving

Public pages never touch a server (A7): they are baked to HTML in object storage and served by the edge from the client's own account. Servers handle only live actions and baking.

```text
Browser ──HTML, JS──────────────▶ Edge: static hosting + request function ─▶ object storage (site)
        ──images, live JSON─────▶ Edge: public bucket domain ──────────────▶ object storage (media)
        ──API calls, uploads────▶ Edge: tunnel (rate limit, bot challenge, access gateway on admin)
                                        └─ outbound-only ─▶ Pod: Go app (API + bake worker), SQLite
Pod ──bake: S3 PUT──▶ object storage      Pod ──Litestream, ~1 s──▶ offsite backup + ops host
```

| Component | Role | Owned by (A10) |
| --- | --- | --- |
| Domain, DNS, static hosting, object storage, tunnels, access gateway | Edge | Client |
| Database replicas, copies of image originals | Offsite backup | Client |
| Repo, CI, container images | Code host | Client |
| API instances | Compute | Briolink (dedicated tier: client) |
| Trial system, warm standby, control plane | Compute, ops host | Briolink |

### Environments and hostnames (A14)

Preview and production each have their own hostnames, buckets, site project, instance, database and tunnel; they share only the code.

| Role | Production | Preview | Protection |
| --- | --- | --- | --- |
| Site | `www.<domain>` | `preview.<domain>` | Preview: access gateway + `noindex` |
| Media | `media.<domain>` | `preview.<domain>/media/*` (through the function, so the gateway covers it) | As above |
| API | `api.<domain>` | `preview-api.<domain>` | Prod: rate limit, bot challenge on login and sign-up. Preview: gateway |
| Back office | `admin.<domain>` | `preview-admin.<domain>` | Gateway (staff e-mails) + TOTP |
| Apex | `<domain>` → 301 to www | — | — |

- One label deep only (`preview-api.`, not `api.preview.`), so free edge certificates cover every name. Keep the old site's canonical host.
- Preview deploys `main`; production deploys `production`, which the founder fast-forwards to a tagged commit. Preview apps start on a deploy or on demand and stop after 24 h idle.
- **Config flows one way:** the instance's `.env` (`SITE_ORIGIN`, `API_ORIGIN`, `MEDIA_ORIGIN`, `ROBOTS`, `ENV`, storage endpoint, keys, bucket) → the baker writes the public values into every page as `<meta name="app-config">` → the edge function gets the same values as env vars. Changing the domain is config plus a full re-bake: that is how a preview or a trial becomes production.

### Client repo (from the template)

```text
<client>/
├── AGENTS.md          same skeleton as this repo: AGENTS.md, agents/ and the /briolink command (synced from the template)
├── api/               Go module: cmd/server/, spec/, gen/, internal/{core,plugins,domain}/, templates/, migrations/, tests/
├── app/               public/ (ES modules, CSS, fonts, headers), edge/ (the request function)
├── container/         Containerfile (arm64 + amd64), Quadlet unit templates
├── compose.yaml       dev stack
├── docs/              the client's specs, decisions, bindings
├── tasks/             task contracts
├── infra/             provisioning root
└── ops/               instance.env.example, RUNBOOK.md
```

The core and the selected plugins are copied in, so the client owns a complete working copy with a perpetual licence to them. Secrets never enter the repo.

### Baked and live

A page is re-baked within about a minute of a change; anything faster is fetched live.

| Data | Where | Fresh |
| --- | --- | --- |
| Content pages | Baked HTML | < 1 min after an edit |
| Search and filter index | Baked JSON per section plus a small global index; split above ~300 KB gzipped | Same |
| Live state (current price, count, end time, stock) | `live/<id>.json` in the media bucket | 2 s edge TTL; the user's own action is confirmed in the response |
| Per-user data | API, `no-store` | Live |

With 1,000 viewers polling every 2 s, the server sees about one read per edge location every 2 s. Beyond polling, the edge's WebSocket service on the client's account.

**Baking steps:**
1. **Record:** a domain write inserts `bake_outbox(entity, entity_id, reason)` in the same transaction; a unique key on pending rows merges repeated edits.
2. **Claim:** the bake worker (a goroutine in the API binary) takes jobs one at a time; SQLite's single writer prevents double claims.
3. **Plan:** a pure function turns an entity into page keys (its page, its parent, the section index, the home page if featured, the sitemap).
4. **Render** with `html/template`; skip the upload if the hash matches `baked_pages.sha256`, so a full re-bake is cheap.
5. **Publish:** S3 PUT with `Content-Type` and `Cache-Control` as metadata, in the order images → pages → index and sitemap.
6. **Remove:** overwrite with a stub carrying `status=410`, or `status=301` plus `location`, honoured by the function. The old site's URL map is a baked `redirects.json`.
7. **Re-bake everything** after template or config changes and at launch; safe to repeat.

### Images and files (A18)

The `media` plugin: uploads go through the API (duplicate hash, real type check); keys are content-addressed (`img/<sha256>/<variant>.<ext>`); EXIF orientation applied and metadata dropped from public images; private files go to a private bucket.

| Option | Default | Example profile |
| --- | --- | --- |
| Stored format, codec | As uploaded, none | WebP, browser-encoded when possible, else server-side at q75 |
| Thumbnail | None | 300 px at upload |
| Derived sizes at bake | None | 480, 960, 1600 px with `srcset`, plus a 1200×630 `og:image` JPEG |
| Master | Kept as uploaded | Kept in the private originals bucket |
| EXIF on private files | Dropped | Kept where location matters |
| Trial profile | As production | Master + thumbnail only |

### Every public page carries

Title, description and canonical from `SITE_ORIGIN`; Open Graph tags with an absolute `og:image` from `MEDIA_ORIGIN` and `twitter:card=summary_large_image`; JSON-LD where useful; `hreflang` on multilingual sites and `dir="rtl"` where needed; `noindex` on previews and trials. Check og tags on production during the launch rehearsal (the gateway blocks link previews).

### Serving contract

The request function, bound to the site bucket, must in order:
1. Redirect the provider's default hostnames: 301 to the canonical host in production, 403 in preview.
2. Normalise the path: trailing slash or upper case → 301; `/` → `pages/index.html`; `/items/123-name` → `pages/items/123-name.html`.
3. Apply `redirects.json` (cached in memory, refreshed every 60 s).
4. Read with the request's conditional headers; a matching ETag returns 304.
5. Honour stubs (`410`, `301`); a missing key serves `pages/404.html` with 404.
6. Set `Cache-Control`, `ETag`, `X-Robots-Tag`, HSTS, `X-Content-Type-Options`, `Referrer-Policy`, and a CSP whose `connect-src` allows only the API and media origins.
7. In preview, serve `/media/*` from the preview media bucket.

Assets never run the function. One contract test (URLs → expected status and headers) runs against every edge adapter, preview and the trial system.

### Cache semantics (A8)

| Resource | `Cache-Control` | Edge cache |
| --- | --- | --- |
| Baked HTML | `public, max-age=0, must-revalidate` + ETag | None |
| Index JSON | `public, max-age=60, stale-while-revalidate=300` | None |
| Sitemap, robots, redirects | `public, max-age=3600` | None |
| JS modules, CSS | `public, max-age=0, must-revalidate` | Static hosting |
| Fonts | `public, max-age=31536000, immutable` | Static hosting |
| Images | `public, max-age=31536000, immutable` | Yes |
| Live JSON | `public, max-age=2` | Yes |
| API | `no-store` | Bypass |

Built modules keep stable names, because baked pages reference them and a deploy may remove old files.

### CORS, cookies and CSRF (A9)

- **CORS:** `Access-Control-Allow-Origin` is the request's Origin only if it is exactly `SITE_ORIGIN` or `ADMIN_ORIGIN`; credentials true; methods `GET, POST, PUT, DELETE`; header `Content-Type`; max age 7200; `Vary: Origin`.
- **Cookie:** `__Host-sid=<random>; Path=/; Secure; HttpOnly; SameSite=Strict`, host-only on `api.`, sent by `fetch(…, { credentials: 'include' })`. Sessions are server-side: 14 days sliding for users, 8 h for staff; the ID rotates at login; JS reads the state from `GET /v1/me`.
- **CSRF:** every non-GET needs `Origin` equal to an allowed origin (no Origin is rejected); JSON bodies only; no user content on another subdomain.
- **Abuse:** bot challenge on sign-up, login and registration; edge rate limit on `/v1/auth/*`; per-account limits in the app.
- **Back office:** a static SPA on `admin.`, behind the gateway, with TOTP for staff.

## Servers

Each client runs as **one rootless pod under its own Linux user**, started by systemd (A11). The same image runs in development, on arm64 servers and on the amd64 ops host.

| Container | Role |
| --- | --- |
| `app-prod` | The Go binary: API, back office, bake worker; `GOMEMLIMIT` ~40 MB plus a container limit |
| `app-prev` | The preview version, on demand |
| `tunnel` | The edge's tunnel agent for both environments, one per client (each lives in the client's edge account) |

One shared Litestream daemon runs on the host under a `backup` user.

### Isolation

| Layer | Per client (`acme`) | Guard |
| --- | --- | --- |
| User | `c-acme`: no shell, no sudo, own subordinate UIDs, lingering; home `/srv/acme` 0750 | Root in a container is an unprivileged host UID |
| Containers | Quadlet units in the user's systemd directory | Read-only root, `--cap-drop=all`, `no-new-privileges`; only `var/` writable |
| Resources | Delegated cgroup v2 | Memory, CPU and process caps |
| Network | One namespace per pod, no published ports | nftables drops the UID's traffic to cloud metadata, the private network and the provider's internal range |
| Tunnel | Config on the server, mounted read-only | A compromised edge account can't redirect it |
| Database | A SQLite file per instance in `var/`, `ATTACH` disabled | Other users can't read it |
| Secrets | Podman secrets owned by the user | Never in the image, repo or an unencrypted backup |
| Access | No client shell; founder SSH over the private network, keys only | No public ingress |

### Deploys are pulled

1. A tag on `main` runs the release workflow: tests, one image for arm64 and amd64, pushed with a build attestation.
2. A per-client timer on the server checks every 2 minutes with a read-only token: preview takes the newest tag, production the tag at the head of `production`.
3. It pulls by digest, verifies the attestation and runs migrations in a one-off container (expand then contract, so code rollbacks never need a schema rollback).
4. It switches the units to the new digest, restarts, smoke-tests `/v1/health`, and on failure rolls back and alerts.
5. The ops host pulls the same tags, so it is always ready.

### Capacity and tiers (A16)

Per client (*estimate*, measured by the 150-pod spike): app ~30 MB, tunnel agent ~25 MB, pod ~5 MB, Litestream share ~1 MB, preview 0 when stopped: **~60 MB**. A 12 GB server holds **~150 clients** after reserving ~0.8 GB for the OS, ~0.4 GB for trials, ~1 GB file cache and ~0.8 GB margin. The real CPU risk is several live events at once; the blast radius of a shared-server failure is every client on it.

| Tier | Where | Paid by | Move up when |
| --- | --- | --- | --- |
| Shared | A free-tier server, ~150 clients + trials | Nobody | Near the memory cap, or the server is full |
| Pooled | A low-cost server shared by many | Briolink | The client's load justifies its own |
| Dedicated | A server in the client's own account | The client | — |

The control plane raises a "promote" alert from 7-day trends.

**Moving a client:** prepare the target with the same tag; turn on the app's maintenance flag (public pages keep serving); let replication catch up and restore there; stop the tunnel on the old server and start it on the new one (same tunnel ID, no DNS change); smoke-test, clear the flag, remove the old pod after 7 days.

## Backups

Only what can't be rebuilt is backed up; baked pages and derived images are regenerated.

| What | How | Kept |
| --- | --- | --- |
| Each database | Litestream, continuously (~1 s), to the client's offsite backup (client's key) and to the ops host (private network) | A point-in-time window (e.g. 30 days) + monthly snapshots for 12 months |
| Image masters, private files | Nightly copy to the offsite backup, no deletes | Hidden versions 60 days |
| Instance config and secrets | Encrypted (age) copy to the ops host and Briolink's ops bucket, on change | Last 10 |
| Servers | Rebuilt from the setup script and runbook | — |

Backup keys can't delete; lifecycle rules expire. Alerts: a replica older than 5 minutes; a monthly restore test (integrity and row counts) older than 35 days. **Service level offered:** best effort; recovery point ~1 s; live features back within minutes on the standby, and on a replacement server within 24 h.

## Failover (A13)

If the shared server fails for any reason, including the provider closing the account.

**Ready in advance:** a live copy of every database and config on the ops host; every pod pre-created there, stopped, images pulled; the ops host's own tunnel per client; a fallback server account, unused; a **lease** (one object with an epoch number, conditional writes) that the primary renews and every scheduled job checks.

| Step | How | Time |
| --- | --- | --- |
| Detect | Checks every 30 s from the ops host and the external pinger; 3 failures in a row | ~1–1.5 min |
| Claim | The ops host takes the lease with a new epoch. The old primary stops itself (apps and jobs stopped, databases read-only) on its next refused renew, or when neither a renew nor a promise is valid (below); best effort, the ops host also stops it through the provider's API with a narrow key | Seconds |
| Start | Restore each database from the local copy, start the pods | ~1–2 min for 150 |
| Reroute | The edge's DNS API points each API hostname at the ops host's tunnel | Seconds |

**The lease (A23):** one object `{epoch, holder}`, written only with `If-Match` on the last etag. Each side times it on its own monotonic clock, never a shared wall clock.

- **Renew:** the primary renews every 10 s. The lease is valid until the moment it sent its last successful renew, plus 30 s.
- **Claim:** the ops host, the only claimant, claims after it has seen the same etag unchanged for 45 s and its last promise has ended, plus a margin. It then writes epoch+1 with `If-Match` on that etag.
- **Promise:** when a renew fails without a refusal (timeout, 5xx), the primary asks the ops host over the private network. The ops host answers "not claimed, epoch N, valid for 30 s" only while it isn't claiming, and records each promise's end.
- **Stop:** a refused renew (412) means stop at once, whatever the ops host says. Otherwise the primary stops when neither a renew nor a promise is valid. Scheduled jobs check the same way.

Two servers can never write at once, and failover depends neither on the private network nor on the failed server: the network only lets the primary stay up through a lease store outage. Accepted gap: if the lease store is down and the primary dies, nothing fails over until the store is back. **After:** a replacement server within 24 h, restored from the ops host's replicas; clients moved one by one; failback by hand only. Rehearse before the first client launch, then yearly.

## Trials (A15)

Free, time-limited sites on the preview domain, created from a form in seconds; a trial can be the "your site, rebuilt" step of an assessment. On payment it becomes the client's preview environment with no code change.

- **One system:** a multi-tenant Go app (tenant from the hostname), self-hosted S3 storage and a tunnel agent, in its own pod (~0.4 GB, low CPU priority). One wildcard record → one tunnel; names `<tenant>.`, `<tenant>-api.`, `<tenant>-admin.`. Nothing per trial at the edge provider.
- **Isolation:** a SQLite file per tenant opened only after the tenant is resolved, `ATTACH` disabled, only recent tenants kept open; one bucket and key per tenant, secrets derived as `HMAC(master_secret, tenant_id)`; sessions in the tenant's database with host-only cookies; per-tenant temp directories and cache keys through one helper; per-job storage keys in the bake worker; the tenant ID on every log line; access by signed invite link plus staff login. The residual risk, a hostname-resolution bug, exposes only public pages. Trials hold test data only.
- **Lifecycle:** create (template database, bucket, demo data, invites) → test (100 MB quota, item cap, trial image profile) → expire after 30 days (reminder at 7) → convert (accounts and pod, copy the database, sync the bucket, regenerate images, switch config, re-bake) or delete 15 days after expiry.
- **Capacity:** sized for ~150 at once; alert at 80%.

## Operations

### Private network (A21)

Hub and spoke, hub on the ops host. Servers dial out (`PersistentKeepalive = 25`) and keep zero inbound ports; the hub exposes one UDP port and answers no unauthenticated packet.

| Peer | Address | Inbound |
| --- | --- | --- |
| Ops host (hub) | 10.88.0.1 | One UDP port; a DNS-only record kept current by DDNS |
| Servers | 10.88.0.10–49 | None |
| Founder devices | 10.88.0.100–119 | None |

Forwarding: devices → servers SSH; devices → ops host 443 and SSH; servers → ops host replica endpoint and metrics; everything else dropped. Spokes re-resolve the hub from a timer every 2 minutes, acting only when the last handshake is over 135 s old, so a spoke is back within about 4 minutes of a hub address change (measured in [#26](https://github.com/briolink/briolink/issues/26)). The hub runs userspace WireGuard (E12). Keys are generated on each host and rotated yearly; a lost device is removed by deleting its peer. SSH listens on the private interface only, keys only, no root. Public sites and failover don't depend on the network; if the hub is down, use the providers' consoles.

### Control plane

- Runs on the ops host, bound to the hub's private address; `ops.<brand domain>` is a DNS-only record to it, with a DNS-01 certificate from a DNS token limited to the brand zone (A20). Nothing listens publicly. Setup (measured in [#25](https://github.com/briolink/briolink/issues/25)): a grey-cloud A record `ops` → the hub address; a Cloudflare token with Zone DNS Edit and Zone Read on the brand zone only; `lego run --dns cloudflare -d ops.<brand domain>` with `CLOUDFLARE_DNS_API_TOKEN` set, renewed by a timer. Issuing takes about 20 s, and spokes validate it over the private network with no resolver override.
- Login: password (argon2id) + TOTP, even on the private network; 12 h sessions; passkeys are O10.
- Secrets: each sealed with AES-256-GCM under a master key from a root-only systemd credential (also in the founder's password manager); rotated or deleted, never displayed.
- Audit log: append-only, copied daily to Briolink's ops bucket.
- Its database is replicated by Litestream to Briolink's ops bucket, beside the operator config and the encrypted secrets, so any Linux host can restore it.
- The external pinger expects a ping every 5 minutes.
- **No production write keys** (A11): a full compromise could read metrics, trigger re-bakes and run workers, never delete data, change DNS, read client databases or deploy. Failover credentials live in a separate component, limited to the lease, repointing API hostnames and stopping the shared server.

| Key | Can | Cannot |
| --- | --- | --- |
| Edge, per client | Read analytics, DNS, tunnel and site status | Change anything |
| Offsite backup, per client | List buckets, read replica metadata | Read data, write, delete |
| Compute provider | Read metrics, inspect instances | Stop, change, terminate |
| Code host, monitoring | Read metadata and tags | Push, tag, change settings |
| Code host, bot (build workers, runner) | Create and push `task/*` branches, open pull requests, comment on and label issues, on the repos it is installed on | Push `main` or `production`, approve, merge, change settings or CI |
| Instance ops API | Read status; request a re-bake or restore test | Read business data, change config, deploy |

### Checks

| Check | Every | Alert when |
| --- | --- | --- |
| Home page, a content page and `/v1/health` (database + storage write) per client | 1 min | 2 failures in a row |
| Shared server reachable (ops host + external pinger) | 30 s | 3 failures: takeover |
| Tunnel connected | 5 min | Unhealthy |
| Replication position, offsite and ops host | 5 min | Older than 5 min |
| Restore test result | Daily | Last good one > 35 days |
| Bake queue | 1 min | Pending > 10 min, or a failure |
| Running digest vs latest tag, servers and ops host | On change | Failed deploy, rollback, standby out of sync |
| Provider quotas (storage, operations, function requests, backup) | 1–6 h | Over 70% of a free limit |
| Provider rules (e.g. idle reclaim, [4-choices.md](4-choices.md#e9--oracle-cloud-a1)) | 1 h | Per provider |
| Server health pushed over the network: disk, memory per pod, updates, reboot | 5 min | Disk > 80%, a pod at its cap, updates > 7 days |
| Private network handshake per peer | 5 min | Older than 5 min |
| Promote trends, trial pool | Daily, 1 h | A client should move up; pool > 80% |
| Domain expiry (RDAP), edge certificate, credential expiry | Daily | < 30, < 14, < 30 days |

Alerts go to the notifier, with a fallback channel; de-duplicated, with recovery messages and a daily digest.

### Infrastructure as code

Until the first client launches, everything is created by hand and logged in the runbook; then the provisioning tool adopts it with `import` blocks. State is encrypted client-side and stored in the client's offsite bucket, with a sealed passphrase handed to the client. Versions are pinned exactly, the lock file committed, upgrades planned on preview first. Stacks: `platform` (Briolink's resources), a `client` module (edge, storage, backup buckets, repo and branch protection), and `add-client.sh <client>` for everything inside a server. Account sign-ups, 2FA, cards, domains and bootstrap credentials stay manual. The control plane can generate `infra/` and run a plan; applying needs the founder's passphrase (A11).

**Runbook** (`ops/RUNBOOK.md` per client): setup log; deploy and rollback; restore (from the ops host, the offsite backup, to a point in time); move servers; failover drill and takeover; launch (domain switch, re-bake, DNS); hand-over (accounts, final export, deletion within 30 days).

## Data protection

Not legal advice: check each client's local law before any real personal data is collected; previews and trials with test data come first.
- The client is the **controller**; Briolink is the **processor**; its compute, ops host, model providers, and the edge, backup and mail providers whose accounts it holds for the client (A24) are subprocessors. Providers and locations come from [4-choices.md](4-choices.md) and the operator config.
- **The processing agreement covers:** subject, duration, purpose, data categories and subjects; instructions-only processing; lasting confidentiality; a security annex built from this doc; subprocessors with advance notice and a right to object; breach notice within an agreed deadline (e.g. 48 h) and help with data subjects' requests; retention and deletion per category; the exit (client-triggered, A24: final database export, image masters, keys, account ownership; Briolink's copies deleted within 30 days; the repo and runbook let the client run alone); the service level above; a liability cap and a yearly report.
- **Special cases:** avoid storing ID scans unless required (then the private bucket with its own retention); minors need parental consent; health means appointment requests only, never medical records; client privacy policies name the processors and locations.
- Evidence and deletion duties: A19.
