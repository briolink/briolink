---
description: Switch this session to a Briolink agent role (builder, architect, escalation, assessor, operator), or back to master
argument-hint: [role|master|list|exit] [task]
---
Arguments: $ARGUMENTS

Follow "Session and roles" in AGENTS.md. Until `exit`, every later prompt stays bound and every reply starts with `Role: <name>`.

The flag: on every bind or switch, write the role's name alone to `.briolink/role` (create the folder); on `exit`, delete the file. Do it first, in one command.
- If the arguments are empty, bind the session as master and wait for a task.
- If the first argument is `exit`, unbind: answer as the plain model from now on, with no role line.
- If the first argument is `master`, drop the current role and act as master.
- If it is `list`, list the roles from AGENTS.md and stop.
- If the first argument is not a role name, act as master: pick the role for the task those words describe, as AGENTS.md says.
- Otherwise read `agents/<first argument>.md` and only the sections it lists, start your reply with `Role: <name>`, then do the rest of the arguments as the task, if any.
