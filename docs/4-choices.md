# Choices

Every fixed decision and why, in one place, current state only. IDs are stable: **M** for the methodology ([1-methodology.md](1-methodology.md)), **A** for the architecture ([2-architecture.md](2-architecture.md)), **E** for the execution (our stack and providers). To change a choice, edit its row and the doc it points to; a new choice takes the next free ID in its group. Other docs cite IDs and never repeat the reason.

## Methodology

| ID | Choice | Why |
| --- | --- | --- |
| M1 | A reasoner/coder loop with human escalation: the reasoner writes the model, tests and generator templates; generators write the plumbing; a local coder writes the pure functions; no other model writes code | The local coder costs nothing per token; the reasoner is spent only on judgement; the human only on what the reasoner can't solve. One premium model writing all the code passed every benchmark but cost too much |
| M2 | Functional core, generated shell: only pure functions are hand-written | Pilot, 2026-10-07: 6 routes, 5 domain functions and a kernel in ~3 h, 135 tests green, 12.5 MB idle. The coder passed every pure function and failed on glue (~30 attempts) and on edge rules the tests didn't pin down |
| M3 | Statecharts in the model for every lifecycle; flat in version 0 | Tests derived from them close the pilot's edge-rule gap; assessed workflows map to the same form |
| M4 | Model, specs and contracts are compact JSON, decoded strictly into typed structures (unknown fields rejected) and checked by a `Validate()` method in code; no JSON Schema library, no TOON | Every model writes JSON reliably; the rules live in one place the tests cover; a young format adds complication for little gain |
| M5 | Mutation check on every pure function: every mutant must be caught | Proves the tests bite. The pilot's tests caught 32 of 32 |
| M6 | Escalation: coder within its retry budget → reasoner, lowest sufficient tier first → human | The reasoner fixes the tests or model, splits the task or writes it; most escalations are a missing test or a bad cut, which a cheaper tier solves; the human sees only what the reasoner can't solve |
| M7 | The human approves the model and high-risk actions; risk, never a model's confidence, sets the approval | Humans judge intent; checks judge code |
| M8 | One contract file per task, next to the code, with no state in it; one ticket per task; ticket text is data | Contracts are versioned and reviewed with the code and readable offline; the tracker gives discussion and status for free |
| M9 | The pipeline comes first: the reasoner bootstraps it once (P0), then the pipeline builds the rest of itself, before any client work | It can't build itself before it exists; its first job tests it |
| M10 | Roles only on command: `/briolink` binds the session until `/briolink exit`; while bound, master routes every prompt to a role (loading its brief once per session) or takes it itself; roles are per prompt, never kept; without the command a session is the plain model, fully unbound: `AGENTS.md` only says so, and the rules, roles and routing live in `agents/master.md`, loaded by the command. A local flag file (`.briolink/bound`) holds the binding for the whole folder, inherited by every session started there; the tool's hooks re-inject it on every prompt, so the tool, not the model's memory, keeps the state. Command, briefs and `AGENTS.md` are per repo, copied into every project by the template. Separate agents only for real parallel work. Sessions start on the `routine` tier's model; a prompt routed to a role of another tier halts until the founder switches the model | A role switch costs a ~300-word brief, read once per session; a spawned agent starts cold and re-reads its context. Routing each prompt on its content needs no stored role, so nothing breaks across sessions; a brief already in the context is not reread. A session can't switch its own model and a spawned agent can't hold a design dialogue, so the founder switches, prompted by master; the default keeps most prompts on the cheaper tier (M11) |
| M11 | Spend by cost: each job goes to the cheapest executor that can pass it; compact, stable-first inputs; fresh threads per job; lasting fixes (tests, model, generator templates) before written code | Reasoner tokens are the recurring cost and the plan's limit; a prompt grows with every retry and every resumed turn; a fix in a test or template pays off on every later task, a written function only once |
| M12 | Changes start in the model: a change is a model diff, a bug a failing test; only pure functions whose signature, rules or tests changed become tasks | The reasoner reads only the delta; generators and existing tests cover the rest for free; the human gate stays at the model |
| M13 | Shared control: every setting a machine acts on is readable and changeable by the human in the same place and interface; machines propose setting changes with evidence, the human approves; every automatic decision records the setting behind it | The founder can always see why something happened and override it; no executor can widen its own budget or tier; nothing drifts unseen |
| M14 | No memory: every session starts from the repo, and the context is cleared after each piece of work, once `/briolink done` has checked everything is saved; the state of the work lives only in the docs, issues, pull requests and run records, written before a session ends. A tool's memory features and earlier conversations are never relied on. Sessions ask, loops act: a session offers the roadmap's lead and waits; only the runner chains work by itself, within queued contracts | Sessions restart, get summarised or run on another machine and lose what they held; anything kept elsewhere can't be read by the founder, other agents or the next session (M13). Planning stays the founder's call; the loop only runs what was approved |

