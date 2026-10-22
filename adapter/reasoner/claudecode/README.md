# adapter/reasoner/claudecode

The [Reasoner port](../../../port/reasoner) on headless Claude Code (E15), on
the subscription login. Deep: the flags it relies on are listed in
[docs/4-choices.md](../../../docs/4-choices.md#e14e15--models).

- **Tier:** `Models` maps `routine` and `top` to a model alias or ID; any other
  tier is `ErrUnknownTier`.
- **One call:** `claude -p` with the prompt on stdin, `--output-format json`,
  `--json-schema` when the request has a schema (else the prompt asks for one
  JSON value and the result text is parsed, a code fence unwrapped), no tools,
  no settings, no slash commands, no stored session, in an empty temporary
  directory. `ANTHROPIC_API_KEY` and `ANTHROPIC_AUTH_TOKEN` are removed from its
  environment: this binding never uses an API key. No credential is read,
  logged or passed on.
- **Cost and audit:** `CostUSD` is Claude Code's `total_cost_usd`, a list-price
  *estimate* on the flat plan; `SessionID` is its `session_id`; `Model` the
  model with the most cost in `modelUsage`.
- **Budget:** `MaxUSD` is `--max-budget-usd`, which checks only after a turn.
  `ErrBudget` when the run stops on it, or when the finished call cost more
  than the cap (its output is dropped).
- **Queue:** before a call, the cached `five_hour` and `seven_day` windows in
  `~/.claude.json` are read (only those two fields). While one is at `Limit`
  (100) percent and not yet past its reset, the call waits until the latest
  such reset, then reads them again; the context cancels the wait. Behaviour at
  a real plan limit is not measured yet ([#76](https://github.com/briolink/briolink/issues/76)).
- **Errors:** `ErrRun` (binary missing, not logged in, an error result, with
  the subtype and a clipped message), `ErrOutput` (no JSON value).

Tests run the port's contract against a fake `Run`. `TestLive` runs it against
the real binary, a few cents of the plan: `BRIOLINK_LIVE_CLAUDE=1
BRIOLINK_LIVE_CLAUDE_TIERS=routine,top go test -run TestLive ./adapter/reasoner/claudecode/`.
