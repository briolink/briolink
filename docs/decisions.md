# Decisions log

In order, never renumbered (client repos cite these numbers). A later entry overrides an earlier one where they conflict. Topic files describe the current design; this file says why.

- **By:** F = the founder; D = design session with the founder; M = a proposal awaiting the founder's confirmation.
- **Dates:** 1–33 are from 2026-10-06; later dates are given in the entry.

| # | By | Decision | Why |
| --- | --- | --- | --- |
| 1 | D | One repo per client (`api/`, `app/`); vanilla JS front with no build step (build step added by 70) | No frameworks; simple, long-lived code |
| 2 | D | Public pages baked to HTML in R2; live actions are JS modules calling the API | Pages load from the edge; sites survive server failure |
| 3 | D | One set of accounts per client (domain, Cloudflare, B2, GitHub owned by the client); Briolink owns the compute | Clients can leave with everything |
| 4 | D | Clean URLs through a Pages Function with an R2 binding | One place for `/`, trailing slashes, 404s, the 301 map and preview noindex |
| 5 | D | HTML never edge-cached; media content-addressed and immutable; live JSON with a 2 s TTL | No purge step that could be forgotten |
| 6 | D | Cross-origin, same-site API: exact allowed origins, `__Host-` cookie, Origin check on writes, JSON-only bodies | No shared-domain cookie, no proxy |
| 7 | D | OpenTofu is the only infrastructure writer; monitoring keys are read-only | One writer means no drift; write keys stay off always-on machines |
| 8 | D | Client = controller, Briolink = processor; data-protection filings start early (Tunisia's INPDP from 2026-10) | Filings are slow |
| 9 | D | Oracle upgraded to Pay As You Go | Still $0 within free limits; fewer capacity errors; support tickets |
| 10 | D | Dropped: P2P or decentralised compute paid in tokens | Only small jobs could move out; no gain |
| 11 | D | Build the first client by hand first, generalise after its launch | Generalise from something that works |
| 12 | D | Develop in WSL2 on the tower; WSL is acceptable for the standby | Already set up that way |
| 13 | D | Docker Compose for development; rootless Podman + Quadlet in production | Same image everywhere; no root daemon |
| 14 | D | Trials on a separate preview domain, with one-level hostnames | Keeps Safe Browsing risk away from the brand; the free certificate covers one level |
| 15 | D | Trials are one multi-tenant system, created from a form; nothing per trial at Cloudflare | Near-zero cost per trial |
| 16 | D | Tenant isolation enforced by storage, not table prefixes | Prefixes are naming, not security |
| 17 | D | Garage for trial storage; one S3 client for R2 and Garage | A trial becomes production by changing config only |
| 18 | D | Trial quota 100 MB, reduced image profile, separate disk volume | A full trial pool can't fill production's disk |
| 19 | D | Hosting tiers: shared Oracle → pooled VPS → dedicated VPS in the client's account | Free first, then cheap European VPSs |
| 20 | F | Accept the Oracle terms risk (client hosting is a grey zone); rely on the tower takeover + a VPS within 24 h | Termination is survivable |
| 21 | D | Go + SQLite + Litestream instead of PHP + MariaDB (made final by 39 and 41) | Far less RAM; simpler backups and failover |
| 22 | D | Images processed in the browser, uploaded straight to R2 (amended by 42) | — |
| 23 | D | One pod per client; previews on demand; one shared Litestream daemon; Go memory limits; trials sized for ~150 | ~150 clients on the Oracle VM (*estimate*) |
| 24 | D | Ops dashboard on the tower, not an Oracle micro VM (now inside the control center, 47) | Frees ~47 GB; survives an Oracle termination |
| 25 | D | Failover: Litestream replica on the tower, pods pre-created there, lease with self-stop, DNS repointed through the API | ~2–5 min outage, ~1 s data loss |
| 26 | D | Don't cut the hosting price (generalised by 46) | The barrier is the rewrite, not the hosting price |
| 27 | D | Commercial tiers Essential / Standard / Business, add-ons, snapshot migration, "rewrite included" plan | Volume without server capacity; lower entry barrier |
| 28 | D | Target WordPress/PrestaShop sites with a rewrite on the template and a speed comparison | Baked pages are faster |
| 29 | D | Don't sell auction software; it is a client's product | Small market; different business; the client owns that code |
| 30 | D | Entities: Canadian company (platform) + Tunisian agency (sales, TND invoices), with a written agreement (generalised by 46) | Local payment and trust; protects both sides |
| 31 | D | Libya through the Tunisian agency, with a local representative ([markets/tunisia.md](markets/tunisia.md#libya)) | Test the market cheaply |
| 32 | D | Brand: Briolink (briolink.ca) | `.ca` suits the Canadian company |
| 33 | D | Client-specific (now in that client's repo) | — |
| 34 | D | Two-week Go pilot on a client project before choosing Go or PHP (done: 39) | A port translates a known spec |
| 35 | F | Money stored and computed as integers in the minor unit (millimes for TND); percentages in basis points | Floats can't represent money exactly |
| 36 | F | Buy orders with equal maximums: the earliest wins | The usual auction rule |
| 37 | F | Cookies `SameSite=Strict` (Secure + `__Host-` in production), never `None` | Front and API are always same-site; `None` weakens CSRF protection |
| 38 | D | AI roles: Claude writes tickets and tests, Qwen implements (refined by 55) | — |
| 39 | D | 2026-10-07 · Go pilot result: GO | 6 routes, 5 domain functions and the kernel in ~3 h; 75 + 60 tests green; 32/32 mutation guards caught; 12.5 MB idle. Qwen passed the pure functions and failed on glue code (~30 attempts) and on edge rules the tests didn't pin down. Report in that client's repo |
| 40 | F | 2026-10-07 · `cpu-keeper` on the Oracle VM from day one, phased out as real load grows ([operations.md](adapters/oracle.md#idle-reclaim-line)) | Keeps the scarce A1 capacity from idle reclaim |
| 41 | F | 2026-10-08 · On a client project, Sonnet wrote all the code and Claude (Opus) validated (superseded for the platform by 55) | Sonnet passed 24/24 benchmark runs, but at a cost the founder wants to cut |
| 42 | F | 2026-10-09 · Images stored as WebP; the browser encodes when it can, otherwise the API converts (q75); uploads go through the API; master + 300 px thumbnail; no WASM encoder, no AVIF for now (amends 22) | One upload path; the API needs the pixels anyway; EXIF stripped from public photos |
| 43 | F | 2026-10-09 · Private files (ID scans, user-submitted photos, mandates, invoices, statements) in a private bucket, served through session-checked 5-minute presigned URLs | Public upload URLs leak through e-mails, logs and forwards |
| 44 | F | 2026-10-13 · **WireGuard replaces Tailscale.** Hub on the tower; servers dial out and keep zero inbound ports ([operations.md](operations.md#private-network)) | No third-party coordination service or account; an open standard; fewer moving parts |
| 45 | F | 2026-10-13 · Briolink is one pipeline with three offers (assess → build → host). The blueprint sets the product frame; the hosting design is its "host" stage | Audits and agent delivery feed hosting; hosting alone is commoditised in large markets |
| 46 | F | 2026-10-13 · General market first: offers priced in USD (hypotheses); regional markets through partner agencies with their own price lists; Tunisia is the first regional market | The Tunisian market alone is a weak target |
| 47 | F | 2026-10-13 · The ops dashboard becomes the control center's Infrastructure area: one Go app on the tower, WireGuard-only, never holding production write keys; production releases stay a founder action | One operator interface; the same security boundary |
| 48 | F | 2026-10-13 · Docs: an agent entry file plus topic files (entry file refined by 66); the blueprint is absorbed; `docs/` is the Obsidian vault (no separate vault) | Agents load only what they need; one source of truth |
| 49 | F | 2026-10-13 · Priority: first client + audit engine in parallel (superseded by 59) | — |
| 50 | F | 2026-10-13 · Projects that are not Briolink work are removed from this repo | Not Briolink work |
| 51 | F | 2026-10-13 · The preview environment is the staging stage; there is no third environment | Preview is already a full copy of production with its own data |
| 52 | F | 2026-10-13 · Litestream only; LiteFS only for a measured multi-node need | One writer per database is enough |
| 53 | F | 2026-10-13 · Control center at `ops.<brand domain>`: a DNS-only record to the WireGuard address, with a certificate through ACME DNS-01 | HTTPS on a real name; nothing listens publicly |
| 54 | F | 2026-10-13 · **No third-party libraries in our Go code:** standard library plus our own modular packages, publishable later. The one exception is the SQLite driver, `modernc.org/sqlite`. Tools we run (Podman, cloudflared, Litestream, OpenTofu, Ollama, Playwright, Lighthouse, axe-core) are not dependencies. Front end unchanged (1) | No existing library covers the full spec; own code stays small, auditable and offline-buildable |
| 55 | F | 2026-10-13 · **Code roles:** Claude (Opus) writes specs, tests and generator templates; our generators write the plumbing; Qwen writes the pure logic; no Sonnet or Haiku code | Cuts cost. Qwen did well on pure logic and failed only on glue, which can be generated deterministically (39) |
| 56 | F | 2026-10-13 · Escalation: Qwen within its retry budget → Claude → founder | Claude fixes tests or specs, splits the task, or writes it; the founder only sees what Claude can't solve |
| 57 | F | 2026-10-13 · The runner is a module of one centralised control plane, operated from the browser. No CLI tool (Aider…) and no separate orchestrator (Hermes, Armature) in the workflow | One place to see and control everything; no added tool that adds little |
| 58 | F | 2026-10-13 · The control plane reaches Claude through headless Claude Code (`claude -p`, JSON output, restricted tools, budget cap) under the founder's subscription; an API key only if Anthropic's terms rule that out (to check) | Reuses the plan already paid for |
| 59 | F | 2026-10-13 · Pipeline first: P0–P1 pass before any client work goes through the platform (supersedes 49) | A stable pipeline before production use |
| 60 | F | 2026-10-13 · Claude bootstraps P0 (control plane skeleton, runner, generators, spec format, mutation checker) once; from then the pipeline builds the rest of itself | The pipeline can't build itself before it exists; its first job tests it |
| 61 | F | 2026-10-13 · Client-specific material lives only in each client's repo; examples here use `acme` | One repo per concern; this repo stays the platform |
| 62 | F | 2026-10-13 · Contracts (task, assessment) are stored in compact JSON and checked by our own code: strict decoding into Go structs (unknown fields rejected) plus a `Validate()` method for rules, instead of a separate JSON Schema document and a validator library | Same JSON on disk; one place for the rules, in code the tests cover; no extra library |
| 63 | F | 2026-10-13 · Control center secrets use AES-256-GCM (standard library) instead of libsodium XChaCha20; login is password + TOTP, passkeys later (O10) | Equally secure for this volume; standard library preferred |
| 64 | F | 2026-10-13 · **Libraries: standard library first; a third-party library only when needed and strong.** Needed: the standard library lacks it and our own version would be large or security-critical. Strong: widely used, reputable maintainer, stable for years, few dependencies, permissive licence. Vendored; each one recorded here. Admitted: `modernc.org/sqlite`, `golang.org/x/crypto` (refines 54) | Use libraries wisely rather than never |
| 65 | F | 2026-10-13 · **Opinionated inside, agnostic at the edges:** every provider, machine, model, brand, market and client is a role (port) with swappable adapters chosen by config; the control plane is headless (API + events, UI as one client); variation is data; installations are replicable; docs follow the same split ([architecture.md](architecture.md)) | Replicable and generalised from day one; no hard coupling |
| 66 | F | 2026-10-13 · Doc mechanics for 65: `AGENTS.md` is the tool-agnostic entry point (`CLAUDE.md` only imports it); generic docs speak in roles; current choices only in `bindings.md`; provider details and provider spikes in `adapters/`; templates for adapters and markets | Swapping a provider touches one adapter doc and one row |
| 67 | F | 2026-10-13 · Passwords hashed with argon2id from `golang.org/x/crypto` | The security community's first choice; maintained by the Go team |
| 68 | F | 2026-10-13 · Specs and prompts use compact JSON only; no TOON | Every model writes JSON reliably; a young format adds complication for little gain |
| 69 | F | 2026-10-13 · **Everything is a plugin.** Client apps are a minimal core plus plugins chosen per project by the architect (reasoner model, founder-approved) in `spec/project.json`, with options; nothing forced on a client. Each plugin is a generic capability with a manifest, tests and a version; missing plugins are specified generically, built by the pipeline and join the catalog. The first plugins come from the first client's requirements, abstracted. Image formats, conversion and thumbnails become `media` plugin options (resolves O9; 42 becomes an example option set) | Replicable, generic, decoupled from any one client |
| 70 | F | 2026-10-13 · **Front-end build step with esbuild:** vanilla JS ES modules stay the source, with no frameworks; esbuild tree-shakes and minifies them at build. Built modules keep stable names (no hashes). esbuild is a tool we run, not a dependency (54) (amends 1) | Smaller assets for visitors; one fast tool with no dependencies of its own |
| 71 | F | 2026-10-13 · The reasoner runs on the founder's **Pro subscription, never an API key**: headless Claude Code draws from the plan's limits; reasoner work queues when the limit is reached; if unattended use is blocked or the login expires, jobs become hand-off packages the founder runs through the editor extension (refines 58) | Use the plan already paid for; Pro has no API credits |
| 72 | F | 2026-10-13 · **Evidence is internal workflow data**, never client personal data; it never expires on its own and is deleted only by the founder from the control plane, with an audit entry. Client data deletion duties raise a request in the Approvals inbox, confirmed by the founder (resolves O8) | Nothing is ever deleted silently; legal deadlines still can't be missed |
| 73 | F | 2026-10-13 · Go pinned in `go.mod` (`go 1.27.0`, `toolchain go1.27.2`), so every machine and cloud session fetches the right toolchain by itself; the runner's build workers use a Go container image pinned by digest | Zero setup per session; cloud sessions run no container daemon |
| 74 | F | 2026-10-13 · The platform's home is the `briolink/briolink` repo (module `github.com/briolink/briolink`). It holds the core of the project and manages the derived projects' repos; it moves there before any code lands | The module path ends up in every import |
| 75 | F | 2026-10-13 · **Work is tracked on the code host.** Each repo's issues, labels, milestones and pull-request reviews are the work queue, the task view and the code approvals; escalations to the founder are issues assigned to the founder. The task contract is a JSON file in the repo (`tasks/<id>.json`), added through a reviewed pull request and owned by the founder (code owners), with one issue per task linking it; the runner reads only the file, never issue text. Task state lives in the issue and the control plane, not in the file. Run records (attempts, tokens, cost) stay in the control plane, which posts a short summary on the issue. The roadmap's checklist, spikes and open decisions move to issues; stages and exit criteria stay in `roadmap.md`. The control center UI moves to P3 (refines 57, 62) | A deep but stable binding, like the edge: a familiar workflow for the founder and clients, no P0 UI to build, approvals enforced by protected branches, and clients keep their ticket history when they leave (3) |
