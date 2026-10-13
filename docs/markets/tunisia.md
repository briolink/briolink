# Market: Tunisia (and Libya)

Tunisia is the first regional market, because the first client is there. It is a proving ground, not the main target (decision 46): the market is small and prices are low.

**Why the offer works here:**
- managed hosting is expensive (an OVH VPS costs ~1,000 TND a year);
- online payment is rare, so WordPress and PrestaShop add little;
- sites are in French and Arabic.

## Partner and entities

| Who | Role | Paid by |
| --- | --- | --- |
| Tunisian agency | Partner agency: sales team, contracts, TND invoices with VAT, delivery customisation, first-line support | Clients, in TND |
| Briolink | Platform, code, infrastructure, second-line support | Platform fee per active client from the agency, in CAD, under the platform services agreement (related parties, so a fair market fee) |
| A representative in Libya | On-site representative of the agency (below) | Commission from the agency |

How the money moves to Canada (carte technologique or bank transfer), withholding tax and VAT on imported services: see the questions for professionals below.

## Regional prices

Per year, all hypotheses. These sit far below the general USD prices: it is a purchasing-power price list, with the partner carrying sales and support.

| Tier | Price |
| --- | --- |
| Essential | ~150 TND |
| Standard | **300 TND (~$100)** |
| Business | ~600+ TND |
| Rewrites, snapshot migration | Fixed-scope packages, priced after checking local agency rates |

## Competition

| | OVH VPS | Local shared hosting | Briolink Standard |
| --- | --- | --- | --- |
| Price | ~1,000 TND/yr (VPS + backups + Plesk licence) | ~50–100 TND/yr | 300 TND/yr |
| Server administration | The client's problem | None needed / none offered | Included |
| Backups | Snapshots, untested | Varies | Continuous, restore-tested monthly |
| CDN, DDoS protection, SSL | No | Rarely | Included (edge) |
| Site up if the server fails | No | No | Yes |
| Ownership of domain and data | Client, tied to the server | Often the host | The client, in their own accounts |

Briolink competes with OVH and agency-managed hosting here, not with cheap shared hosting.

## Market size (estimates)

- **Domains:** ~60,477 `.tn` domains (registre.tn, April 2024), plus Tunisian businesses on `.com`.
- **Digital maturity:** about half of businesses are only now digitalising, and most don't take digital payments.
- **Addressable segment** (sites that outgrew cheap hosting or pay an agency): an *assumed* 10–20% of active business sites, ~6,000–12,000. Test this against the agency's experience.

| By | Clients | Hosting revenue per year |
| --- | --- | --- |
| End 2027 | ~20 | ~6,000 TND |
| End 2028 | ~150 (with the growth levers) | ~35–45k TND |
| End 2030 | ~500–1,000, many on Essential | ~100–200k TND |

Rewrites and add-ons come on top and may match or exceed hosting.

## Data protection (INPDP)

Organic Law 2004-63 applies. Article references are from a guide by Boussayen Knani & Associés; a Tunisian lawyer must confirm all of this.

| Article | Requirement |
| --- | --- |
| 7 | Declare the processing to INPDP before handling data |
| 18, 19 | All necessary security precautions, including in transit |
| 23 | Confidentiality, including after processing ends |
| 45 | Destroy data when the retention period ends |
| 50, 51 | No transfer harming public security; transfers only to countries with adequate protection |
| 52 | **INPDP authorisation for every transfer abroad**; one month to decide (budget more) |
| 90 | An unauthorised transfer: up to one year in prison and a 5,000 TND fine |

**Processing chain:** client (controller) → Tunisian agency (processor) → Briolink (subprocessor) → the compute providers, the ops host, and AI model providers when used. The client also contracts directly with its edge and offsite-backup providers. Current providers and locations: [../bindings.md](../bindings.md).

**Steps per client** (filed by the client; the agency prepares a template):
1. Declare the processing before any real personal data is collected (article 7). Previews and trials with test data come first.
2. Request the transfer authorisation (article 52). Name every destination from the current bindings (compute regions, the ops host's country, edge, offsite backup, AI model providers if used). Argue adequacy (article 51) from the GDPR and Convention 108 status of the countries involved.
3. Keep the acknowledgements in the client repo's `ops/` folder.

Clients currently on OVH need the same authorisation, since OVH's servers are in France, so this is no disadvantage against OVH.

## Libya

- **Representation:** the agency signs contracts remotely. The representative finds clients and gives on-site support for a commission, and never concludes contracts, to avoid creating a taxable presence in Libya.
- **Payment:** **bank transfer in EUR/USD** to the agency. No cash collected in Libya.
- **Sanctions:** screen every client against Canadian, UN and US sanctions lists. Canadian rules also cover indirect dealings.
- **Accounts:** clients often have no international card, which some edge storage requires. Use Briolink-managed storage or the Essential tier, and keep the edge's free plan on the client's own account.
- **Language:** templates are Arabic-first, with proper right-to-left layout.
- **Data law:** Libya has no comprehensive data-protection law as far as is known. Use the same agreements anyway, and ask the lawyer whether Tunisian law applies to the agency's processing.
- **Timing:** start after 3–4 Tunisian clients are live; meanwhile the representative tests demand informally. Create a Libyan entity only if volume justifies it.

## Questions for professionals

**Tunisian lawyer**
- [ ] Each client's INPDP declaration and transfer authorisation: what to file, and the expected timing.
- [ ] Does the agency, as a processor, have its own INPDP obligations? Does the subprocessor chain need more than back-to-back agreements?
- [ ] A reusable filing template; review of the processing agreement template ([../security-and-data.md](../security-and-data.md#data-protection)).
- [ ] The client contract: which code is client-specific and which stays reusable (core, plugins, generators, template), with a perpetual licence to them for the client.
- [ ] The agency ↔ Briolink platform services agreement (fee, scope, IP, brand licence).
- [ ] Libya: rules on representing foreign companies (historically reserved to Libyans); the representative's status; confirmation that not signing contracts avoids a taxable presence; whether Tunisian law covers the agency's processing of Libyan clients' data.

**Tunisian accountant**
- [ ] Paying Briolink: carte technologique (limits, allowed uses) or bank transfer under the agreement.
- [ ] Withholding tax on services from a non-resident; VAT due by the payer on imported services; the Canada–Tunisia tax treaty.
- [ ] Related-party pricing: a fair market fee.
- [ ] EUR/USD from Libyan clients as a service export: declaration and repatriation rules; whether the Arab Maghreb Union tax convention applies.
- [ ] VAT on hosting and rewrite invoices in TND.
