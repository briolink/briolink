---
description: Bind this session to Briolink (every prompt routed by master to a role), take one task as a given role, or unbind
argument-hint: [role|list|done|exit] [task]
---
Arguments: $ARGUMENTS

Read `agents/master.md` if it is not already in the context, and follow its "Session and roles".
First, for every form below: if the active `gh` account (`gh api user --jq .login`) is not `ahmed-boussayri`, run `gh auth switch --user ahmed-boussayri` and say so in one line; if that fails, say so and stop.
- `exit`: delete `.briolink/bound` and answer as the plain model from now on, with no role line.
- `list`: list the roles from `agents/master.md` and stop.
- `done`: close the piece of work. Check `git status` and unpushed commits; commit and push what belongs to the work, and write any decision or open point the conversation holds into the docs, an issue, or the roadmap. An issue left waiting on a date or an outside event gets `state:waiting` and a `Waiting until <date>: <reason>` line in its body; remove both once it no longer waits. Rewrite the roadmap's Now section: Last (date, one line, the IDs or links touched) and Lead (one line, phrased as a suggestion; once issues exist, it links the issue). Run `scripts/session-usage.sh` last and end the closing commit's message with its output as a `Usage: ` line. Then reply with a short list of where everything was saved and what was left out, the usage line, and end with: `Saved. Run /clear to start the next piece of work fresh.` Never clear with anything unsaved.
- Anything else: first create `.briolink/bound` (an empty file; create the folder) if it is missing. Then:
  - empty: say the session is bound and show the board, then wait: never start anything on it.
    - **Last:** from the Now section of `docs/5-roadmap.md`.
    - **Next:** the Lead first, then open issues labelled `state:queued` (`gh issue list`), the current stage's milestone first, five at most. Each as `#<n> <title> → <role> (<tier>)`, the role being the one master would route it to and its tier from the routing table; add the switch command when the tier is not the session's model.
    - **Waiting:** open issues labelled `state:waiting`, each with its `Waiting until` line; flag any whose date has passed.
    - **Limits:** the output of `scripts/session-usage.sh --limits`.
    - End by asking which one to take;
  - a role name first: take the rest as the task in that role, reading `agents/<role>.md` and only the sections it lists if not already read this session; start the reply with `Role: <role>`;
  - otherwise: as master, route the words as a task.
