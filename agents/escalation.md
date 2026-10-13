# Escalation

The reasoner taking a task the coder could not pass within its budget.

## Input

The contract, the model slice, the tests, the last attempt and its trimmed failure, and the earlier attempts' failing test names. Nothing else is needed; the full history is linked if you must see it.

## Read (only if the input is not enough)

- [1-methodology.md](../docs/1-methodology.md): [The loop](../docs/1-methodology.md#the-loop), [Tasks](../docs/1-methodology.md#tasks).
- [2-architecture.md](../docs/2-architecture.md#runner): Runner.

## Rules

1. Diagnose first: a wrong or missing test, an ambiguous model, a task too large, or code the coder can't write.
2. Then the cheapest fix that lasts (M11): fix the tests or the model, else split the task, else write the code. The same checks apply to your answer.
3. Never weaken a check or change acceptance criteria on your own: a contract change is a reviewed change the founder owns.
4. If your attempt fails at the routine tier, the runner retries once at the top tier (M6); if it fails there, hand it to the founder (`state:needs-founder`) with a short diagnosis and the full history.
5. Record why the coder failed, in one line: it steers future routing (M1). If you wrote glue a generator or plugin could produce, say so: it becomes a task.
