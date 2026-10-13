# Roadmap

## Now

The pointer between sessions, rewritten by `/briolink done`. One line each; the details live where the links point. **Lead** is a suggestion for the founder, never an instruction: a session proposes it and waits. Only the runner's loop acts on its own, and only on queued task contracts.

- **Stage:** design, committed straight to `main`; next, P0.
- **Last (2026-10-13):** session commands: `/briolink` binds a folder flag, master routes every prompt, `/briolink done` saves before a clear (M10, M14).
- **Lead:** is the design stable enough to start P0? If so, the code host setup (Checklist) and the P0 skeleton (`go.mod`, layout, CI) would be the first issues.

The pipeline comes first (M9): no client work goes through it until P0 and P1 pass their exit criteria. Stages: **P** platform code (built by the pipeline from P1 on), **H** hosting operations (mostly by hand), **B** business work.

## Stages

| Stage | Delivers | Exit criteria |
| --- | --- | --- |
| **P0 · Pipeline core** (now; the reasoner bootstraps it) | **Control plane:** headless API and event stream, operator config, the routing table (`spec/routing.json`, served read-only), ports with contract tests (object storage, code host, coder, reasoner), API tokens, audit log; no UI. **Runner:** coder through its port, offline workspace per task, checks, retries, escalation, run records. **Tooling:** spec format v0 with statecharts, plugin manifest v0, the generator host and the first generator plugins (`domain`, `http`, `sqlite`), the mutation checker | The runner completes a benchmark set of tasks unattended (statechart-driven pure functions plus generated handlers), every attempt recorded, visible through the API and summarised on each issue and pull request |
| **P1 · Core, first plugins, reference app** | The app core, the rest of the initial catalog, a reference app using some of it: sign-up and login, one statechart-driven feature, baking | Its end-to-end tests pass. Measured: coder first-pass rate, retries, escalations per tier, cost per task |
| **P2 · Assessment engine** | Scope validation, the crawler worker, the output contract, a report with coverage, SSRF, timeout, budget and cancellation tests | Repeatable assessments on a test set; honest coverage; no open critical security defect |
| **P3 · Control center** | The UI: login, projects, specifications area, work queue and runner views, approvals, assessments, gates, infrastructure | The founder runs a project from spec to preview in the browser |
| **H1 · Hosting base** (alongside P1–P2) | The shared server by hand; the private network; a client pod; pulled deploys; replication to both targets; uptime checks; the ops-host standby and a **failover drill**; every step in a runbook | Preview serves a baked page from the reference app; takeover and rebuild are timed |
| **H2 · Hosting generalised** | The client template repo; provisioning adopting existing resources; `add-client.sh`; preview domain and trial system; CMS importer and comparison report | A throwaway client built by the scripts alone; a plan shows no changes on a live client; a trial converts end to end |
| **P4 · Spec engine** | Backlog from assessment evidence; models drafted from a business brief; task graphs with dependencies, risk and budgets | No task can complete without its evidence |
| **P5 · Greenfield pipeline** | Spec templates, journeys from statecharts, preview deployment with an end-to-end report | A small app built from a reviewed spec, validated on preview |
| **S · Scale selectively** | Queues, worker pools, regions, usage accounting, each with a measured problem | — |
| **B0 · Validate the audit offer** (any time) | A sample report, the authorisation and data-handling procedure, price tests with 3–5 real prospects | A repeatable scope, a usable report, evidence that someone pays |

## Checklist

