---
description: Switch this session to a Briolink agent role (builder, architect, escalation, assessor, operator), or back to master
argument-hint: [role|master|list] [task]
---
Arguments: $ARGUMENTS

Follow "Session and roles" in AGENTS.md:
- If the arguments are empty, act as master and wait for a task.
- If the first argument is `master`, drop the current role and act as master.
- If it is `list`, list the roles from AGENTS.md and stop.
- If the first argument is not a role name, act as master: pick the role for the task those words describe, as AGENTS.md says.
- Otherwise read `agents/<first argument>.md` and only the sections it lists, start your reply with `Role: <name>`, then do the rest of the arguments as the task, if any.
