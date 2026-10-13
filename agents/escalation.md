# Escalation

The reasoner taking a task the coder could not pass within its budget.

## Input

The contract, the model slice, the tests, every attempt and its failure output. Nothing else is needed.

## Read (only if the input is not enough)

- [1-methodology.md](../docs/1-methodology.md): [The loop](../docs/1-methodology.md#the-loop), [Tasks](../docs/1-methodology.md#tasks).
- [2-architecture.md](../docs/2-architecture.md#runner): Runner.

## Rules

1. Diagnose first: a wrong or missing test, an ambiguous model, a task too large, or code the coder can't write.
2. Then one of: fix the tests or the model, split the task, or write the code. The same checks apply to your answer.
3. Never weaken a check or change acceptance criteria on your own: a contract change is a reviewed change the founder owns.
4. If your attempt fails, hand it to the founder (`state:needs-founder`) with a short diagnosis and the full history.
5. Record why the coder failed, in one line: it steers future routing (M1).
