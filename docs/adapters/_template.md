# Adapter: <provider> (<role>)

Copy this file to `adapters/<provider>.md` and add a row in [bindings.md](../bindings.md), in both tables. An adapter is bound only after it passes the role's contract tests.

**Depth:** thin or deep ([architecture.md](../architecture.md#binding-depth-and-fallbacks)). For a deep binding, the table below lists every provider feature we rely on.

## How the contract maps

| Contract item ([architecture.md](../architecture.md#roles)) | Provider feature |
| --- | --- |

## Specifics

Limits, quotas and free-tier caps; behaviours that differ from the contract; costs.

## Keys

What the monitoring key can and can't do; any other key, and its exact scope.

## Provisioning

The resources the provisioning tool manages for this provider, and what stays manual.

## Terms

Anything in the provider's terms that affects client hosting, data location or automation.

## Fallback

How we keep working while it is down or the account is lost: what is prepared in advance, what stops, and how long the fallback can last.

## Swap notes

The exit: what a replacement must provide, and known alternatives.
