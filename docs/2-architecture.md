# Architecture

Briolink's shape for running the [methodology](1-methodology.md). It names roles, never providers: today's providers and the reasons for every choice are in [4-choices.md](4-choices.md).

Briolink sells three offers on one pipeline, in this order: **Assess** an existing site, **Build** (modernize it or build a new app from a spec), **Host** the result. Each qualifies the client for the next.

## Roles and adapters

*Choices: A1, A2.*

- Core code depends on **ports** only: small interfaces named after their role (`ObjectStore`, `Edge`, `Coder`…), each with contract tests an adapter must pass before it can be bound.
- Adapters live in `adapter/<role>/<provider>/`; only the composition root (`cmd/*`) imports them, wired from the installation's operator config (`operator.json`). Secrets are kept apart.
- Standard protocols over vendor APIs wherever one exists: S3, SMTP, WireGuard, OCI, Git, SQL, HTTP.
- A deep binding uses provider features only inside its adapter or the provider's own config files (CI workflows, edge rules). Its entry in [4-choices.md](4-choices.md#bindings-in-detail) lists every feature relied on, its fallback and its exit; what it holds stays exportable in a standard form and is copied to infrastructure we control.
- Variation is data: brand (name, domains, look), sector templates, and each client's spec and accounts.

| Role | Contract | Used by |
| --- | --- | --- |
| Edge | DNS API; static hosting with a small request function in front of object storage; a public domain on a bucket; an outbound-only tunnel; an access gateway; a bot challenge; rate limiting; an edge cache honouring origin `Cache-Control` | Client sites |
| Object storage | S3 subset: PUT with metadata, conditional GET, HEAD, DELETE, list, presigned GET and PUT | Baking, uploads, trials, evidence |
| Lease store | A conditional write on one object | Failover |
| Offsite backup | An S3-compatible bucket in the client's account; keys that list and write but can't delete; lifecycle expiry | Database replicas, nightly copies |
| Compute | A Linux host (arm64 or amd64) with systemd, rootless containers, cgroup v2; an API that can stop an instance | Client pods, trials |
| Ops host | An always-on machine for the control plane, the warm standby, the coder and the network hub | Operator side |
| Private network | An encrypted peer network that servers dial out to | Admin access, replicas, metrics |
| Code host | Git with protected branches and required reviews by code owners; issues with labels and milestones through an API; CI on tags; an OCI registry with attestations and read-only pull tokens | Repos, work tracking, approvals, deploys |
| Coder model | Local, no per-token cost; turns a task contract into code | Runner |
| Reasoner model | Structured output, cost reporting, a tier chosen per call | Runner, specifications |
| Notifier | Push alerts, de-duplicated, with a fallback channel | Control plane |
| External pinger | A dead-man's switch outside our infrastructure | Monitoring, failover |
| Mail sender | Transactional e-mail with SPF and DKIM | Client apps |
| Provisioning | Declarative infrastructure with encrypted state | Client and platform resources |
| Crawler tools | Browser automation, lab performance, accessibility checks | Assessment workers |

**An installation** is the repo, an operator config, secrets and an ops host. Another installation (region, white-label) stands up from the same scripts.

## Control plane

*Choices: A3.*

