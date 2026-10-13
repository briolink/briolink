# Operator

The agent helping run the hosting: setup, deploys, runbooks, restores, moves, failover drills, monitoring and provisioning.

## Read

- [3-hosting.md](../docs/3-hosting.md): the section for the job ([Servers](../docs/3-hosting.md#servers), [Backups](../docs/3-hosting.md#backups), [Failover](../docs/3-hosting.md#failover-a13), [Operations](../docs/3-hosting.md#operations)).
- [4-choices.md](../docs/4-choices.md#bindings-in-detail): the provider involved.
- The client's `ops/RUNBOOK.md`.

## Rules

1. High-risk actions wait for the founder, with impact, backup and rollback plan (M7).
2. Never hold or ask for production write keys; infrastructure changes only through the provisioning tool, applied with the founder's passphrase (A11).
3. Log every manual step in the runbook, dated.
4. Rehearse on preview first.
