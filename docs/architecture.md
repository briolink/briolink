# Architecture: roles, adapters, headless control

**Opinionated inside, agnostic at the edges (decision 65).**
- **Inside:** our own code (Go, SQLite, vanilla JS, the generators, the core and plugins) is a fixed choice. Inside, capabilities are plugins chosen per project ([platform.md](platform.md#plugins)).
- **At the edges:** everything else (providers, machines, models, brand, markets, sectors, clients) is a **role** with a contract. A swappable **adapter** fills each role, chosen by configuration.

Nothing in the core names a provider. Today's choices are listed in [bindings.md](bindings.md).

## Rules

1. **Core code depends on ports only.**
   - A port is a small Go interface named after its role (`ObjectStore`, `Edge`, `Coder`…).
   - Adapters implement ports in `adapter/<role>/<provider>/`.
   - Only the composition root (`cmd/*`) imports adapters, and it wires them from config.
2. **Configuration, not code, picks providers.** Each installation has one operator config (`operator.json`) holding the brand, the domains, and the adapter and settings for each role. Secrets are kept separately.
3. **Headless.**
   - The control plane is an HTTP JSON API (`/api/v1`) plus an event stream (Server-Sent Events).
   - The control center UI, CI and any future client (mobile, chat) are API clients with no logic of their own.
4. **Standard protocols over vendor APIs** wherever one exists: S3 for storage, SMTP or HTTP for mail, WireGuard, OCI images, Git, SQL, HTTP. An adapter is often just config for a standard client.
5. **Variation is data, not code:**
   - markets: currency, languages, price list, legal filing templates;
   - sectors: templates;
   - clients: spec plus bindings, in their own repo;
   - brand: name, domains, look.
6. **Contract tests per port.**
   - An adapter must pass its port's contract tests before it can be bound.
   - The serving contract test that runs against both preview and trials is the first example.
7. **Replicable.**
   - An installation is the repo, an operator config, secrets and an ops host.
   - A partner, a new region or a white-label brand stands up its own installation with the same scripts.
   - It is built for one operator today, with nothing that prevents a second.
8. **Docs follow the same split:**
   - generic docs speak in roles;
   - [bindings.md](bindings.md) names the current choices;
   - [adapters/](adapters/) holds each provider's specifics.
   Docs are plain Markdown that any tool or agent can read, and `AGENTS.md` is the entry point.

## Roles

| Role (port) | Contract: what the core needs | Used by |
| --- | --- | --- |
| Edge | DNS API; static hosting with a small request function in front of object storage; a public custom domain on a bucket; an outbound-only tunnel to the origin; an access gateway for staff and previews; a bot challenge; rate limiting; an edge cache that honours origin `Cache-Control` | Client sites |
| Object storage | S3 subset: PUT with metadata, conditional GET, HEAD, DELETE, list, presigned GET and PUT | Baking, uploads, private files, trials, artifacts |
| Lease store | A conditional write (compare-and-swap) on one object | Failover |
| Offsite backup | An S3-compatible bucket in the client's own account; keys that can list and write but not delete; lifecycle expiry | Database replicas, nightly copies |
| Compute | A Linux host (arm64 or amd64) with systemd, rootless containers and cgroup v2; a provider API that can stop an instance | Client pods, trial system |
| Ops host | An always-on machine for the control plane, the warm standby, the coder model and the network hub | Operator side |
| Private network | An encrypted peer network that servers dial out to | Admin access, replicas, metrics |
| Code host | Git with protected branches; CI on tags; a container registry with build attestations and read-only pull tokens | Client repos, deploys |
| Coder model | Runs locally with no per-token cost; turns a task contract into code | Runner |
| Reasoner model | Specs, tests and escalations; structured output; cost reporting | Runner, Specifications area |
| Notifier | Push alerts, de-duplicated, with a fallback channel | Control plane |
| External pinger | A dead-man's switch outside our infrastructure | Monitoring, failover detection |
| Mail sender | Transactional e-mail with SPF and DKIM | Client apps |
| Provisioning | Declarative infrastructure with encrypted state | Client and platform resources |
| Crawler tools | Browser automation, lab performance, accessibility checks | Assessment workers |

**Part of the product, not roles:** Go, SQLite and its driver, Litestream, Podman + Quadlet, vanilla JS and esbuild, our generators, the app core and the plugin catalog.

## Headless control plane

- **API first:** every action (create a project, approve a spec, run a task, answer an escalation, trigger a re-bake) is an API call, and the UI calls the same endpoints. The API itself is built by our own generators from a route spec.
- **Events:** task attempts, check results and alerts stream as Server-Sent Events. The UI subscribes to them, and so can any other client.
- **Services behind the API:**
  - scheduler, runner and monitors;
  - the failover component, a separate process with its own credentials;
  - assessment and build workers.
- **No logic in clients:** the UI renders; it never decides.

## Replicating

| To replicate | You provide | You get |
| --- | --- | --- |
| A client | A spec (plugins + options) and the client's own accounts (edge, offsite backup, code host) | Their app from the template, on the same pipeline and hosting |
| A market | A file from [markets/_template.md](markets/_template.md) and a partner | Local prices, currency, languages, legal filing templates |
| A capability | A generic plugin, specified by the architect and built by the pipeline | A catalog entry any later project can pick |
| A provider | An adapter (code, plus a doc from [adapters/_template.md](adapters/_template.md)) that passes the role's contract tests | A swap by changing one binding |
| The whole platform | The repo, an operator config, secrets and an ops host | Another installation (partner, region, white-label) |