- [x] An account with 2FA (and a card where needed) for each binding in [4-choices.md](4-choices.md#bindings)
- [x] Repo at `briolink/briolink`
- [ ] With the runner (P0): the bot, a GitHub App owned by the org (A22), and the pull-request author check
- [ ] When the first code lands: branch protection on `main`, `CODEOWNERS`, labels, milestones per stage; then this checklist, the spikes and the open decisions move to issues, leaving the stages here (A4). Until then, design work goes straight to `main`
- [ ] Oracle tenancy on Pay As You Go; `cpu-keeper` on from the day the VM is created
- [ ] Providers' terms: pooled and dedicated providers allow third-party hosting; the ops host's ISP allows a short-term fallback server and a hub; model providers' terms for client code and data

## Spikes

Each tests one assumption; a failure changes only the area in brackets.

**Pipeline (P0)**
- [ ] Offline build container per task: `go build` and `go test` need no network, with dependencies vendored [isolation]
- [ ] Statechart v0: the derived tests plus the reasoner's boundary values kill every mutant of the guards and actions on a pilot entity [statecharts]
- [ ] The `sqlite` plugin's generator: preparing every statement against the migrated schema catches wrong tables, columns (in `WHERE` too), counts and parameters, and the declared result columns match the database's [generators]
- [ ] The mutation checker reproduces the pilot's 32/32 on the same functions, cross-checked with Gremlins on them: every mutant one tool kills and the other misses is explained [quality]
- [ ] Port contract tests pass on two object-storage adapters [ports]
- [ ] Coder: tokens per second and time per task while the GPU is shared [coder]
- [ ] Reasoner: the subscription login stays valid unattended; JSON output gives cost and session ID; `--max-budget-usd` stops a run; queueing at the limit and the hand-off fallback work end to end [reasoner]
- [ ] Reasoner tiers: on the benchmark's escalations, the routine tier's solve rate and cost against the top tier's; whether a cheap replan before escalating (the routine tier re-cuts the task or rewrites the plan from the failure, then the coder retries) solves enough failures to skip escalations; proposes the first changes to `spec/routing.json` [reasoner]
- [ ] Code host fallback: restore one repo with its issues from the mirror and the export into Forgejo [code host]

**Hosting (H1)**
- [ ] 150 dummy pods on a 12 GB server under fake traffic: real memory per app, tunnel agent and pod; CPU during simultaneous live events [capacity]
- [ ] Rootless Podman + Quadlet on arm64 Ubuntu: pods start at boot, cgroup limits apply, the per-UID nftables rule blocks metadata, the private network and the provider range [isolation]
- [ ] One shared Litestream daemon with two replicas per database; time to restore 150 small databases on the ops host [failover]
- [ ] Lease with conditional writes; the primary's behaviour when it can't confirm the lease (O11) [failover]
- [ ] The edge's DNS API repoints an API hostname between tunnels: time to take effect [failover]
- [ ] A tunnel moved to another server keeps its ID, with no DNS change [moves]
- [ ] The serving contract test passes on the edge adapter, preview and the trial system [serving]
- [ ] `ops.<brand domain>` → private address with a DNS-01 certificate [control center]
- [ ] Ops host: public IPv4 (no CGNAT) and the hub's UDP port forwarded; the hub inside WSL2 routes between peers; spokes recover within ~2 min of an IP change [network]
- [ ] Edge: conditional R2 reads return correct 304s; what visitors see past 100,000 Function requests a day; a second Pages project takes `preview.` and Access covers `*.pages.dev`; the preview Access redirect workaround; a Cache Rule honours `max-age=2` on `/live/*.json`; EU-jurisdiction buckets work with a custom domain and bindings [edge]
- [ ] Garage on arm64 behaves like R2 for the storage plugin's calls; bucket quotas hold [trials]
- [ ] Whether a PAYG Oracle tenancy is exempt from idle reclaim [shared compute]
- [ ] Multi-arch builds stay within the free CI minutes and private package storage [code host]

**Assessment (P2)**
- [ ] Lighthouse spread across 5 runs of the same page on the ops host [audit quality]
- [ ] Rate limits and bot blocks when crawling typical small-business hosts from the ops host's connection [audit coverage]

## Open decisions

| # | Decision | Options and recommendation | By |
| --- | --- | --- | --- |
| O2 | Mail sender | Any provider with SMTP or an HTTP API, behind the mail port | P1 |
| O4 | Preview domain | A cheap neutral domain | H2 |
| O10 | Passkeys for the control center | Own WebAuthn module after a security review, or TOTP only | After P3 |
| O11 | The primary when the lease store is unreachable | Today it stops itself, so a lease-store outage takes every live feature down although no takeover is possible. Recommendation: keep running while the ops host, reached over the private network, confirms it has not claimed the lease; stop only when cut off from both | H1 |
