# Adapter: GitHub (code host, CI, registry)

Current binding for the **code host** role ([bindings.md](../bindings.md)). Each client's repo lives in the client's own GitHub org.

**Depth: deep** ([architecture.md](../architecture.md#binding-depth-and-fallbacks)), by choice (decision 75): repos, reviews, issues, CI and the registry all come from GitHub. It is stable, and clients recognise the workflow. Every feature we rely on is in the table below.

| Contract item | GitHub feature |
| --- | --- |
| Protected branches | Branch protection on `main` and `production` |
| CI on tags | Actions: `.github/workflows/ci.yml` (vet, tests, JS lint, contract test), `release.yml` (on tag: tests, multi-arch image, attestation) |
| Container registry with attestations | GHCR with build attestations. A cleanup step keeps the last 10 versions, because private package storage is limited on the Free plan |
| Read-only pull token | A fine-grained token per client, packages read-only, used by the server's deploy timer |
| Build-worker access | A fine-grained token per client repo: push to task branches and open pull requests only |
| Platform home | `briolink/briolink` holds the platform core (control plane, generators, core, plugin catalog) and manages the derived projects' repos (decision 74) |
| Work tracking | Issues (one per task, linking `tasks/<id>.json`), labels for kind, risk and state (see [platform.md](../platform.md#work-tracking)), milestones per roadmap stage, sub-issues for split tasks |
| Code approvals | Required pull-request review; `CODEOWNERS` makes the founder the owner of `tasks/` and `spec/` |
| Edge deploys | The edge provider's code-host app, installed once per client org ([cloudflare.md](cloudflare.md)) |

**Provisioning (client module):**
- the repo, created from the template;
- branch protection, required reviews and `CODEOWNERS`;
- the standard labels;
- Actions variables.

## Fallback

**Prepared in advance (on the ops host):**
- a mirror of every repo, fetched every few minutes (`git clone --mirror`);
- a nightly export of every repo's issues, comments, labels and milestones as JSON, copied to Briolink's ops bucket;
- the last image of each client, already pulled for the warm standby.

**During an outage:**
- running sites are unaffected: servers only reach the registry on a deploy;
- the runner keeps working on tasks it already holds, from the mirror, and pushes branches and opens pull requests when GitHub returns;
- new issues, reviews, CI and releases wait. An urgent production fix can be built on the ops host and loaded onto the server over the private network (`podman save` / `podman load`), with the founder's approval.

**If an org or account is lost:** move to the exit below, from the mirror and the issue export.

## Spikes

- [ ] Multi-arch builds stay within the free Actions minutes and private package storage
- [ ] Fallback drill: restore one repo with its issues from the mirror and the export into a Forgejo instance on the ops host

## Swap notes

Any Git host with protected branches, required reviews, an issue tracker with an API, tag-triggered CI and an OCI registry with attestations. The deploy timer only needs a registry URL and a read-only token.
- **First exit:** Forgejo, self-hosted on the ops host or a VPS. It has issues, pull requests, CI that runs GitHub-style workflows, and an OCI registry. The issue export imports through its API.
- **Also possible:** GitLab, hosted or self-hosted.