One Go binary on the ops host, reachable only over the private network ([3-hosting.md](3-hosting.md#control-plane)): an HTTP JSON API (`/api/v1`) plus Server-Sent Events. The control center UI, CI and any later client call the same endpoints and hold no logic. The API itself is built by our generators from a route spec.

```text
Control plane: API + events (ops host)
  projects · specs · tasks · runner · approvals · assessments · gates · infrastructure
  ├── Runner → Coder port · Reasoner port
  ├── Build workers: one offline container per task (generate, build, test, mutate)
  └── Assessment workers: containers with restricted egress (crawler tools)
Evidence: run records in the control plane's database + artifacts in object storage
Release: preview → founder approval → production (pulled deploys)
```

| Component | Does | Never |
| --- | --- | --- |
| Control plane | Projects, specs, task dispatch, scope rules, approvals outside a repo, audit log, scheduling, monitoring | Delegates authorisation to a model; holds production write keys; exposes a setting or action only machines can reach (M13) |
| Runner | Executes task contracts | Changes acceptance criteria or its own routing; skips a check |
| Build workers | One task in one workspace | Reach the network; touch files outside the task |
| Assessment workers | Crawling, browser tests, measurements | Reach private networks; change a target's state without authorisation |

On a failover takeover the workers pause and the coder's priority drops. A command-line entry point exists only for CI and debugging, and it calls the API too.

**Control center** (the UI, P3): login, projects, the specifications area (founder ↔ reasoner), the work queue and live runner views (prompt, diff, check output, retries, escalations, cost), approvals for actions outside a repo (evidence and data deletions), assessments, quality gates, infrastructure checks.

## Runner

*Choices: M6, M11.* The methodology's loop, as a module of the control plane:
1. Create a workspace (a git worktree) in a build container with no network.
2. Send the coder, through its port: the contract, the function signature, its documentation, the tests and the last failure output, in that order (stable first, so the prefix is cached).
3. Apply the returned code to the allowed files only; anything else is rejected.
4. Run the checks: `gofmt`, `go vet`, regenerate-and-diff, `go test`, the mutation check.
5. **Pass:** record it, open a pull request that closes the task's issue, post an attempt summary on the issue (no cost figures: client repos are visible to the client). **Fail:** retry with the failure output until the budget is spent, or escalate early when two attempts fail the same tests with the same output.
6. Escalate to the reasoner at the tiers [routing](#routing) gives (first answer, then once a second), within the task's `reasoner_usd`; the input is the contract, the model slice, the tests, the last attempt and its trimmed failure, and the earlier attempts' failing test names. The same checks apply to its answer.
7. Escalate to the founder: the issue gets `state:needs-founder` and is assigned, with a link to the full history.

## Routing

*Choices: M11, M13, E15.* Which executor and tier take each job is a table, not code: `spec/routing.json` in the platform repo, owned by the founder like `spec/`.

| Job | Tier |
| --- | --- |
| Model, statecharts, specification gate | `top` |
| Tests from a checked statechart; a change diff inside one context | `routine` |
| Escalation: first answer / second answer | `routine` / `top` |
| Assessment interpretation | `routine` |
| Session roles: architect, escalation hand-off / builder, operator, assessor, master | `top` / `routine` |

```json
{ "tiers": { "routine": "<model>", "top": "<model>" },
  "jobs": { "escalation.first": "routine", "escalation.second": "top" },
  "propose_promotion_below": 0.6, "on_top_limit": "queue-design-jobs" }
```

- **Precedence:** a project may override a job's tier in its `spec/project.json`; otherwise the platform table applies. Models per tier are in [4-choices.md](4-choices.md) (E15).
- **Human side:** the founder edits the file by pull request; before P3 that is the only way, after it the control center opens the same pull request. In a session, the founder switches the model with the tool's own command.
- **Machine side:** the control plane serves the table at `/api/v1/routing` (read-only). When a job kind's solve rate at its tier falls below `propose_promotion_below` (or stays high a tier up), it opens a pull request changing that row, with the run records as evidence. It never applies one.
- Every run record names the tier, the row that chose it and the model used.

## Work tracking

*Choices: A4, M8.*

From the first code on, each repo's issues are its tasks:
- One issue per task, linking `tasks/<id>.json`; milestones follow the roadmap stages; a split task becomes sub-issues.
- **Labels:** `kind:logic|spec|test|plugin|docs|spike|decision`, `risk:low|medium|high`, `state:queued|running|needs-reasoner|needs-founder|blocked`. Done means closed by its pull request.
- The founder owns `tasks/` and `spec/` as code owner; code is approved by pull-request review on protected branches.
- The control plane reaches issues only through the code host port and exposes tasks as `/api/v1/tasks`, so a code host swap changes one adapter. Contracts are files, so the runner keeps working from the repo mirror while the code host is down.
- A client's issue is a request: the reasoner turns it into a contract, which the founder approves.

**Until the runner exists** (P0), agents run the same flow by hand: pick an open issue in the current milestone with no `state:blocked` label and closed dependencies; read its contract (or, for docs, spikes and decisions, the issue's stated outcome); work on a branch; open a pull request with `Closes #<n>` and the evidence, including what was not verified. A spike closes with its measured answer; a decision closes with a row in [4-choices.md](4-choices.md).

## Specs and generators

| File | Holds | Generator |
| --- | --- | --- |
| `spec/project.json` | Plugins and their options | Wiring |
| `spec/domain/<context>.json` | One bounded context: glossary, entities and invariants, statecharts, pure function signatures and rules | `domaingen`: types, transition tables, dispatchers, stubs, derived tests |
| `spec/routes.json` | Method, path, role, request and response fields with validation, the use-case function | `apigen`: handlers, validation, route table, typed structs |
| `spec/queries/*.sql` | Statements with header comments: name, cardinality (`:one`, `:many`, `:exec`), typed parameters, result | `sqlgen`: typed functions, schema check test |

Generated code goes to `gen/` with a `DO NOT EDIT` header. The mutation checker (`quality/mutate`) is our own, built on `go/ast`.

## App core and plugins

*Choices: A5.*

A client app is a **minimal core plus the plugins its project picks**. The core: the plugin registry and lifecycle, config loading and validation, the migration runner, the runtime the generated code targets.

Each plugin solves a generic capability (never one client's case), ships a manifest (name, version, purpose, options with defaults, ports needed, plugins depended on, contributed spec fragments, migrations, templates, front-end modules), and has its own tests, contract tests, README and semantic version.

- The **architect** (the reasoner, approved by the founder) picks plugins and options into `spec/project.json`; generators write the wiring; plugins are compiled in.
- Each client repo receives only its plugins; the control plane syncs updates version by version.
- A missing capability is specified as a generic plugin, built by the pipeline, and joins the catalog.

**Initial catalog** (P1):

| Plugin | Capability | Options (examples) |
| --- | --- | --- |
| `http` | Routing on `net/http` patterns, JSON, errors | Base path, error format |
| `session` | Server-side sessions, `__Host-` cookie, rotation at login | Lifetimes per role |
| `auth` | Accounts and login: password (argon2id), TOTP, roles | Which methods are on |
| `origin` | Exact-origin CORS, `http.CrossOriginProtection` plus the Origin check | Allowed origins |
| `storage` | SigV4 client for the object-storage contract | Buckets per purpose |
| `media` | Uploads through the API, derived images, private files (A18) | Formats, codec, thumbnail and derived sizes, EXIF policy, buckets |
| `mail` | Transactional e-mail through the mail port | Sender, templates |
| `bake` | Outbox, planner, renderer, publisher | What gets baked, index split size |
| `live` | Live JSON for fast-changing state | TTL, paths |
| `tenancy` | Tenant from the hostname, per-tenant database and storage | Hostname pattern, open-database cap |

## Assess

A deterministic evidence collector; interpretation comes afterwards. A Go worker drives the crawler tools inside its container (P2).

**Collects:**
- **Routes:** `robots.txt`, sitemaps, internal links, client-side navigation; templates, redirects, canonicals, broken links.
- **Interactive surface:** controls and their accessible names, modals, tabs, menus, validation messages. Forms only under explicit authorisation; never payments, orders, real accounts or irreversible actions outside a test environment.
- **Workflows** as graphs of states and actions with guards and outcomes, observed or inferred (with confidence). They become draft statecharts for a modernization.
- **Performance:** lab runs, field data where it exists, Core Web Vitals with tool versions, headers, compression, sizes; mobile and desktop.
- **Accessibility:** automated checks per page and state, plus keyboard and focus where feasible; reported as partial.

**Coverage, honestly:** never claim 100%. Each dimension shows numerator, denominator and exclusions; a page counts as tested only if more than its first load was checked.

| Dimension | Status values |
| --- | --- |
| Routes | discovered / tested / passed / failed / blocked / unverified |
| Templates | identified / sampled / validated / unverified |
| Interactive elements | discovered / exercised / passed / failed / untested |
| Workflows | mapped / tested / passed / failed / unverified |
| States | observed / reached / validated / unreachable |
| Performance (page × device) | measured / regressed / within target / unavailable |
| Accessibility | scanned / issues found / reviewed / unverified |

Every report also lists scope and exclusions, environment and tool versions, the authentication role, network failures, skipped interactions and blind spots. Records stay separate: **observation** (a tool recorded it), **finding** (interpretation backed by observations), **hypothesis** (not verified), **decision** (approved, with owner and date).

**Output contract** (a versioned Go type, decoded strictly): `schema_version`, `assessment_id`, `project_id`, `scope{…}`, `environment{…}`, `coverage{…}`, `routes`, `interactions`, `workflows`, `performance_results`, `accessibility_findings`, `technical_inventory`, `findings`, `limitations`, `recommended_tasks`, `artifact_refs`. Binary artifacts stay outside, referenced by ID.

**Safe crawling:**
- Record the authorisation before anything beyond ordinary passive access; show the approved hosts, paths, accounts and actions; enforce rate limits and budgets; keep a stop control and an audit trail of scope changes.
- **SSRF:** HTTP and HTTPS only; resolve every hostname and reject private, loopback, link-local, multicast, CGNAT and reserved ranges, metadata addresses, the private network and the control plane; re-check after each redirect, pin the resolved IP; an egress filter as second boundary; time, size, redirect and per-host limits. Its test suite comes before crawling grows.
- **Browsers:** disposable containers with storage per client; downloads and dangerous protocols off; page content is untrusted.

## Build

Two entry paths into the same pipeline:
- **Existing site:** an assessment's evidence becomes a backlog, which the reasoner turns into specs.
- **New app:** from the idea, users, journeys, roles, rules, entities, integrations and constraints, the reasoner clarifies and writes the model and tests.

Both pass the specification gate, then the founder's approval, the pipeline, a preview, and a release.

**Gates** (by project type): format and vet, regenerate-and-diff, unit tests and mutation check, integration tests, browser end-to-end tests on critical journeys, secret scanning, accessibility, performance against a baseline, migration checks with a recovery plan, security checks in scope.

**Release:** runner → preview (each client's staging, A14) → founder approval → production (pulled deploy, health checks, automatic rollback) → smoke tests and close watching. A change ships when its criteria hold, its checks pass or an exception is approved, its diff and evidence can be reviewed, its rollback is written and its approvals are recorded.

## Host

Serving, servers, backups, failover, trials and operations: [3-hosting.md](3-hosting.md).

## This repo

```text
README.md              for humans
AGENTS.md              entry point for any agent (CLAUDE.md only imports it)
agents/                one brief per agent role: what to read, role-only rules
docs/                  1-methodology … 5-roadmap
go.mod                 module github.com/briolink/briolink (E1)
cmd/control/           composition root: control plane + control center
cmd/assess/            composition root: assessment worker
spec/                  the control plane's own spec, and routing.json (Routing)
tasks/                 task contracts
port/                  one interface per role, with contract tests
adapter/<role>/<name>/ provider implementations
runner/                the loop: coder, checks, retries, escalation
generate/              domaingen, apigen, sqlgen
quality/mutate/        mutation checker
core/                  the client-app core
plugins/<name>/        the plugin catalog
web/                   control center UI
```

Each module has its own tests and README and depends on others through small interfaces, so it could become its own module later.