## Architecture

| ID | Choice | Why |
| --- | --- | --- |
| A1 | Opinionated inside, agnostic at the edges: our code is fixed; every provider, machine, model, brand and client is a role (port) filled by an adapter chosen by config | Replicable installations, no hard coupling |
| A2 | Bindings are thin (config for a standard protocol) or deep (the provider's full feature set, on purpose, only for large stable providers). Deep or thin, a binding stays behind its port and has a prepared fallback and an exit | Use stable providers fully without being trapped |
| A3 | One headless control plane: an HTTP JSON API plus Server-Sent Events; every UI is a client; the runner is a module of it; no separate orchestrator or CLI coding tool | One place to see and control everything; extra tools added little |
| A4 | Work is tracked on the code host: issues, milestones and pull-request reviews are the queue, the task view and the code approvals; the control plane mirrors tasks in its API and keeps run records internal; it starts when the first code lands | A familiar workflow for the founder and clients; no early UI to build; approvals enforced by protected branches; clients keep their ticket history |
| A5 | Everything is a plugin, generators included: client apps and the pipeline are a minimal core plus generic plugins chosen per project, compiled in; each spec kind belongs to the plugin that ships its generator, so we write our own generators | Nothing forced on a client; every capability reusable; a generator is ours to extend with our conventions and checks, and the core stays small |
| A6 | In every language and layer: standard library first, no frameworks. A third-party library only when it is needed (the standard library lacks it and our own would be large or security-critical), strong (widely used, reputable, stable, few dependencies) and under a permissive licence that allows commercial use and closed redistribution (MIT, BSD, Apache-2.0, ISC; no copyleft); vendored and listed here, pinned by `go.sum`. **Exception:** a dependency whose vendored tree would bloat every clone (measured, not guessed) is instead a pre-seeded module cache mounted read-only into the offline build container ([#4](https://github.com/briolink/briolink/issues/4)), kept only in this repo and its platform forks; a client project repo still vendors it, so a client's exit ([3-hosting.md](3-hosting.md)) stays fully offline from `git clone` alone. Admitted: `modernc.org/sqlite` (BSD-3; 137 MB vendored, measured in [#13](https://github.com/briolink/briolink/issues/13) — the exception applies here, decided in [#41](https://github.com/briolink/briolink/issues/41)), `golang.org/x/crypto` (BSD-3, vendored). Tools we run are not dependencies, but their licence must allow commercial use | Small, auditable, offline builds; code we ship to clients carries no licence obligations; one 137 MB C-runtime transliteration shouldn't live in every clone of the platform repo, but a client keeping nothing but its own repo must still build alone |
| A7 | Public pages are baked to object storage and served by the edge; live actions are JS modules calling the API | Pages load from the edge; sites survive a server failure |
| A8 | No cache purge, ever: HTML is never edge-cached, media is content-addressed and immutable, live JSON lives 2 s | Nothing to forget |
| A9 | The API is cross-origin but same-site: exact allowed origins, a host-only `__Host-` cookie with `SameSite=Strict`, an Origin check on every write, JSON-only bodies | No shared-domain cookie, no proxy; `SameSite=None` would weaken CSRF protection |
| A10 | Clients own their accounts (domain, edge, offsite backup, code host); Briolink owns the shared compute. One repo per client holding the core and its plugins | A client can leave with everything |
| A11 | One rootless pod per client under its own Linux user; servers pull deploys; automation never holds production write keys; one infrastructure writer, applied with a passphrase the founder enters | Small blast radius; nothing always-on can change production |
| A12 | One writer per database: a SQLite file per instance, replicated continuously to two places; multi-node only for a measured need | Far less memory, simple backups and failover |
| A13 | Failover to a warm standby on the ops host, guarded by a lease | ~2–5 min of live features down, ~1 s of data lost |
| A14 | Two environments per client, preview and production; preview is the staging | Preview is already a full copy with its own data |
| A15 | Trials are one multi-tenant system on a separate preview domain, isolated by storage (not table prefixes), with quotas; a trial becomes production by config only | Near-zero cost per trial; a flagged trial can't touch the brand domain |
| A16 | Hosting tiers: shared → pooled → dedicated (in the client's account) | Free first, then low-cost servers, moved by the same routine |
| A17 | Money as integers in the currency's minor unit; percentages in basis points | Floats can't represent money exactly |
| A18 | Images through the `media` plugin's per-project options; private files in a private bucket, served by session-checked 5-minute presigned URLs | Public upload URLs leak through e-mails, logs and forwards |
| A19 | Evidence is internal workflow data, never client personal data; it never expires and is deleted only by the founder, audited. Client data deletion duties become approvals | Nothing is deleted silently; legal deadlines are still met |
| A20 | The control center is private: on the private network only, under a real hostname with a DNS-01 certificate; password (argon2id) + TOTP; secrets sealed with AES-256-GCM | HTTPS on a real name with nothing listening publicly; standard library crypto |
| A21 | A hub-and-spoke private network; servers dial out and keep zero inbound ports | No exposed admin surface |
| A22 | Two identities on the code host: the founder (sessions, reviews, merges) and the bot (every write made by automation). Automation never acts as the founder; the founder is the only approver, and only the founder can merge into `main` | A review means something only if the reviewer isn't the author; the bot's reach is bounded by its own permissions; anyone can tell who did what |

## Execution

### Stack (fixed inside)

| ID | Choice | Why |
| --- | --- | --- |
| E1 | **Go**, pinned in `go.mod` (`go 1.27.0`, `toolchain go1.27.2`); build workers use a Go image pinned by digest | The pilot (M2). ~15–30 MB per idle instance against ~75–100 MB for PHP-FPM + nginx; one static binary for arm64 and amd64; safe HTML templates; every session fetches the right toolchain itself |
| E2 | **SQLite** (WAL) through `modernc.org/sqlite` (pure Go, no cgo), `ATTACH` disabled; **Litestream** for replication; no LiteFS | No database server; a tenant is a plain file (A12) |
| E3 | **Vanilla JS ES modules** (A6); **esbuild** tree-shakes and minifies at build, keeping stable file names | Simple, long-lived code; smaller assets; one fast tool with no dependencies |
| E4 | **Rootless Podman + Quadlet** in production, Docker Compose in development | The same image everywhere; no root daemon |

### Bindings

| ID | Role | Choice | Depth | Why |
| --- | --- | --- | --- | --- |
| E5 | Edge | Cloudflare, Free plan, in the client's account: DNS, Pages + one Function, R2 custom domain, Tunnel, Access, Turnstile, WAF | Deep | Every edge need on one free plan; the client owns it |
| E6 | Object storage | R2 in production; Garage for trials and development; one S3 client for both | Thin | A trial becomes production by config |
| E7 | Lease store | One R2 object in Briolink's account | Thin | Conditional writes, no extra service |
| E8 | Offsite backup | Backblaze B2, EU Central, in the client's account | Thin | S3 API; keys that can't delete; cheap |
| E9 | Shared compute | Oracle Cloud A1 Always Free (arm64, 2 OCPU, 12 GB, 200 GB) on a Pay As You Go tenancy | Thin | $0 within the free limits; PAYG avoids most capacity errors |
| E10 | Pooled and dedicated compute | netcup ARM first; Contabo or OVH as alternatives; EU data centres | Thin | Low-cost European servers |
| E11 | Ops host | The tower: an always-on Windows 11 machine (12 threads, 32 GB, 16 GB GPU) running WSL2 | Thin | Already owned; its GPU runs the coder |
| E12 | Private network | WireGuard (`wg-quick`), hub on the ops host | Thin | An open standard with no coordination service or third-party account |
| E13 | Code host | GitHub: the platform in `briolink/briolink`, each client in its own org; Issues, Actions, GHCR | Deep | Stable, and recognised by clients (A4) |
| E14 | Coder model | `qwen3-coder:30b` through Ollama on the ops host's GPU, as the routing table's `coder` tier | Thin | Free per token; passed every pure function in the pilot and led the earlier benchmarks (M2) |
| E15 | Reasoner model | Claude through headless Claude Code, on the founder's Pro subscription, never an API key; two tiers mapped in the routing table ([2-architecture.md](2-architecture.md#routing)): `routine` = Sonnet, `top` = Opus | Deep | Reuses the plan already paid for; the routine tier stretches the plan's limits (M11); the founder's split, refined by run records |
| E16 | Notifier | Telegram bot, e-mail fallback | Thin | Free, immediate |
| E17 | External pinger | healthchecks.io (free) | Thin | A dead-man's switch outside our infrastructure |
| E18 | Provisioning | OpenTofu, exact provider pins, state encrypted in the client's offsite bucket | Thin | Open licence; client-side state encryption |
| E19 | Crawler tools | Playwright, Lighthouse, axe-core, inside assessment containers | Thin | The reference tools for each measure |
| E20 | Brand | Briolink, `briolink.ca`; trials on a separate neutral domain (O4) | — | — |
| E21 | Mail sender | Not chosen (O2) | Thin | — |

## Bindings in detail

Each entry: **fallback** (how we keep working while it fails, prepared in advance) and **exit** (the replacement). Open checks are the spike issues ([5-roadmap.md](5-roadmap.md#checklist-spikes-and-open-decisions)).

### E5–E7 · Cloudflare (edge, object storage, lease)

| Contract item | Feature |
| --- | --- |
| DNS API | Cloudflare DNS (proxied records) |
| Static hosting + request function | Pages project + one Pages Function (`functions/[[path]].js`) with an R2 binding |
| Public bucket domain | R2 custom domain on the media bucket |
| Outbound-only tunnel | Cloudflare Tunnel (cloudflared), ingress defined on the server (`config_src = "local"`) |
| Access gateway | Access (staff e-mails, one-time PIN) |
| Bot challenge | Turnstile |
| Rate limiting | The one Free-plan rule, on `/v1/auth/*` |
| Edge cache honouring origin TTL | Cache Rules: images eligible; `/live/*.json` needs a rule |
| Lease | R2 conditional write on one object |

- **Limits:** the free certificate covers one label (hence one-level hostnames). Each HTML view is one Function request out of 100,000 free per day per account; alert at 60,000, fix with Workers Paid ($5/month) on that account. R2 free: 10 GB, 1M Class A and 10M Class B operations a month (~3M reads at 100,000 views a day); a card on file.
- **Specifics:** `*.pages.dev` is redirected (production) or refused (preview), with Access on preview deployments. `R2.get(key, { onlyIf: request.headers })` serves conditional reads; `_routes.json` keeps `/assets/*` off the Function. Pages deploys through Cloudflare's code-host app, once per client org. Live updates beyond polling: a Durable Object with WebSockets.
- **Preview behind Access:** one Access application covers `preview.` and `preview-api.` with credentials allowed; in preview, `boot.js` calls `/v1/health` and on failure sends the top window to `preview-api.<domain>/_access?return=<url>`, which signs the tester in and redirects back.
- **Monitoring key:** reads analytics, zone, tunnel and Pages status (usage through GraphQL analytics); changes nothing.
- **Provisioned:** zone settings (SSL Full strict, HSTS, AI-crawler block) and rules; Pages projects, domains, env vars, bindings; R2 buckets (site, media, originals, private) with location hint `weur`; both tunnels and their records; Access applications.
- **Fallback:** no hot fallback for a provider-wide outage, an accepted risk (DNS is part of it, so a second CDN wouldn't be reachable). A lost client account affects only that client: rebuild from its repo and offsite backup, then re-bake. Lease unreachable: O11.
- **Exit:** another CDN with edge functions and S3-compatible storage, or a small Go request server behind any CDN (which also removes the request quota). It must pass the serving contract test.

### E8 · Backblaze B2

- Keys list and write, never delete; lifecycle rules expire old versions. Free caps (10 GB, 1 GB/day download) lift with a card.
- **Fallback:** the second replica on the ops host. **Exit:** any S3-compatible bucket in the client's account.

### E9 · Oracle Cloud A1

| Volume | Size | Holds |
| --- | --- | --- |
| System | ~47 GB | OS, images, logs |
| `prod-data` | ~100 GB | Client databases, logs, Litestream state (~0.3 GB per client) |
| `trials-data` | ~30 GB | Trial storage and databases |
| Unallocated | ~20 GB | Growth (volumes grow, never shrink) |

- **Idle reclaim:** Oracle may reclaim an Always Free VM when CPU (p95), network and memory all stay under 20% for 7 days. `cpu-keeper` runs from day one: a systemd timer (`00,06,12,18:00`, 10 min jitter) starting `stress-ng --cpu 1 --cpu-load 70 --timeout 30m` at `Nice=19`, `CPUSchedulingPolicy=idle`. Prefer useful work (imports, load tests, re-bakes, restore tests); disable it once real load holds the p95 above the line.
- **Terms:** the agreement limits use to "internal business operations" and forbids "service bureau use", so client hosting is a grey zone with a termination risk, accepted on the strength of the failover.
- **Keys:** monitoring can read metrics and inspect instances; a separate failover key can only stop this instance.
- **Provisioned:** a network with no ingress, the VM, a budget alert, the monitoring user. The host firewall drops client UIDs' traffic to the VCN range.
- **Fallback:** the ops host takes over (A13), then the prepared fallback VPS account within 24 h. **Exit:** any Linux host with systemd, rootless Podman and cgroup v2 (~$28/month at Oracle list price beyond the free tier).

### E10 · Pooled and dedicated servers

2026 list prices, to verify before ordering: netcup VPS 1000 ARM G11 (6 vCore, 8 GB, 256 GB) €5.26; Contabo Cloud VPS 20 (6 vCPU, 12 GB, 100 GB) €7.00; OVHcloud VPS-2 (6 vCores, 12 GB) €9.99 on annual billing.

- **Terms:** none of the three bars hosting third-party (client) workloads. netcup runs official web-hosting and vServer reseller programmes, so third-party use is contractually anticipated; Contabo's help centre states resale needs no special agreement; OVH Canada's dedicated-server Specific Conditions describe the rental as pure infrastructure with no OVH control over hosted content, and restrict only reselling the allocated IP addresses on their own. *Not fully verified:* netcup's reseller appendix and OVH's general (non-dedicated-server) terms weren't read in full text, only summarised third-party sources and the dedicated-server PDF.

### E11 · The tower

- **Setup:** WSL2 capped at 16 GB, 8 threads and 8 GB swap, mirrored networking and DNS tunnelling, started outside the login session; ~9 GB covers the standby at full capacity. BitLocker on the drive holding database copies; a UPS if possible. Development also runs here.
- **ISP terms:** the residential/mobile home-internet Service Terms cap use to the subscriber's "own individual use" and bar reselling the service or taking any benefit from a third party's use of the connection; neither servers nor VPN/hub traffic are named. Running the tower as a private-network hub (E12) and short-term fallback is personal and non-resold, but it backs billed client work, which sits close to the "individual use" line. Accepted as a grey-area risk at the same level as E9's — enforcement would be suspension, not legal action, and the exit (a rented Linux host) already covers it.
- **Sharing:** Ollama runs on Windows (the model mostly in the GPU's 16 GB, ~2 GB spilled to RAM). CPU is the contended resource: on a takeover the script pauses the workers and lowers Ollama's priority and threads. The failover drill runs while the model generates.
- **Hub:** UDP 51820 forwarded on the router; `wg.<brand domain>` kept current by a DDNS job. If the ISP uses CGNAT, the hub moves to a €3–5 VPS.
- **Location:** kept in the operator config, not here. The ISP's terms must allow a short-term fallback server and a hub.
- **Fallback:** client sites don't depend on it. The control plane's data is replicated off the machine ([3-hosting.md](3-hosting.md#control-plane)); rent a VPS and restore it, moving the hub too. Logic tasks queue until a GPU is back. While it is down the shared server has no standby; if both fail, fail over by hand onto the fallback VPS account. **Exit:** any always-on Linux machine with a GPU, owned or rented, or the coder on a separate GPU host over the private network.

### E13 · GitHub

| Contract item | Feature |
| --- | --- |
| Protected branches, required reviews | Branch protection on `main` and `production`; `CODEOWNERS` makes the founder owner of `tasks/` and `spec/` |
| Work tracking | Issues, the standard labels ([2-architecture.md](2-architecture.md#work-tracking)), milestones per stage, sub-issues for split tasks |
| CI on tags | Actions: `ci.yml` (vet, tests, JS lint, contract test), `release.yml` (on tag: tests, multi-arch image, attestation) |
| Registry with attestations | GHCR; a cleanup step keeps the last 10 versions (private storage is limited on Free) |
| Bot identity (A22) | A GitHub App, `briolink-bot`, owned by the `briolink` org and installed on each repo it works on (client orgs install it too). Permissions: contents, pull requests and issues read and write, metadata read; never administration or workflows, so it can't change settings or CI. Its private key lives in the control plane's secret store; it mints one-hour installation tokens. A ruleset lets only the App create `task/*` branches. App ID `5307371`; installation on the `briolink` org `171151281` (not secrets; they move into `operator.json` when it exists) |
| Keys | Deploy timer: a fine-grained, packages read-only token per client; other scopes in [3-hosting.md](3-hosting.md#control-plane) |

- **Provisioned per client:** the repo from the template, branch protection, required reviews, `CODEOWNERS`, the labels, Actions variables.
- **Fallback:** the ops host keeps a mirror of every repo (`git clone --mirror`, every few minutes) and a nightly JSON export of issues, comments, labels and milestones in Briolink's ops bucket. During an outage running sites are unaffected; the runner works on held tasks from the mirror and pushes later; an urgent fix can be built on the ops host and loaded with `podman save` / `podman load` over the private network, with the founder's approval.
- **Exit:** Forgejo, self-hosted (issues, pull requests, GitHub-style CI, an OCI registry), fed from the mirror and the export; or GitLab.

### E14–E15 · Models

**Coder.** Ollama's HTTP API on the private interface. Still to measure in P0: first-pass rate per task kind. **Fallback:** if the local setup fails, the `coder` tier switches to Claude Haiku through a hosted `Coder` adapter (one line in the routing table, by pull request), at its per-token cost or the plan's limits. **Exit:** any local model behind an HTTP API, bound per task kind if useful.

- **Spike #15 (measured 2026-10-14, on the ops host / WSL2 tower):** `qwen3-coder:30b` through Ollama's HTTP API (`/api/generate`), three representative pure-function prompts (70–220 output tokens, `eval_count`/`eval_duration` from the response). Idle GPU: steady-state ~48–50 tok/s (e.g. 1.4s for 70 tokens, 4.2s for 206). Under contention — a concurrent `qwen3-embedding:0.6b` load on the same GPU, standing in for the tower's only real contention case (the founder's own coding or gaming sessions, not a steady background load): throughput drops to ~16–18 tok/s, roughly a 3x slowdown (the 206-token case: ~4.2s idle vs ~12.3s contended). Feeds the coder adapter's budget default: size a task's time budget off ~45–50 tok/s uncontended, and allow up to 3x that when the tower is in active use for something else. Real gaming-session contention is unmeasured — an exception case, not worth blocking the spike on.

**Reasoner.** Deep: we rely on Claude Code's headless mode, tool restrictions, sessions and budget caps.

| Need | Flag |
| --- | --- |
| Non-interactive on the subscription | `claude -p`, without `--bare` (which needs an API key) |
| Session ID and cost estimate | `--output-format json` |
| Validated results | `--json-schema` |
| Tools limited to the workspace | `--allowedTools` |
| Unattended | `--permission-mode dontAsk --permission-prompts none` |
| Tier per job | `--model sonnet\|opus` |
| Interactive sessions | `"model": "sonnet"` in `.claude/settings.json`; `/model opus` for a top-tier role (M10) |
| One thread per job (an escalation's retry, a spec dialogue) | `--resume <session>` |
| A runaway-loop trip-wire | `--max-budget-usd` |
| The two limit windows, as a percentage | `cachedUsageUtilization.utilization.{five_hour,seven_day}.utilization` in `~/.claude.json`, refreshed on every call |

- **Plan (checked 2026-10-13):** `claude -p` draws from the Pro plan's limits; Pro has no API credits. Using a consumer plan for client work is a grey area in the Consumer Terms, an accepted risk; it can't be resolved by research, only by a fallback that absorbs a provider pushback without the runner depending on the subscription path.
- **Spike #5 (measured 2026-10-14, on the ops host / WSL2 tower, qwen3's own box):** installed with `npm i -g @anthropic-ai/claude-code`, inherits the founder's existing subscription login (`claude auth status`: Pro, no separate login step needed). `--output-format json` carries `session_id` and `total_cost_usd` as designed. `--max-budget-usd` does halt a run (`terminal_reason: "budget_exhausted"`) but only after the in-flight turn finishes, so real spend can overshoot the cap substantially (measured: $0.001 cap, $0.207 actual) — on Pro's flat fee this number is a list-price estimate, not a bill, so it's kept only as a circuit breaker against a runaway loop, never as the queuing signal. The real queuing signal is the plan's own two usage windows: Claude Code already tracks `five_hour` and `seven_day` percentages and caches them locally in `~/.claude.json` (`cachedUsageUtilization`, refreshed whenever any session talks to the API) — no extra call, no touching the OAuth token (reading `.credentials.json` or probing the binary for its auth calls is correctly blocked by the harness and wasn't pursued). A daily systemd user timer (`reasoner-spike-5.timer`, lingering enabled) now probes login + both percentages once a day, logging to `~/ops/reasoner-spike-5/probe.log` on the tower, to answer the one thing that needs days, not a session: does the login survive unattended.
- **Fallback:** at the plan's limit (`five_hour` or `seven_day` utilization at or near 100), reasoner work queues and the coder keeps going; with the top tier's limit reached, design jobs wait and routine jobs continue. If unattended use is blocked or the login expires, each job becomes a hand-off package (task, attempts, failures, expected output) the founder runs in the editor extension with `/briolink escalation`; the result returns through the repo. Roles are one project command, `.claude/commands/briolink.md`, which loads the brief (M10).
- **Exit:** any reasoning model with structured output and cost reporting.

Both adapters record per call: model, prompt and spec version, tokens, cost, duration, result.

### E12, E16–E19 · The rest

- **WireGuard:** fallback, the providers' web consoles. Exit: any encrypted peer network servers dial out to.
- **Notifier:** fallback, e-mail. **Pinger:** fallback, the ops host's own checks and the daily digest's silence.
- **OpenTofu:** fallback, the runbooks (every step is also written by hand).
- **Crawler tools:** run pinned inside the assessment image; nothing else depends on them.
