# adapter/codehost/github

The GitHub binding of the [CodeHost port](../../../port/codehost), as the bot
(A22). So far only its authentication, `TokenSource`; the port's operations
(#59) build on it.

`briolink-bot` is a GitHub App. `TokenSource.Token` signs a short-lived JWT
(RS256, issuer the App ID, valid nine minutes) with the App's private key and
exchanges it for an installation token (`POST /app/installations/{id}/access_tokens`),
which lasts an hour. The token is cached and replaced five minutes before it
ends; a failed request is never cached.

- **Key:** `Key` returns the PEM (PKCS#1 or PKCS#8) on every mint and nothing
  keeps it. In the control plane it is the secret store's `Use`, so each mint is
  audited.
- **IDs:** App `5307371` and installation `171151281` are not secrets; they move
  into `operator.json` when it names them.
- **Errors:** `ErrKey` (missing, not PEM, not RSA) and `ErrRefused` (GitHub did
  not answer 201). No error holds the key, a JWT, a token or a response body.

Tests run against a fake GitHub that verifies the JWT's signature and claims
with the public key. Not tested: the real API.
