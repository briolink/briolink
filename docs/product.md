# Product and business

Briolink sells three things in order: an **audit** that shows the real work, a **project** that does it (modernize an existing site or build a new one), and **managed hosting** that keeps the result fast and safe. Each step qualifies the client for the next. All prices are **hypotheses to test** with real prospects (2026-10). Regional price lists live in [markets/](markets/).

## Offers

| Offer | The client gets | Price hypothesis (USD) |
| --- | --- | --- |
| Audit | Evidence-backed report: route inventory, performance and accessibility baseline, risks, coverage and its limits, prioritised backlog ([platform.md](platform.md#website-intelligence-engine)) | $150–300 |
| Snapshot migration | The existing pages turned into baked pages within hours; the full rewrite is sold later | Small fixed fee (to set) |
| Modernization project | Agreed scope, rewrite on the template in stages, tests, deployment, hand-over | $750–1,500 |
| New application | The same pipeline, starting from a specification | Quoted from the reviewed spec |
| Managed hosting and maintenance | A hosting tier (below), monitoring, continuous backups with monthly restore tests, security updates, defined support | $49–149/month |
| Agency partnership | A partner agency resells or white-labels delivery and hosting in its market | Fee per active client, or revenue share |
| Dedicated engagement | Larger modernization, integrations, dedicated hosting | $1,500–3,500+ |

### Hosting tiers

| Tier | For | Server use | Price |
| --- | --- | --- | --- |
| Essential | Static and snapshot-migrated showcase sites: baked pages, forms through a shared endpoint, no back office | None (edge only) | Entry price below the hosting range (to set) |
| Standard | Rewritten sites with a back office, forms, orders, bookings | One pod, ~60 MB (*estimate*) | Low end of the range |
| Business | Live features (bids, accounts), heavy traffic, priority support | One pod, plus more support time | High end of the range |

Every tier includes hosting, CDN, SSL, continuous backups with monthly restore tests, monitoring and security updates. Content changes and new features are billed separately.

**Other options:**
- A "rewrite included" plan: nothing upfront, one monthly price for 24 months covering the rewrite and hosting, with a minimum term.
- Add-ons: brand mailboxes, content-update packs, SEO, translation, Google Business setup, video (through an edge video service).

**Pricing rules:**
- Don't compete on hosting price. What stops prospects is the upfront rewrite and the hassle of switching. Recurring revenue pays for support and monitoring, and a cheap price makes the service look like shared hosting.
- Price each project by discovery depth, risk, support load and human review time. Track these per project to correct the hypotheses.

## Commercial sequence

1. A narrow paid audit qualifies the client and shows the real work.
2. Suitable audits turn into scoped projects.
3. Offer hosting only where responsibilities and support limits are clearly defined.
4. Start agency partnerships once delivery quality is repeatable.
5. Take dedicated engagements once delivery, security and recovery have been demonstrated.

## Who we serve

**Good fit:** organisations whose site is mostly something to show, plus forms, orders, bookings or bids. That means:
- a database up to ~10 GB;
- tolerance of ~1 s of data loss and a few minutes' outage of live features (public pages stay up);
- at most hundreds of writes a second.

| Sector | Why it fits | Watch |
| --- | --- | --- |
| Hotels, guesthouses, tour operators | Public, multilingual pages; booking requests | Real-time booking engines are integrations |
| Real estate, car rental | Listings suit baked pages and a search index | Photo volume: set a quota |
| Exporters, B2B catalogues | Fast abroad; quote requests | Translation workflow |
| Small shops (orders, cash on delivery) | Light writes; no card data on our side | Card payments only through a gateway's hosted page |
| Schools, training centres | Course pages, registrations | Minors' data |
| Clinics, practitioners | Public information, appointment requests | Appointments only, never medical records |
| Professional firms, associations | The template covers them almost as is | Price-sensitive |
| Local news, blogs | The edge absorbs traffic spikes | Needs a good back office |
| Auction houses, galleries | Baked catalogues plus live bidding | Load-test live sales |

**Decline or refer:**
- zero-downtime contracts that need several writing servers;
- very high write volume (big marketplaces, chat);
- regulated data (medical records, banking, stored card data);
- public bodies that must host in their own country;
- clients who insist on keeping WordPress or PrestaShop with their plugins;
- video-heavy sites, unless they take the video add-on.

**Qualify with three questions:** card payments? sensitive data? how much downtime is acceptable?

## Where we sell

- **Competitors:** agency-managed hosting and unmanaged VPSs, not cheap shared hosting or site builders.
- **Where hosting is commoditised (Europe, North America):** lead with the audit and the modernization. Hosting comes as part of the managed result and is never sold alone.
- **Where managed hosting is expensive and online payment is rare (for example North Africa):** hosting sells on its own, through a partner agency.
- **One market at a time.** Each market gets a file in [markets/](markets/) covering its data-protection law, currency and payment route, partner and regional prices. Current: [markets/tunisia.md](markets/tunisia.md) (Tunisia, Libya). Later candidates: Algeria, Morocco, Mauritania, French-speaking West Africa (mobile money), and selected partners in the Middle East.
- **Lead two or three sectors** rather than chasing overall market share: auctions first, then one of tourism, real estate or exporters.

## Entities and money

| Entity | Role | Paid by |
| --- | --- | --- |
| Operator: Briolink today (Canadian company, brand in [bindings.md](bindings.md)) | Platform, template, core and plugin code, infrastructure, AI pipeline, second-line support; owns the brand; sells directly in general markets (card payments, USD/CAD) | Direct clients, and partners' platform fees |
| Partner agency (one per region) | Sales, contracts and invoices in the local currency, delivery customisation, first-line support | Its own clients |

- Each partner signs a **platform services agreement**: a fee per active client (or a percentage), the scope on each side, IP, and the brand licence. Where a partner is a related party (Tunisia), the agreement also fixes a fair related-party price.
- **Replicable operator:** a partner may also run its own installation under its own brand (white-label), from the same repo, scripts and operator config ([architecture.md](architecture.md#replicating)).
- **Code ownership:** clients own their site's code and get a perpetual licence to the core and the plugins they use. Client-specific code (for example a client's auction system) belongs to that client, and reusing it needs their agreement. Briolink doesn't sell auction software.

## Cost to serve

- **Shared free-tier server:** $0 for up to ~150 clients (*estimate*).
- **Pooled VPS:** a €7–10 server with 12 GB holds ~30–40 Standard clients, well under €1 per client per month.
- **Per-client extras:** a brand mailbox (~$1–2/month); transactional e-mail on a free tier at first.
- **Models and APIs:** cost per accepted task, measured in the control center.
- **People's time** (sales, delivery, support) is the real cost.

## Financial discipline

- **Operating cost:** the starting target is ~$40–50/month. Check it against real usage: domains, monitoring, model subscriptions, API calls.
- **MRR is not income.** Model these separately: recurring and one-off revenue; infrastructure, software and model costs; payment fees, taxes, insurance and accounting; support and incident time; acquisition cost; churn and bad debt; the owner's pay.
- **Scenarios, not plan drivers:** $12k MRR, or $1M ARR within 24–30 months. No architecture or hiring decision rests on them until acquisition, retention, margin and delivery capacity have been demonstrated.

## Growth levers

Without levers, delivery caps at ~25–50 rewrites a year (2–4 weeks each, 1–2 developers). With them, ~100–150 a year (*estimate*).

| Bottleneck | Lever |
| --- | --- |
| Delivery | Fixed-scope packages on sector templates, plus a WordPress/PrestaShop importer: a rewrite in 2–3 days. Partner juniors customise templates and CSS only; Go code stays with the core team and the agent pipeline |
| Delivery | Snapshot migration: the client moves the same day, and the rewrite becomes an upsell |
| Timing | Record each prospect's hosting renewal date; cover the overlap, or start free until the old plan expires |
| Prospects | The audit engine on public pages only (ordinary passive access): find slow sites and send the owner a short report, with an opt-out. On request, rebuild their site as a trial |
| Prospects | Partner programme: freelancers, accountants and chambers of commerce bring clients for a recurring share |
| Trust | The first clients as public case studies with before/after numbers; "you own your domain, accounts and data"; ready-made data-protection filing templates |

**Order:**
1. A stable pipeline (P0–P1) and the hosting base (H1).
2. Templates, importer and trials (H2).
3. Outbound audits and partners, once 3–4 clients are live.

## Metrics

Track:
- **Sales:** lead → paid audit and audit → project conversion rates.
- **Projects:** gross margin and delivery time.
- **Hosting:** MRR, churn, support hours per client, cost per client.
- **Agents:** first-pass acceptance, retries, cost per accepted task, human review time per task.
- **Reliability:** backup freshness, restore-test results, incidents (time to detect, time to recover).

Use them to change prices, scope, models or infrastructure.

## Business risks

| Risk | Mitigation |
| --- | --- |
| Nobody pays for audits | B0 tests that with real prospects before the assessment engine (P2) grows |
| Underpriced delivery | Track hours, support, acquisition and infrastructure cost per client |
| Building too much too early | Stage gates; each stage ships something that works |
| Delivery capacity | Templates, importer, snapshot migration, agent pipeline |
| Support load on hosting | Strict hosting scope, a good back office, paid add-ons |
| Lock-in perception | Clients own the code and accounts; a documented exit |
| Free tiers change (one provider already halved its free compute) | Prices still work on paid servers (< €1 per client per month) |
| Cross-border payments and tax | Written partner agreements; accountants on both sides |
| Competitors add a CDN and backups | Speed by design, a managed service, client ownership, sector focus |

## Questions for professionals (Canada)

Market-specific questions are in each market file.

- [ ] Accountant: GST/HST on services sold to a foreign partner; declaring platform fees and currency conversion; related-party pricing with related partners.
- [ ] Sanctions screening duties for indirect dealings (clients reached through partners).
- [ ] Lawyer: the standard client contract (hosting scope, service level from [servers.md](servers.md#backups), liability cap, exit with deletion within 30 days, list of providers), the trial terms, and the platform services agreement template.
