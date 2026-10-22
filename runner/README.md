# runner

The methodology's loop as a module of the control plane: an offline
workspace per task, the coder through its port, checks, retries, escalation
to the reasoner then the founder, and the run record. It changes neither
acceptance criteria nor its own routing, and never skips a check.

See [docs/2-architecture.md § Runner](../docs/2-architecture.md#runner).

## Built (steps 1–5, up to a passing attempt or a stop)

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

Every attempt is one run record (`Records.AddRun`): the tier and the routing
row (`pure_function`) from `spec/routing.json`, the model the coder named
(the tier's model when it named none), and `passed`, `failed` or `cancelled`.

## Not built

- Opening the pull request and posting the attempt summary on a pass (step 5).
- Escalation to the reasoner and the founder (steps 6 and 7): a `Result` that
  did not pass, with its `Stop` and attempts, is their input. Attempts that
  ended a run are recorded `failed`, not `escalated`, until they exist.
- A project's own `routing` override of the tier.
- The entry point (`cmd/*`) that wires the adapters, the database and the
  contracts of `tasks/`.

## Tests

`go test ./runner/...` covers, on fakes, a pass, a retry, a write outside the
allowed files, a failed check of each kind, the early stop, a spent attempts
budget and a spent minutes budget; the container binding against a real git
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
