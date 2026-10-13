# Adapter: GitHub (code host, CI, registry)

Current binding for the **code host** role ([bindings.md](../bindings.md)). Each client's repo lives in the client's own GitHub org.

| Contract item | GitHub feature |
| --- | --- |
| Protected branches | Branch protection on `main` and `production` |
| CI on tags | Actions: `.github/workflows/ci.yml` (vet, tests, JS lint, contract test), `release.yml` (on tag: tests, multi-arch image, attestation) |
| Container registry with attestations | GHCR with build attestations. A cleanup step keeps the last 10 versions, because private package storage is limited on the Free plan |
| Read-only pull token | A fine-grained token per client, packages read-only, used by the server's deploy timer |
| Build-worker access | A fine-grained token per client repo: push to task branches and open pull requests only |
| Edge deploys | The edge provider's code-host app, installed once per client org ([cloudflare.md](cloudflare.md)) |

**Provisioning (client module):**
- the repo, created from the template;
- branch protection;
- Actions variables.

## Spikes

- [ ] Multi-arch builds stay within the free Actions minutes and private package storage

**Swap:** any Git host with protected branches, tag-triggered CI and an OCI registry with attestations. The deploy timer only needs a registry URL and a read-only token.
