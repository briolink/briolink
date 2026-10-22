# adapter/codehost/github

The GitHub binding of the [CodeHost port](../../../port/codehost), as the bot
(A22), over the REST API. `Host` is the port on one repository; `TokenSource`
gives the token every request carries.

## Host

One request per operation, plus a lookup where GitHub's answer differs from
the port's:

- **Issues and pull requests** share numbers, and GitHub's issues API serves
  both: `GetIssue`, `UpdateIssue` and the label calls read the issue first and
  call a pull request's number not found; `ListIssues` leaves pull requests out
  and sorts by number.
- **Milestones** are named by title in the port and by number on GitHub: an
  issue's milestone, and the `ListIssues` filter, list them first. An unknown
  one is `ErrInvalid` on create and an empty list on filter.
- **Labels** that do not exist are created by GitHub on use. `RemoveLabels`
  deletes only those on the issue.
- **Branches** are git refs: `CreateBranch` reads `from`'s commit and makes a
  ref there. `CreatePullRequest` checks both branches first (GitHub says 422
  for a missing one, the port says not found). GitHub refuses a pull request
  whose head has no commit its base lacks (`ErrInvalid`); the runner pushes
  before it opens one.
- **Lag:** GitHub's issue list may miss a write, or show the issue as it was, for some seconds
  (seen on the live run, 2026-10-22). `ListIssues` reads the issues this `Host`
  changed in the last minute (*estimate*) one by one and filters them itself,
  so a caller sees its own writes; others' writes show once the list has them.
- **Errors:** 404 and 410 are `ErrNotFound`; 422 is `ErrExists` when GitHub
  says so, else `ErrInvalid`; any other status is `ErrAPI` with the status and
  GitHub's message. Lists follow the `Link` header, 100 a page.

Tests run the port's contract against a fake of the API with those quirks
(newest-first lists, two items a page, the 422 shapes, pull requests needing a
commit), plus `cmd/control`'s live test against GitHub itself.

## TokenSource

`briolink-bot` is a GitHub App. `TokenSource.Token` signs a short-lived JWT
(RS256, issuer the App ID, valid nine minutes) with the App's private key and
exchanges it for an installation token (`POST /app/installations/{id}/access_tokens`),
which lasts an hour. The token is cached and replaced five minutes before it
ends; a failed request is never cached.

- **Key:** `Key` returns the PEM (PKCS#1 or PKCS#8) on every mint and nothing
  keeps it. In the control plane it is the secret store's `Use`, so each mint is
  audited.
- **IDs:** App `5307371` and installation `171151281` are not secrets; they are
  in `operator.json`'s `github` section ([cmd/control](../../../cmd/control/README.md)).
- **Errors:** `ErrKey` (missing, not PEM, not RSA) and `ErrRefused` (GitHub did
  not answer 201). No error holds the key, a JWT, a token or a response body.

Tests run against a fake GitHub that verifies the JWT's signature and claims
with the public key.
