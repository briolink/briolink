# runner

The methodology's loop as a module of the control plane: an offline
workspace per task, the coder through its port, checks, retries, escalation
to the reasoner then the founder, the pull request and the run record. It changes neither
acceptance criteria nor its own routing, and never skips a check.

See [docs/2-architecture.md § Runner](../docs/2-architecture.md#runner).

## Built (steps 1–7)

`Runner.Run(ctx, issue, contract)` works one task contract:

1. **Workspace.** `Workspaces.Create` gives a git worktree. `Container` is the
   binding: `git worktree add`, files read and written on the host through
   `os.Root` (a path cannot leave the worktree), every command run by
   `docker run --network none` in the build image of
   [container/](container/README.md), with `GOPROXY=off`.
2. **Coder.** One request, parts in this order: the contract, the files it may
   write as they stand (signature and documentation), the package's other `.go`
   files, its `_test.go` files, and, on a retry, the last failure (check kind and output,
   capped at `MaxFailure` bytes).
3. **Apply.** A final newline is added to a file that lacks one (a model's JSON
   string drops it); nothing else is changed. An answer naming any path outside `allowed_files` is rejected
   whole (check kind `scope`) and nothing is written. Before each attempt the
   allowed files go back to their state at the start.
4. **Checks**, in order, stopping at the first failure: `gofmt -d` and `go vet`
   on the allowed files' packages, regenerate-and-diff (`generate -check`),
   then the contract's `verification_commands` (`go test`, `mutate`). The
   build image needs `generate` and `mutate` on its `PATH` (`Container.Tools`
   mounts them).
5. **Stop.** On a pass, `Result.Passed`. Otherwise it retries with the failure
   until `coder_retries` (`StopAttempts`) or `minutes` (`StopMinutes`) is spent,
   or two consecutive attempts fail the same check with the same output,
   durations ignored (`StopSame`).

6. **Escalation** (a `Reasoner` is set). A run the coder stopped on goes to the
   reasoner at the tiers `escalation.first`, then `escalation.second` give,
   each within what is left of the contract's `reasoner_usd` (`MaxUSD`; none left
   means no call). Its input: the coder's input, the model slice (the files of
   `spec_refs`), the last attempt and its trimmed failure, and the failing test
   names of the earlier attempts. It answers `{diagnosis, files}`; the files go
   through the same scope rule and the same checks as the coder's. A call that
   errors is a failed attempt of kind `reasoner`; so is a malformed or empty
   answer. The coder's last attempt and every failed reasoner attempt are
   recorded `escalated`; a reasoner pass is `passed`.
7. **Outcome** (a `Host` is set). On a pass the workspace (a `Pusher`) commits
   the passing attempt's files and pushes branch `task/<issue>-<task_id>`,
   replacing one a rerun left; a pull request into `Base` (`main`) closes the
   issue. Otherwise the issue gets `state:needs-founder` (`state:queued` and
   `state:running` come off) with the reason and, when `History` is set, a link
   to the full history. Both post the attempt summary on the issue: each
   attempt's tier, model and failed check, the reasoner's one-line diagnosis,
   no cost figures. Without a reasoner the founder follows the coder directly.

Every attempt is one run record (`Records.AddRun`): the tier and the routing
row (`pure_function`, or the escalation row) from `spec/routing.json`, the model
that answered (the tier's model when it named none), and `passed`, `failed`,
`escalated` or `cancelled`.

`Container` pushes from the host, not the container (the one step with a
network), to `Remote` (`origin`) as `GitName <GitEmail>`; the credential is the
host's git configuration.

The minutes budget bounds the coder's loop; escalation runs on the caller's
context.

## Not built

- The founder is not *assigned* (the code host port has no assignee), only
  labelled.
- A project's own `routing` override of the tier ([#107](https://github.com/briolink/briolink/issues/107)).
- The entry point (`cmd/*`) that wires the adapters, the database and the
  contracts of `tasks/` ([#106](https://github.com/briolink/briolink/issues/106)).
- A real run: no `acme` task has opened its pull request through the runner yet.

## Tests

`go test ./runner/...` covers, on fakes, a pass, a retry, a write outside the
allowed files, a failed check of each kind, the early stop, a spent attempts
budget and a spent minutes budget; escalation to a first answer, a second answer,
a hand-off to the founder and a spent `reasoner_usd`, with the pull request and
the issue summary on the fake code host; a push of the real `git` to a bare remote; the container binding against a real git
repository; and `TestAcmeChecks`, the real checks (`gofmt`, `go vet`,
`generate -check`, the derived tests, `mutate`) on the acme fixture's
`SettlesBalance` with a scripted coder. `TestLive` passes the same function
through a real coder: `BRIOLINK_LIVE_OLLAMA=<model> go test -run TestLive
-v ./runner/`.

**Live result (2026-10-21):** `qwen3-coder:30b` through Ollama passed `SettlesBalance` in 4 of 4 runs, each
on the first attempt, all five checks, about 7 s. Before the package's other files and
the final newline were added, the same model failed four attempts (gofmt for a
missing final newline, then `invoice.Total` undefined: it could not see the
helpers); `qwen2.5:1.5b` fails (spaces for tabs, no `amount > 0`). Four runs per
model, so not a solve rate. The commands ran on the host, not in the build image.
