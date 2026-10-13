---
description: Bind this session to Briolink (every prompt routed by master to a role), take one task as a given role, or unbind
argument-hint: [role|list|done|exit] [task]
---
Arguments: $ARGUMENTS

Follow "Session and roles" in AGENTS.md.
- `exit`: delete `.briolink/bound` and answer as the plain model from now on, with no role line.
- `list`: list the roles from AGENTS.md and stop.
- `done`: close the piece of work. Check `git status` and unpushed commits; commit and push what belongs to the work, and write any decision or open point the conversation holds into the docs, an issue, or the roadmap. Rewrite the roadmap's Now section: Last (date, one line, the IDs or links touched) and Next (one line); once issues exist, Next links the issue. Then reply with a short list of where everything was saved and what was left out, and end with: `Saved. Run /clear to start the next piece of work fresh.` Never clear with anything unsaved.
- Anything else: first create `.briolink/bound` (an empty file; create the folder) if it is missing. Then:
  - empty: say the session is bound, read the Now section of `docs/5-roadmap.md`, and propose its Next as the task;
  - a role name first: take the rest as the task in that role, reading `agents/<role>.md` and only the sections it lists if not already read this session; start the reply with `Role: <role>`;
  - otherwise: as master, route the words as a task.
