---
description: Bind this session to Briolink (every prompt routed by master to a role), take one task as a given role, or unbind
argument-hint: [role|list|done|exit] [task]
---
Arguments: $ARGUMENTS

In Claude Code the hook (`scripts/briolink.sh --hook`) answers `/briolink`, `/briolink list` and `/briolink exit` itself, without a model turn, and binds the session and switches the `gh` account for every other form before this text reaches you. Without that hook (another tool), run `scripts/briolink.sh` with the arguments (`bind` for a role or task form, nothing for the board) and print its output verbatim; do nothing else for the board, `list` or `exit`.

Read `agents/master.md` if it is not already in the context, and follow its "Session and roles".
- `done`: close the piece of work. Check `git status` and unpushed commits; commit and push what belongs to the work, and write any decision or open point the conversation holds into the docs, an issue, or the roadmap. An issue left waiting on a date or an outside event gets `state:waiting` and a `Waiting until <date>: <reason>` line in its body; remove both once it no longer waits. An issue whose role is decided gets its `role:<name>` label. Rewrite the roadmap's Now section: Last (date, one line, the IDs or links touched) and Lead (one line, phrased as a suggestion; once issues exist, it links the issue). Run `scripts/session-usage.sh` last and end the closing commit's message with its output as a `Usage: ` line. Then reply with a short list of where everything was saved and what was left out, the usage line, and end with: `Saved. Run /clear to start the next piece of work fresh.` Never clear with anything unsaved.
- a role name first: take the rest as the task in that role, reading `agents/<role>.md` and only the sections it lists if not already read this session; start the reply with `Role: <role>`;
- otherwise: as master, route the words as a task.
