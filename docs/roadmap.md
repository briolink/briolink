# Roadmap

**The pipeline comes first (decision 59).** No client work goes through the platform until P0 and P1 pass their exit criteria. Client projects keep their own plans in their own repos.

Three kinds of stage:
- **P:** platform code, built by the pipeline from P1 on.
- **H:** hosting operations, mostly founder work by hand.
- **B:** business work, with no code.

## Stages

| Stage | Delivers | Exit criteria |
| --- | --- | --- |
| **P0 · Pipeline core** (now; the reasoner model bootstraps it, decision 60) | **Control plane:** the headless API and event stream; the operator config; ports with contract tests for object storage, code host, coder and reasoner; API tokens and the audit log. No UI yet: work is tracked on the code host's issues and pull requests (decision 75). **Runner:** coder model through its port, a workspace per task in an offline build container, checks, retries, escalation to the reasoner then the founder, per-task records. **Tooling:** spec format v0, `apigen` and `sqlgen`, the mutation checker | The runner completes a benchmark set of tasks unattended (pure functions plus generated handlers), with every attempt recorded, visible through the API and summarised on each task's issue and pull request |
| **P1 · Core, first plugins, reference app** (built by the pipeline) | The app core, the plugin manifest format, the initial plugin catalog ([platform.md](platform.md#plugins)), and a small reference app picking a subset of them: sign-up and login, one state-machine feature, baking to object storage | The reference app passes its end-to-end tests. Measured: coder first-pass rate, retries, escalations to the reasoner and to the founder, cost per task |
| **P2 · Assessment engine** | Scope validation; a Go worker driving the crawler tools; the output contract; a report with a coverage table; SSRF, timeout, budget and cancellation tests | Repeatable assessments on a test set; honest coverage; no open critical security defect |
| **P3 · Control center** | The UI (decision 75): login (password + TOTP), projects, the work queue and runner views read from the API, the Approvals inbox for actions outside a repo; the Specifications area (founder ↔ reasoner model), assessment views, quality gates, Infrastructure checks ([operations.md](operations.md#checks)) | The founder runs a project from spec to preview without leaving the browser |
| **H1 · Hosting base** (alongside P1–P2; ready before the first hosted client goes live) | **Servers:** the shared server set up by hand, with its adapter's day-one setup; the private network hub and peers; a client pod; pulled deploys. **Data:** Litestream to the offsite backup and the ops host; uptime checks. **Failover:** ops-host standby (pods pre-created, tunnels, lease) and a **failover drill**. Every step goes into a runbook | Preview serves a baked page from the reference app; takeover and rebuild are timed |
| **H2 · Hosting generalised** | Template repo (core + plugin catalog + generators + spec format + sector templates); provisioning modules adopting the first client's resources; `add-client.sh`; preview domain and trial system; WordPress importer and comparison report | A throwaway client is built from scratch by the scripts alone; a plan shows no changes on a live client; a trial converts end to end |
| **P4 · Spec engine** | Backlog from assessment evidence; specs drafted from a business brief; task graphs with dependencies, risk and budgets | No task can be marked complete without its required evidence |
| **P5 · Greenfield pipeline** | Spec templates, journeys, statecharts, preview deployment with an end-to-end report | A small app built from a reviewed spec and validated on preview |
| **S · Scale selectively** | Queues, worker pools, more regions, usage accounting: each only with a measured problem statement | — |
| **B0 · Validate the audit offer** (any time, in parallel) | A sample report and its evidence requirements; authorisation and data-handling procedure; price tests with 3–5 real prospects | A repeatable scope, a usable report, and evidence that someone pays |

## Checklist

- [x] For each binding in [bindings.md](bindings.md): account, 2FA, card if needed (the setup and spike items in each adapter doc stay open)
- [x] Repo moved to `briolink/briolink` before any code lands (decision 74)
- [ ] When the first code lands, the code host is set up for work tracking (decision 75): branch protection on `main`, labels, milestones per stage, `CODEOWNERS`, required reviews; then this checklist, the spikes (here and in [adapters/](adapters/)) and the open decisions move to issues, leaving the stages here
- [ ] Mail sender chosen (O2)
- [ ] Accountant (Canada) and lawyer questions from [product.md](product.md#questions-for-professionals-canada)

## Spikes

Each spike tests one assumption; a failure only changes the area in brackets. **Provider-specific spikes are listed in each adapter doc** ([adapters/](adapters/)).

**Pipeline (P0)**
- [ ] Offline build container per task: `go build` and `go test` need no network, with dependencies vendored [isolation]
- [ ] `sqlgen` schema check: preparing each annotated statement against the migrated schema catches wrong columns, counts and parameters [generators]
- [ ] The mutation checker reproduces the pilot's result (every guard caught) on the same functions [quality]
- [ ] Port contract tests pass on two object-storage adapters (production and trial stores) [ports]

**Hosting (H1)**
- [ ] 150 dummy pods on a 12 GB shared server under fake traffic: real memory per app, tunnel agent and pod; CPU during simultaneous fake live events [capacity]
- [ ] Rootless Podman + Quadlet on an arm64 Ubuntu host: pods start at boot (lingering), cgroup limits apply, the per-UID nftables rule blocks cloud metadata, the private network and the provider's internal range [isolation]
- [ ] One shared Litestream daemon with two replicas per database; time to restore 150 small databases on the ops host [failover]
- [ ] Lease with a conditional write on the lease store; the primary stops itself when it can't confirm the lease [failover]
- [ ] Edge DNS API repoints an API hostname from one tunnel to another: how fast it takes effect [failover]
- [ ] A tunnel moved to another server keeps its ID, with no DNS change [moves]
- [ ] The serving contract test passes on the edge adapter, preview and the trial system [serving]
- [ ] `ops.<brand domain>` → private address, with a DNS-01 certificate, served by the control plane [control center]

**Assessment engine (P2)**
- [ ] Lighthouse repeatability on the ops host: score spread across 5 runs of the same page [audit quality]
- [ ] Crawling from the ops host's connection: rate limits and bot blocks on typical small-business hosts [audit coverage]

## Open decisions

| # | Decision | Options and recommendation | Decide by |
| --- | --- | --- | --- |
| O2 | Mail sender | Any provider with SMTP or an HTTP API, behind the mail port | P1 |
| O4 | Preview domain name | A cheap neutral domain | H2 |
| O5 | General prices | Validate the USD hypotheses in [product.md](product.md#offers) through B0 | End of B0 |
| O6 | Regional prices (Tunisia) | See [markets/tunisia.md](markets/tunisia.md#regional-prices) | Before the first client there |
| O10 | Passkeys for the control center | Own WebAuthn module after a security review, or TOTP only | After P3 |
| O11 | The primary when the lease store is unreachable | Today it stops itself, so a lease-store outage takes every live feature down although no takeover is possible either. Recommendation: the primary keeps running while it can reach the ops host over the private network and the ops host confirms it has not claimed the lease; it stops only when it is cut off from both. Test it in the lease spike | H1 |
