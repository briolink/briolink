# Roadmap

## Now

The pointer between sessions, rewritten by `/briolink done`. One line each; the details live where the links point. **Lead** is a suggestion for the founder, never an instruction: a session proposes it and waits. Only the runner's loop acts on its own, and only on queued task contracts.

- **Stage:** P0 under way (pull requests from here); design docs still go straight to `main`.
- **Last (2026-10-21):** [#9](https://github.com/briolink/briolink/issues/9) closed: `author` is a required check on the `main` ruleset, and the bot's installation tokens are minted from the stored key ([#98](https://github.com/briolink/briolink/pull/98), tested against a fake GitHub only; the production install still needs the key seeded). [#85](https://github.com/briolink/briolink/issues/85) merged ([#97](https://github.com/briolink/briolink/pull/97)); [#88](https://github.com/briolink/briolink/issues/88) built in [#94](https://github.com/briolink/briolink/pull/94).
- **Lead:** build [#59](https://github.com/briolink/briolink/issues/59), the GitHub code host adapter as the bot (now queued), and try `TokenSource` once against the real API with the dev store; before the control plane runs for real, create the master key credential and your `admin` token, and seed the bot key, `MIRROR_GH_TOKEN` and `CF_API_TOKEN`; decide [#83](https://github.com/briolink/briolink/issues/83), then [#84](https://github.com/briolink/briolink/issues/84); pipeline side [#86](https://github.com/briolink/briolink/issues/86), [#76](https://github.com/briolink/briolink/issues/76); network side [#78](https://github.com/briolink/briolink/issues/78) still unblocks #79–#82.

The pipeline comes first (M9): no client work goes through it until P0 and P1 pass their exit criteria. Stages: **P** platform code (built by the pipeline from P1 on), **H** hosting operations (mostly by hand), **B** business work.

## Stages

| Stage | Delivers | Exit criteria |
| --- | --- | --- |
| **P0 · Pipeline core** (now; the reasoner bootstraps it) | **Control plane:** headless API and event stream, operator config, the routing table (`spec/routing.json`, served read-only), ports with contract tests (object storage, code host, coder, reasoner), API tokens, audit log; no UI. **Runner:** coder through its port, offline workspace per task, checks, retries, escalation, run records. **Tooling:** spec format v0 with statecharts, plugin manifest v0, the generator host and the first generator plugins (`domain`, `http`, `sqlite`), the mutation checker | The runner completes the P0 acceptance set ([briolink/acme](https://github.com/briolink/acme)) unattended (statechart-driven pure functions plus generated handlers), every attempt recorded, visible through the API and summarised on each issue and pull request |
| **P1 · Core, first plugins, reference app** | The app core, the rest of the initial catalog, a reference app using some of it: sign-up and login, one statechart-driven feature, baking | Its end-to-end tests pass. Measured: coder first-pass rate, retries, escalations per tier, cost per task |
| **P2 · Assessment engine** | Scope validation, the crawler worker, the output contract, a report with coverage, SSRF, timeout, budget and cancellation tests | Repeatable assessments on a test set; honest coverage; no open critical security defect |
| **P3 · Control center** | The UI: login, projects, specifications area, work queue and runner views, approvals, assessments, gates, infrastructure | The founder runs a project from spec to preview in the browser |
| **H1 · Hosting base** (alongside P1–P2) | The shared server by hand; the private network; a client pod; pulled deploys; replication to both targets; uptime checks; the ops-host standby and a **failover drill**; every step in a runbook | Preview serves a baked page from the reference app; takeover and rebuild are timed |
| **H2 · Hosting generalised** | The client template repo; provisioning adopting existing resources; `add-client.sh`; preview domain and trial system; CMS importer and comparison report | A throwaway client built by the scripts alone; a plan shows no changes on a live client; a trial converts end to end |
| **P4 · Spec engine** | Backlog from assessment evidence; models drafted from a business brief; task graphs with dependencies, risk and budgets | No task can complete without its evidence |
| **P5 · Greenfield pipeline** | Spec templates, journeys from statecharts, preview deployment with an end-to-end report | A small app built from a reviewed spec, validated on preview |
| **S · Scale selectively** | Queues, worker pools, regions, usage accounting, each with a measured problem | — |
| **B0 · Validate the audit offer** (any time) | A sample report, the authorisation and data-handling procedure, price tests with 3–5 real prospects | A repeatable scope, a usable report, evidence that someone pays |

## Checklist, spikes and open decisions

Moved to issues (A4), labelled `kind:infra`, `kind:spike` or `kind:decision`, in their milestone: [P0](https://github.com/briolink/briolink/milestone/1), [H1](https://github.com/briolink/briolink/milestone/2), [P2](https://github.com/briolink/briolink/milestone/3), [B0](https://github.com/briolink/briolink/milestone/4), [P1](https://github.com/briolink/briolink/milestone/5), [H2](https://github.com/briolink/briolink/milestone/6), [P3](https://github.com/briolink/briolink/milestone/7).
