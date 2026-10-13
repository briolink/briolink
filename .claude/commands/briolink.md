---
description: Switch this session to a Briolink agent role (builder, architect, escalation, assessor, operator), or back to master
argument-hint: <role|master|list> [task]
---
Arguments: $ARGUMENTS

Follow "Session and roles" in AGENTS.md:
- If the first argument is `master`, drop the current role and act as master.
- If it is `list` or empty, list the roles from AGENTS.md and stop.
- Otherwise read `agents/<first argument>.md` and only the sections it lists, start your reply with `Role: <name>`, then do the rest of the arguments as the task, if any.
