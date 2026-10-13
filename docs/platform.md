# Platform: control plane, code pipeline, audit engine

One controlled engineering pipeline with two entry paths:
- **Existing site:** observe, measure, map, prioritise, modernize, validate.
- **New app:** specify, model, plan, build, validate.

Both paths share the control plane, task contracts, the runner, the evidence store, gates, approvals and the release path. The governing rule: *automate the repeatable work, expose uncertainty, require evidence for completion, and keep human judgement for ambiguity and consequential decisions.*

Status: design. The pipeline itself comes first (P0–P1), then the audit engine (P2) ([roadmap.md](roadmap.md)).

## Pipeline

```text
project definition → spec → task graph → runner (generate · coder model · checks · escalate)
  → evidence review → approval → release → monitoring
```

## Components

**One centralised, headless control plane** (decisions 57, 65): an HTTP JSON API plus an event stream, with the browser UI as its client ([architecture.md](architecture.md#headless-control-plane)). It is one Go binary on the ops host, and worker containers do the heavy work. Every external service sits behind a port ([architecture.md](architecture.md#roles)); today's choices are in [bindings.md](bindings.md).

```text
Operator (browser, over WireGuard)
  │
Control plane: API + events (Go + SQLite, on the ops host); the control center UI is one API client
  projects · specs · queue · runner · approvals · assessments · gates · infrastructure
  │
  ├── Runner → Coder port (local model)  ·  Reasoner port (specs, tests, escalations)
  ├── Build workers (containers, one workspace per task, offline): generate, build, test, mutate
  └── Assessment workers (containers, restricted egress): Playwright, Lighthouse, axe-core
  │
Evidence store: SQLite records + artifact bucket (diffs, logs, screenshots, traces, reports)
  │
Release: preview → founder approval → production (pulled deploys, servers.md)
```

| Component | Does | Never |
| --- | --- | --- |
| Control plane | Projects, specs, task queue, scope rules, approvals, audit log, scheduling, infrastructure monitoring ([operations.md](operations.md#control-plane)) | Delegate authorisation to a model; hold production write keys |
| Runner | Executes task contracts: prompts the model, applies edits to allowed files only, runs the checks, retries, escalates, records everything | Change acceptance criteria or skip a check |
| Build workers | Generate, build, test and mutation-check one task in its own workspace | Reach the network (the code has no external dependencies to fetch); touch files outside the task's allowed paths |
| Assessment workers | Crawling, browser tests, performance and accessibility measurement | Reach private networks; change state on a target without authorisation |
| Docs (this repo, client repos) | Decisions, specs, runbooks, evaluations | Hold job state or secrets |

**Where it runs:** on the ops host, sharing the CPU with the warm standby. On a failover takeover, the workers pause and the coder model's priority drops ([servers.md](servers.md#failover)). Artifacts (the evidence store) go to Briolink's object storage. They are internal workflow data, never expire on their own, and are deleted only by the founder from the control plane (decision 72). A command-line entry point exists only for CI and debugging; like the UI, it only calls the API.

## Control center (the UI)

| Area | Shows and does |
| --- | --- |
| Projects | Client, app, type (existing / new), environment, status |
| Specifications | Requirements, journeys, rules, domain model, route and query specs; the founder discusses and approves them with the reasoner model here |
| Work queue | Tasks, dependencies, risk, status |
| Runner | Live attempts per task: model, prompt, diff, check output, retries, escalations, cost |
| Approvals | Escalated tasks and high-risk actions, with reason, impact, rollback plan and evidence |
| Assessments | Scope, routes, coverage, performance, accessibility, findings, evidence |
| Quality gates | Builds, tests, mutation scores, accessibility, release blockers |
| Infrastructure | Hosting checks and alerts ([operations.md](operations.md#checks)) |

## How code is produced

Only the coder model writes code, plus the reasoner model on escalation (decision 55). Current models: [bindings.md](bindings.md).

| Code | Written by | Checked by |
| --- | --- | --- |
| Specs, tests, generator templates | Reasoner model | Founder review of specs; the tests run |
| **Plumbing:** routing, decoding, validation, auth checks, transactions, SQL access, outbox rows, encoding | **Our generators**, from the spec | Regenerate and diff: any difference fails |
| **Pure logic:** domain rules, state transitions, calculations, planners | **Coder model** (local), through the runner | The reasoner's tests + mutation check |
| Anything the coder can't pass within its budget | Reasoner model, then the founder (escalation) | The same checks |

The evidence comes from a Go pilot on a client project (decision 39). The coder model passed pure functions under tests. It failed on glue code, and on edge rules the tests didn't pin down. Generators remove the glue; better tests close the edge rules.

**The code shape every endpoint follows:**

```text
decode → authorise → load (generated queries) → pure function → save + outbox (generated, same transaction) → encode
```

- Only the pure function is hand-written.
- Pure functions take values and return values or decisions. No I/O, no clock and no randomness: time and random values are passed in.
- That makes them easy to test, easy to mutation-check, and the kind of code the coder model writes well.
- Anything that doesn't fit this shape (streaming, uploads, baking, external calls) belongs in a plugin, specified by the reasoner model and built through the same pipeline.

### Spec format (v0, defined in P0)

Specs on disk are **compact JSON** (decision 68): every model writes it reliably, and the standard library decodes it strictly. The founder reviews specs rendered in the control center, not as raw files. The reasoner model writes them; the founder approves. What the runner sends to models is compact JSON too. Token savings come from sending only the task's slice of the spec and trimming failure output, not from exotic formats.

| File | Holds | Generator output |
| --- | --- | --- |
| `spec/routes.json` | Method, path, role required, request and response fields with validation rules, the use-case function | Handlers, route table, request validation, typed request/response structs |
| `spec/queries/*.sql` | SQL statements with header comments: name, cardinality (`:one`, `:many`, `:exec`), typed parameters, result type | Typed Go functions; a generated test that prepares every statement against the migrated schema and checks the columns |
| `spec/domain/*.json` | Pure function signatures, documentation, and the rules they implement | Function stubs and test skeletons; the reasoner model fills in the test cases |

Generated code lives in `gen/`, carries a `DO NOT EDIT` header, and is never edited by hand. A check regenerates it and fails on any diff.

### Runner

The runner is the reasoner/coder test loop, built into the control plane (decision 57). For each task contract:
1. Create a workspace (a git worktree) inside a build-worker container, with no network access.
2. Send the coder model, through the `Coder` port: the contract, the function signature, its documentation, the tests, and the previous attempt's failure output.
3. Apply the returned code to the **allowed files only**. Edits anywhere else are rejected.
4. Run the checks: `gofmt`, `go vet`, regenerate-and-diff, `go test`, then the mutation check.
5. **Pass:** record it and queue it for merge (and for approval if the task's risk requires it). **Fail:** retry with the failure output until the retry budget is spent.
6. **Escalate to the reasoner model** (decision 56), through the `Reasoner` port, with the task, every attempt and its failures. It fixes the tests or spec, splits the task, or writes the code; the same checks apply.
7. **Escalate to the founder** if the reasoner's attempt also fails. The task lands in the Approvals inbox with its full history.

**What is recorded per task:** model, attempts, durations, tokens and cost, check results, and who finally solved it. These numbers drive routing and prices.

How each model is called (flags, budgets, terms) is adapter detail: [adapters/models.md](adapters/models.md).

### Mutation check

Our own module, built on the standard library's `go/ast`. It flips operators, boundaries and constants in the pure function and reruns the tests. Every mutant must be caught; a surviving mutant means a missing test, which goes back to the reasoner model. The pilot's tests caught 32 of 32 mutants.

### Task contract

A Go type, decoded strictly (`DisallowUnknownFields`) and checked by a `Validate()` method (decision 62):

```json
{ "task_id": "", "project_id": "", "objective": "one testable outcome",
  "kind": "logic|spec|test|plugin", "spec_refs": [], "allowed_files": [], "dependencies": [],
  "acceptance_criteria": [], "verification_commands": [],
  "risk_level": "low|medium|high", "required_approvals": [],
  "retry_budget": 3, "expected_artifacts": [], "status": "queued" }
```

**A task is done only when:**
1. the expected files exist;
2. the acceptance criteria hold;
3. every check passes, including the generated-code diff and the mutation check;
4. the evidence is attached;
5. any required approvals are recorded;
6. the attempt history is saved.

**Bounded execution:**
- small tasks;
- time, token and retry budgets;
- declared paths only;
- workers can't edit their own acceptance criteria or disable gates;
- tasks can be cancelled and resumed safely.

**Risk sets the approval, not a model's confidence:**

| Risk | Examples | Policy |
| --- | --- | --- |
| Low | Docs, isolated pure functions with tests, static analysis | Runs automatically; the checks decide |
| Medium | Plugin changes, spec changes, preview config | Diff review in the control center, validation on preview |
| High | Production DB migrations, DNS, payment or auction rules, deletions, secret or access changes | Founder approval, with impact, backup and rollback plan, recorded |

## Code rules (decisions 64, 65)

- **Standard library first.** A third-party library only when it is **needed** (the standard library lacks it, and our own version would be large or security-critical) **and strong** (widely used, reputable maintainer, stable for years, few dependencies, permissive licence). Vendored, so builds stay offline, and recorded as a decision. Admitted so far: `modernc.org/sqlite` (pure Go, no cgo) and `golang.org/x/crypto` (Go team; argon2id, decision 67).
- **Ports and adapters:** core packages depend on interfaces in `port/`; providers are implemented in `adapter/<role>/<provider>/`; only `cmd/*` wires them, from the operator config.
- **Tools we run are not dependencies:** the container runtime, Litestream, the edge's tunnel agent, the provisioning tool, the coder model's runtime, and the crawler tools inside assessment workers ([bindings.md](bindings.md)).
- **Modular, publishable later:** each module is its own package with its own tests and README, depends on others only through small interfaces, and could become its own Go module.
- **Everything is a plugin** (decision 69): see [Plugins](#plugins).

## Plugins

A client app is a **minimal core plus the plugins its project needs**. Nothing is forced on a client.

**Core** (always present, kept tiny): the plugin registry and lifecycle, config loading and validation, the migration runner, and the runtime the generated plumbing targets.

**Plugins** cover every capability: routing and HTTP, sessions, auth, CORS/CSRF, storage, media, mail, baking, tenancy, search… Each plugin:
- **solves a generic capability**, never one client's case. Client-specific behaviour stays in that client's domain code, or becomes an option of the plugin;
- **ships a manifest:** name, version, purpose, options with defaults, the ports it needs (object storage, mail sender…), the plugins it depends on, and what it contributes (route and query spec fragments, migrations, templates, front-end modules);
- **has its own** tests, contract tests and README, and a semantic version.

**Selection at project creation:**
1. The architect (the reasoner model, approved by the founder) picks plugins and their options into the project's `spec/project.json`.
2. The generators write the wiring from that list. Plugins are compiled in; there is no runtime loading.
3. Each client repo receives only its selected plugins, so the client owns a complete working copy. The control plane syncs plugin updates version by version.

**Missing plugins.** When a project needs a capability the catalog lacks:
1. the architect specifies it as a generic plugin (contract, options, tests);
2. the pipeline builds it;
3. it joins the catalog for later projects.

The catalog grows with demand. The first plugins come from the first client's requirements, abstracted from day one.

**Initial catalog** (status: to build in P1):

| Plugin | Capability | Options (examples) |
| --- | --- | --- |
| `http` | Routing on `net/http` patterns, JSON decode/encode, errors | Base path, error format |
| `session` | Server-side sessions, `__Host-` cookie, rotation at login | Lifetimes per role, sliding or fixed |
| `auth` | Accounts and login | Password (argon2id), TOTP, roles; which methods are on |
| `origin` | Exact-origin CORS; `http.CrossOriginProtection` plus the Origin check | Allowed origins |
| `storage` | SigV4 client for the object-storage contract (any S3-compatible store) | Buckets per purpose |
| `media` | Uploads through the API, derived images, private files with short-lived presigned URLs | Accepted formats, conversion codec (none by default), thumbnail and derived sizes (none by default), EXIF policy, public/private buckets |
| `mail` | Transactional e-mail through the mail sender port | Sender, templates |
| `bake` | Outbox, planner, renderer, publisher | What gets baked, index split size |
| `live` | Live JSON files for fast-changing state | TTL, paths |
| `tenancy` | Tenant resolved from the hostname, per-tenant database and storage | Hostname pattern, open-database cap |

## Entry paths

**A · Existing site.**
- **Input:** URL and proof of authorisation; scope (public only, test account, routes, or source code); critical journeys; optional staging URL, test credentials and repo; crawl and rate limits, exclusions; the desired outcome.
- **The system:**
  1. validates the scope and target safety;
  2. discovers routes and templates;
  3. captures evidence;
  4. measures performance and accessibility;
  5. maps workflows;
  6. reports coverage, findings and uncertainty;
  7. proposes a backlog that the reasoner model turns into specs;
  8. hands the approved specs to the pipeline above.

**B · New application.**
- **Input:** idea and users; features and journeys; roles and rules; entities; integrations; performance, accessibility, privacy, security and compliance constraints; hosting and budget; acceptance criteria.
- **The system:**
  1. the reasoner model clarifies ambiguities;
  2. it writes the spec (journeys, entities, state transitions, routes, queries, pure functions) and the tests;
  3. the founder approves;
  4. the pipeline builds;
  5. a preview is deployed;
  6. release follows the gates and approval.

**Spec quality gate before building:**
- every critical workflow has a start condition and an expected outcome;
- important rules are testable;
- permissions are explicit;
- critical state transitions, including invalid ones, are defined;
- data ownership and retention are understood;
- integrations have contracts or are marked open;
- non-functional targets are measurable;
- costly ambiguities are resolved or assigned to a named human decision.

The reasoner model drafts, but never invents consequential business rules.

## Website intelligence engine

A deterministic evidence collector; interpretation comes afterwards. It is not an all-seeing reverse-engineering system. It is built by the pipeline in P2, and the Go worker drives Playwright, Lighthouse and axe-core as tools inside its container.

**What it collects:**
- **Routes:** `robots.txt`, sitemaps, internal links, client-side navigation; URL normalisation; templates, redirects, canonicals, broken links.
- **Interactive surface:** controls with labels and accessible names; modals, tabs and menus where they can be tested safely; validation messages.
  - Forms only under explicit authorisation.
  - Never payments, orders, real accounts, publishing or irreversible actions outside a controlled test environment.
- **Workflows as graphs:** nodes (pages, UI states), edges (actions), guards, outcomes, evidence. Observed and inferred are labelled, with confidence.
- **Performance:** Lighthouse lab runs; PageSpeed Insights and CrUX where data exists; Core Web Vitals with tool versions; status codes, cache headers, compression, asset sizes; mobile and desktop separately; raw results kept.
- **Accessibility:** axe-core per page and state; keyboard and focus checks where feasible. Automated checks are partial, and the report says so.

**Coverage, honestly.** Never claim 100%. The goal: *maximise verified coverage of the declared, in-scope, observable surface, and report the rest.* Every dimension shows its numerator, denominator and exclusions; a page counts as tested only if more than its initial load was checked.

| Dimension | Unit | Status values |
| --- | --- | --- |
| Routes | In-scope routes | discovered / tested / passed / failed / blocked / unverified |
| Templates | Distinct layouts | identified / sampled / validated / unverified |
| Interactive elements | Visible controls | discovered / exercised / passed / failed / untested |
| Workflows | Defined journeys | mapped / tested / passed / failed / unverified |
| States | Observable UI states | observed / reached / validated / unreachable |
| Performance | Page × device profile | measured / regressed / within target / unavailable |
| Accessibility | Pages and states | scanned / issues found / reviewed / unverified |

Each report also lists:
- scope and exclusions;
- environment (browser, viewports, tool versions);
- authentication role;
- network failures and rate limits;
- skipped interactions;
- blind spots and recommended manual checks.

**Keep the record types separate:**
- **Observation:** a tool recorded it.
- **Finding:** an interpretation backed by observations.
- **Hypothesis:** plausible, not verified.
- **Decision:** an approved action, with owner, rationale and date.

**Output contract:** a versioned Go type, decoded strictly. Top-level keys:
- `schema_version`, `assessment_id`, `project_id`
- `scope{target, authorization_record, included_paths, excluded_paths, authentication_profiles}`
- `environment{browser, viewport_profiles, tool_versions, started_at, completed_at}`
- `coverage{routes, templates, interactive_elements, workflows, states, performance, accessibility}`
- `routes`, `interactions`, `workflows`, `performance_results`, `accessibility_findings`, `technical_inventory`, `findings`, `limitations`, `recommended_tasks`, `artifact_refs`

Binary artifacts stay outside the JSON, referenced by stable IDs. Crawling safety: [security-and-data.md](security-and-data.md#safe-assessment).

## Quality gates and releases

**Gates (depending on the project type):**
- formatting and vet;
- regenerate-and-diff;
- unit tests and the mutation check;
- integration tests;
- browser end-to-end tests on critical journeys;
- secret scanning;
- accessibility;
- performance against a baseline;
- migration checks with a recovery plan;
- security checks within scope.

**Release stages:**
1. Runner (bounded tasks, fast checks).
2. **Preview:** each client's full preview environment, which is also staging.
3. Founder approval.
4. Production: pulled deploy, health checks, automatic rollback.
5. After release: smoke tests and close watching.

A change is done when its criteria hold, its checks pass (or an exception is approved), its diff and evidence can be reviewed, its rollback is written down, and its approvals are recorded.

## Knowledge

- **Where docs live:** platform docs here; each client's specs, decisions and runbook in that client's repo. Both open as Obsidian vaults. Job state stays in the control plane's database.
- **Traceability:** stable project and task IDs in notes and artifacts; specs versioned in Git; rejected alternatives recorded.
- **Model boundaries:** define per client what may go to an external model and what stays with the local one ([security-and-data.md](security-and-data.md#ai-models-and-client-data)).

## This repo's layout (as code arrives)

```text
go.mod                 module github.com/briolink/briolink; go 1.27.0, toolchain go1.27.2 (decision 73)
AGENTS.md              entry point for any agent (CLAUDE.md only imports it)
docs/                  design: generic docs, bindings.md, adapters/, markets/
cmd/control/           composition root: control plane API + control center (one binary)
cmd/assess/            composition root: assessment worker
spec/                  the control plane's own API spec (built with our generators)
port/                  interfaces per role, each with its contract tests
adapter/<role>/<name>/ provider implementations, selected by config
runner/                task loop: coder model, checks, retries, escalation
generate/apigen/       routes spec → handlers, validation, route table
generate/sqlgen/       annotated SQL → typed query functions + schema check test
quality/mutate/        mutation checker (go/ast)
core/                  minimal client-app core: plugin registry, config, migrations, generated-code runtime
plugins/<name>/        the plugin catalog (manifest, code, tests, README each)
web/                   control center UI (vanilla JS, built with esbuild)
```
