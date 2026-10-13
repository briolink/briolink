# Adapter: Cloudflare (edge, object storage, lease store)

Current binding for the **edge**, **object storage** (R2) and **lease store** roles ([bindings.md](../bindings.md)). Each client uses its own account on the Free plan; Briolink's platform account holds the lease.

## How the edge contract maps

| Contract item ([architecture.md](../architecture.md#roles)) | Cloudflare feature |
| --- | --- |
| DNS API | Cloudflare DNS (proxied records) |
| Static hosting + request function | Pages project + one Pages Function (`functions/[[path]].js`) with an R2 binding |
| Public bucket domain | R2 custom domain on the media bucket |
| Outbound-only tunnel | Cloudflare Tunnel (cloudflared), config `config_src = "local"` |
| Access gateway | Cloudflare Access (staff e-mails, one-time PIN) |
| Bot challenge | Turnstile |
| Rate limiting | One rate-limiting rule on the Free plan (on `/v1/auth/*`) |
| Edge cache honouring origin TTL | Cache Rules: images eligible, and `/live/*.json` needs a rule because `.json` isn't cached by default |
| Lease (compare-and-swap) | R2 conditional write on one object |

## Specifics

- **Free certificate** covers one label, hence one-level hostnames.
- **`*.pages.dev` is public by default:** the Function redirects it (prod) or refuses it (preview), and Access is enabled on preview deployments.
- **Function:**
  - `R2.get(key, { onlyIf: request.headers })` handles conditional reads;
  - `redirects.json` is kept in memory per isolate;
  - `_routes.json` excludes `/assets/*`, so assets never run the Function.
- **Request quota:** each HTML view is one Workers request, out of 100,000 free per day **per account**. The control plane alerts at 60,000; the fix is Workers Paid ($5/month) on the client's account.
- **R2 free tier:** 10 GB, 1M Class A and 10M Class B operations per month. At 100,000 page views a day, reads come to ~3M a month. R2 needs a card on file.
- **Tunnel:**
  - the ingress is defined on the server, so a compromised account can't redirect it;
  - moving a tunnel to another server keeps its ID, so no DNS change is needed;
  - failover repoints the API hostname to the ops host's own tunnel through the API.
- **Deploys:** Pages deploys from the client's repo through Cloudflare's code-host app, installed once per client org.
- **Live updates beyond polling:** a Durable Object with WebSockets on the client's account.
- **Data location:** R2 EU-jurisdiction buckets are possible (spike).

**Preview behind Access.** A cross-origin fetch can't follow Access's login redirect (to verify in a spike):
1. Put both hostnames in one Access application, with allowed origin `preview.<domain>` and credentials allowed.
2. In preview only (`ACCESS_BOUNCE=1`), `boot.js` calls `/v1/health`. If that fails, it navigates the top window to `preview-api.<domain>/_access?return=<url>`.
3. Access signs the tester in, setting its cookie on the API host, and the API redirects back.

## Monitoring key scope (per client)

- **Can:** read analytics, zone, tunnel and Pages status. Usage comes from GraphQL analytics: R2 storage and operations, Function requests.
- **Cannot:** change DNS, rules, buckets, tunnels or Access.

## Provisioning resources (client module)

- **Zone:** DNS and zone settings (SSL Full strict, HSTS, AI-crawler block); redirect, cache and rate-limit rules.
- **Pages:** projects, custom domains, env vars, R2 bindings.
- **R2:** buckets (site, media, originals, private), the media custom domain, location hint `weur`.
- **Tunnels:** the server tunnel and the ops-host tunnel, plus their DNS records.
- **Access:** applications and policies (preview, admin).

## Spikes

- [ ] `R2.get(key, { onlyIf: request.headers })` returns correct 304s
- [ ] Pages Functions on Free: what visitors see after 100,000 requests in a day
- [ ] A second Pages project takes `preview.` as a custom domain; Access on preview deployments covers `*.pages.dev`
- [ ] Preview Access redirect workaround with credentials
- [ ] A Cache Rule makes the edge honour `max-age=2` on `/live/*.json`
- [ ] R2 EU-jurisdiction buckets work with a custom domain and Pages bindings
- [ ] Garage on arm64 behaves like R2 for the `s3` module's calls; bucket quotas hold (trial store)

## Swap notes

Any replacement must provide the whole edge contract and pass the serving contract test. Options to evaluate if needed:
- another CDN with edge functions and S3-compatible storage;
- a small Go request server behind a CDN, which also removes the per-request quota.
