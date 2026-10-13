# Security and data

Covers the platform (audits, agents) and the hosting. Server isolation is in [servers.md](servers.md#isolation); control center security is in [operations.md](operations.md#security-model). None of this is legal advice: a lawyer in each market reviews the data-protection parts.

## Principles

- **Least privilege:** each worker gets only the credentials, network access, files and tools its task needs.
- **Clients are separate security boundaries:** each client's data, credentials, artifacts and environments.
- **Read-only by default.** Changing state needs an explicit policy.
- **Reproducible:** tool versions, configs and artifacts are kept with each result.

## Safe assessment

**Authorisation:**
- Record an authorisation before crawling or testing beyond ordinary passive access.
- Show the operator the approved hostnames, paths, accounts and actions.
- Enforce rate limits and crawl budgets, and provide a stop control.
- Keep an audit trail of every scope change.

**SSRF.** The worker fetches URLs that users supply, so it must:
- allow only HTTP and HTTPS;
- resolve every hostname and reject private, loopback, link-local, multicast, CGNAT and other reserved ranges, cloud metadata addresses, the WireGuard subnet and control-plane addresses;
- re-check after every redirect and DNS change, and pin the resolved IP for the connection;
- sit behind a container egress filter as a second boundary;
- apply timeouts, response-size limits, redirect limits and per-host budgets;
- keep internal addresses and credentials out of reports.

The test suite for these controls is written before crawling grows (P2).

**Browser isolation:**
- Disposable containers, with separate storage state per client. Never reuse cookies or profiles across clients.
- Downloads and dangerous protocols are off.
- Page content is untrusted. Prompt-injection text in a page never changes policies, task contracts or approvals.

**State-changing actions:**
- Read-only by default. Anything that creates, changes, publishes, buys or deletes needs an explicit policy.
- Prefer a staging environment with seeded test data.
- Production interactions need specific authorisation and a remediation plan.

## Credentials and artifacts

- **Secrets** come from a secret store or per-environment injection (Podman secrets on servers, the encrypted store in the control center). Never from Markdown, Git or prompts.
- **Test accounts:** dedicated, with the least privilege possible.
- **Redaction:** strip cookies, authorisation headers, tokens, personal data and sensitive query parameters from logs and artifacts.
- **Evidence is internal workflow data** (diffs, logs, test output, screenshots, traces, reports): it never holds client personal data, never expires on its own, and is deleted only by the founder from the control plane, with each deletion in the audit log (decision 72).
- **Client data deletion duties** (a client leaves, or a retention period ends) are never silent either: the control plane raises a deletion request in the Approvals inbox before the deadline, the founder confirms it, and the audit log records it.

## AI models and client data

- Before client code or evidence goes to an external model, the client agreement says so, and the model provider is listed as a subprocessor.
- Client personal data and production data never go into prompts unless the client has authorised it. Use seeded or test data instead.
- If a client forbids external models, its tasks go to the local model only.
- Prompts, spec versions and outputs are logged without secrets.

## Data protection

**Roles:**
- The client is the **controller** of its end users' data.
- Briolink is the **processor**. Where a partner agency holds the contract, the agency is the processor and Briolink is a **subprocessor**.
- Briolink's providers are subprocessors: compute providers, the ops host's location, and AI model providers when used.
- The client contracts directly with its edge and offsite-backup providers, in its own accounts.
- The current providers and their locations are listed in [bindings.md](bindings.md) (the ops host's location is in the operator config); filings and agreements name them from there.

| Data | Location | Contract holder |
| --- | --- | --- |
| End users' identity, contact details, orders, bids; staff accounts | SQLite on the compute servers | Briolink |
| Live copies of the same | The ops host, encrypted disk | Briolink |
| Continuous replicas | The client's offsite backup | The client |
| Edge requests, logs, access-gateway identities | The client's edge provider | The client |
| Public pages, photos, private files | The client's object storage | The client |

**Per market:**
- Before any real personal data is collected, check the client's local law for filing and cross-border transfer rules: for example GDPR for EU clients, PIPEDA (plus Quebec's Law 25 for Quebec clients), or Tunisia's INPDP ([markets/tunisia.md](markets/tunisia.md#data-protection-inpdp)).
- Previews and trials with test data come first, before any filing is needed.
- Transfer filings name every destination listed above.
- A ready-made filing template per market is a selling point.

**The processing agreement must contain:**
- the subject, duration, purpose, data categories and data subjects;
- processing only on the client's documented instructions;
- confidentiality that continues after the contract ends;
- a security measures annex, built from [servers.md](servers.md) and [operations.md](operations.md);
- the subprocessors and their locations, with changes announced in advance and a right to object;
- breach notice to the client within an agreed deadline (for example 48 h), and help with data subjects' requests;
- retention and deletion per data category (accounting records follow their own legal period);
- the exit: the client receives a final database export, the image masters and any decryption keys; Briolink's copies are deleted within 30 days; the repo and runbook let the client run everything on its own server;
- the service level from [servers.md](servers.md#backups);
- a liability cap, and a yearly written report (an export from the control center).

**Special cases:**
- **ID document scans:** avoid storing them unless the law requires it. If it does, use the private bucket ([hosting.md](hosting.md#images-and-files)) with its own retention rule.
- **Minors (schools):** careful wording and parental consent.
- **Health (clinics):** appointment requests only, never medical records, which need certified hosting.
- **Trials:** test data only, never real end users.
- **Client privacy policies** must disclose the processors and their locations to the client's own end users. Consent in terms of service doesn't replace a required filing or authorisation.
