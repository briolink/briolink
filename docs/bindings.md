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
| Code host | GitHub (the client's org), Actions, GHCR | [adapters/github.md](adapters/github.md) | |
| Coder model | `qwen3-coder:30b` through Ollama's HTTP API, on the ops host's GPU | [adapters/models.md](adapters/models.md) | |
| Reasoner model | Claude (Opus) through headless Claude Code, on the founder's subscription | [adapters/models.md](adapters/models.md) | Terms check pending (decision 58) |
| Notifier | Telegram bot, e-mail fallback | — | |
| External pinger | healthchecks.io (free) | — | |
| Mail sender | Not chosen yet (O2) | — | |
| Provisioning | OpenTofu | [operations.md](operations.md#infrastructure-as-code) | Exact provider pins; state encrypted in the client's offsite bucket |
| Crawler tools | Playwright, Lighthouse, axe-core | [platform.md](platform.md#website-intelligence-engine) | Run inside assessment worker containers |
| First market | Tunisia (and Libya), through a partner agency | [markets/tunisia.md](markets/tunisia.md) | |

## Compute price reference

2026 list prices; verify before ordering.

| Server | CPU / RAM / disk | Per month |
| --- | --- | --- |
| Oracle A1 beyond the free tier | 2 OCPU / 12 GB | ~$28 |
| Contabo Cloud VPS 20 | 6 vCPU / 12 GB / 100 GB NVMe | €7.00 |
| netcup VPS 1000 ARM G11 | 6 vCore / 8 GB / 256 GB NVMe | €5.26 |
| OVHcloud VPS-2 | 6 vCores / 12 GB | €9.99 (annual billing) |
