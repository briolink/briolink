---
description: Bind this session to Briolink (every prompt routed by master to a role), take one task as a given role, or unbind
argument-hint: [role|list|exit] [task]
---
Arguments: $ARGUMENTS

Follow "Session and roles" in AGENTS.md.
- `exit`: delete `.briolink/bound` and answer as the plain model from now on, with no role line.
- `list`: list the roles from AGENTS.md and stop.
- Anything else: first create `.briolink/bound` (an empty file; create the folder) if it is missing. Then:
  - empty: say the session is bound and wait for a task;
  - a role name first: take the rest as the task in that role, reading `agents/<role>.md` and only the sections it lists if not already read this session; start the reply with `Role: <role>`;
  - otherwise: as master, route the words as a task.
