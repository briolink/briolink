# Current bindings

The only doc that names today's providers, machines, models and brand. Generic docs refer to roles ([architecture.md](architecture.md#roles)). To swap one, add an adapter and change its row here.

| Role | Current binding | Details | Notes |
| --- | --- | --- | --- |
| Brand | Briolink, `briolink.ca`; trials on a separate neutral preview domain (O4) | [product.md](product.md#entities-and-money) | Canadian company |
| Edge | Cloudflare, Free plan, **in the client's own account**: DNS, Pages + one Function, R2 custom domain, Tunnel (cloudflared), Access, Turnstile, WAF | [adapters/cloudflare.md](adapters/cloudflare.md) | R2 needs a card on file |
| Object storage | R2 (production), Garage (trials, development) | [adapters/cloudflare.md](adapters/cloudflare.md) | Both through the `storage` plugin |
| Lease store | One R2 object in Briolink's platform account | [adapters/cloudflare.md](adapters/cloudflare.md) | |
| Offsite backup | Backblaze B2, EU Central, **in the client's own account** | — | S3 API; keys list and write, never delete; lifecycle rules; free caps (10 GB, 1 GB/day download) lifted with a card |
| Compute, shared tier | Oracle Cloud A1 (Always Free on a PAYG tenancy), arm64, EU region | [adapters/oracle.md](adapters/oracle.md) | Idle-reclaim rule; terms are a grey zone (decision 20) |
| Compute, pooled and dedicated | netcup ARM first; Contabo or OVH as alternatives; EU data centres | [servers.md](servers.md#hosting-tiers) | |
| Ops host | The tower: Windows 11 + WSL2 | [adapters/tower.md](adapters/tower.md) | Also the private-network hub; its location is in the operator config |
| Private network | WireGuard, hub on the ops host | [operations.md](operations.md#private-network) | |
| Code host | GitHub: the platform in `briolink/briolink`, each client in the client's own org; Issues, Actions, GHCR | [adapters/github.md](adapters/github.md) | The platform repo manages the derived projects' repos (decision 74); work is tracked in each repo's issues (decision 75) |
| Coder model | `qwen3-coder:30b` through Ollama's HTTP API, on the ops host's GPU | [adapters/models.md](adapters/models.md) | |
| Reasoner model | Claude (Opus) through headless Claude Code, on the founder's Pro subscription (no API key) | [adapters/models.md](adapters/models.md) | Plan limits apply; fallback is a hand-off through the editor extension (decision 71) |
| Notifier | Telegram bot, e-mail fallback | — | |
| External pinger | healthchecks.io (free) | — | |
| Mail sender | Not chosen yet (O2) | — | |
| Provisioning | OpenTofu | [operations.md](operations.md#infrastructure-as-code) | Exact provider pins; state encrypted in the client's offsite bucket |
| Crawler tools | Playwright, Lighthouse, axe-core | [platform.md](platform.md#website-intelligence-engine) | Run inside assessment worker containers |
| First market | Tunisia (and Libya), through a partner agency | [markets/tunisia.md](markets/tunisia.md) | |

## If a binding fails

Depth, fallback and exit are defined in [architecture.md](architecture.md#binding-depth-and-fallbacks). The adapter docs hold the details.

| Role | Depth | When it fails: fallback | What stops meanwhile |
| --- | --- | --- | --- |
| Edge (Cloudflare) | Deep | No hot fallback: a provider-wide outage is an accepted risk. A lost client account is rebuilt from the client's offsite backup on the exit path ([adapters/cloudflare.md](adapters/cloudflare.md#fallback)) | That client's public site and API |
| Object storage (R2) | Thin | No hot fallback: it shares the edge provider, so it fails with the edge. Nothing is lost: originals are in the offsite backup and pages are rebuilt by re-baking into any S3 store | Media, uploads, baking |
| Lease store (one R2 object) | Thin | None yet: see open decision O11 in [roadmap.md](roadmap.md#open-decisions) | Takeover; and today the primary stops itself |
| Offsite backup (B2) | Thin | Litestream's second replica on the ops host | One of the two database copies |
| Compute, shared tier (Oracle) | Thin | The ops host (the tower) takes over in ~2–5 min, then the prepared fallback VPS account within 24 h ([servers.md](servers.md#failover)) | Nothing, after the takeover |
| Ops host (the tower) | Thin | Client sites don't depend on it. The control plane is restored on a rented VPS from its database replica and the repo; the hub moves with it ([adapters/tower.md](adapters/tower.md#fallback)) | Control plane, coder model, runner, warm standby |
| Private network (WireGuard) | Thin | The providers' web consoles ([operations.md](operations.md#private-network)) | Admin access over SSH, metrics |
| Code host (GitHub) | Deep | The ops host keeps a mirror of every repo and a nightly export of issues; the runner keeps working on queued tasks and pushes when it returns. Exit: a self-hosted Forgejo on the ops host ([adapters/github.md](adapters/github.md#fallback)) | New issues, reviews, CI, new images and deploys. Running sites are unaffected |
| Coder model (Qwen) | Thin | Logic tasks queue; urgent ones escalate to the reasoner model | First attempts on logic tasks |
| Reasoner model (Claude) | Deep | Work queues at the plan's limit; hand-off packages through the editor extension ([adapters/models.md](adapters/models.md#reasoner-claude-through-headless-claude-code)) | Specs, tests, escalations |
| Notifier (Telegram) | Thin | E-mail | — |
| External pinger (healthchecks.io) | Thin | The ops host's own checks; the founder notices silence in the daily digest | The dead-man's switch on the ops host |
| Provisioning (OpenTofu) | Thin | The runbooks: every step is also written by hand | Automated changes |

## Compute price reference

2026 list prices; verify before ordering.

| Server | CPU / RAM / disk | Per month |
| --- | --- | --- |
| Oracle A1 beyond the free tier | 2 OCPU / 12 GB | ~$28 |
| Contabo Cloud VPS 20 | 6 vCPU / 12 GB / 100 GB NVMe | €7.00 |
| netcup VPS 1000 ARM G11 | 6 vCore / 8 GB / 256 GB NVMe | €5.26 |
| OVHcloud VPS-2 | 6 vCores / 12 GB | €9.99 (annual billing) |
