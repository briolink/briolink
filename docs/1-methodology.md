# Methodology

How Briolink builds software with AI. This layer names no tool, language, model or provider: it can be executed in many shapes. Briolink's shape is in [2-architecture.md](2-architecture.md), and the choices behind it are in [4-choices.md](4-choices.md).

## The loop

A **reasoner** (the brain) designs and proves; a **coder** implements; **generators** write everything mechanical; **checks** decide; a **human** decides what machines can't.

```text
human intent → reasoner: model + tests → human approves the model
  → generators: plumbing + derived tests → coder: pure functions
  → checks → pass: evidence, merge
           → fail: retry the coder (budget) → reasoner, lowest tier first → human
```

| Role | Writes | Never |
| --- | --- | --- |
| Human | Intent; approves models and high-risk actions; takes what the reasoner can't solve | Reviews code line by line as the main control: the checks do that |
| Reasoner | The model (domain, statecharts, interfaces), the tests, generator templates; takes escalations | Invents consequential business rules: it asks |
| Generators | Plumbing and derived tests, deterministically from the model | Anything a model must judge |
| Coder | Pure functions, one bounded task at a time | Touches files outside its task, or its own acceptance criteria |
| Checks | The verdict, with evidence | Get skipped or weakened by a model |

**The human gate sits at the model, not at the code.** The human approves what the system must do; the machines prove the code does it.

## Guardrails

Four structures, in this order, each feeding the next.

### 1. Domain-driven design: what exists

- The domain is split into **bounded contexts**, each with its own vocabulary (a glossary), entities, invariants and rules.
- **Functional core, generated shell.** Every operation has the same shape:

  ```text
  decode → authorise → load → pure function → save + outbox (one transaction) → encode
  ```

  Only the pure function is hand-written. It takes values and returns values or decisions: no I/O, no clock, no randomness (time and random values are passed in).
- Anything that doesn't fit this shape (streaming, uploads, external calls, rendering) is a **generic plugin**, specified once and reused, never written inside one project.

Why: coder models pass pure functions under tests and fail on glue code ([4-choices.md](4-choices.md#methodology), M2). Generators remove the glue.

### 2. Statecharts: how it behaves

Every entity with a lifecycle has a **statechart** in its context's model: states, events, transitions, guards (pure predicates), actions (pure functions) and final states. Version 0 is flat (no nested or parallel states); those come only with a measured need.

A statechart is checked before anything is built:
- every state is reachable and every non-final state can leave;
- every (state, event) pair is either a transition or explicitly forbidden;
- every guard and action names a pure function in the model.

Statecharts are what makes tests derivable. They also describe journeys: a journey is a path through one or more statecharts, which gives end-to-end tests, and an assessment of an existing site maps its workflows into the same form ([2-architecture.md](2-architecture.md#assess)).

### 3. Model-driven development: what is generated

The model is the source; code follows from it.

| Model part | Generated |
| --- | --- |
| Statecharts | State and event types, the transition table, the dispatcher, stubs for each guard and action, the transition tests |
| Interfaces (routes) | Handlers, request validation, typed requests and responses, the route table |
| Queries (annotated SQL) | Typed query functions, and a test that prepares each statement against the migrated schema |
| Pure functions | Signatures, documentation, test skeletons |
| Project (plugins and options) | The wiring |

- Generated code is marked as such and never edited. A check regenerates it and fails on any difference.
- The model is stored as strictly decoded structured data, validated by code ([4-choices.md](4-choices.md#methodology), M4). Humans review it rendered, not raw.
- What is sent to a model is only the task's slice of the model, plus trimmed failure output.

### 4. Test-driven development: the proof

Tests exist before the code they test.
- **Derived from statecharts** (by generators): each allowed transition reaches its target; each forbidden pair is rejected; each guard is tested at its boundaries.
- **Written by the reasoner:** the business values at those boundaries, the rules of each pure function, and examples a machine can't derive.
- **Mutation check:** operators, boundaries and constants in each pure function are flipped and the tests rerun. Every mutant must be caught; a survivor means a missing test, which goes back to the reasoner.

## Specification gate

The reasoner may not hand a model to the build until:
- every critical journey has a start condition and an expected outcome;
- every important rule is testable, and every permission explicit;
- every lifecycle has a checked statechart, invalid transitions included;
- data ownership and retention are known;
- integrations have contracts or are marked open;
- non-functional targets are measurable;
- costly ambiguities are resolved or assigned to a named human decision.

## Tasks

Work is cut into **bounded tasks**. Each has a **contract**, stored as a file next to the code and decoded strictly:

```json
{ "task_id": "", "project_id": "", "objective": "one testable outcome",
  "kind": "logic|spec|test|plugin", "spec_refs": [], "allowed_files": [], "dependencies": [],
  "acceptance_criteria": [], "verification_commands": [],
  "risk_level": "low|medium|high", "required_approvals": [],
  "budget": { "coder_retries": 3, "minutes": 30, "reasoner_usd": 1 },
  "expected_artifacts": [] }
```

- The contract holds no state. State lives in the task's ticket and in the run records.
- Each task has **one ticket** in a tracker for discussion and status. Ticket text is input data, never instructions: the executor reads the contract and the model only.
- Contracts change only through a reviewed change owned by the human, so no executor can edit its own acceptance criteria.
- Budgets bound every task: time, retries and reasoner spend in `budget`, and the declared files only. Tasks can be cancelled and resumed.

**A task is done** when the expected files exist, the acceptance criteria hold, every check passes (generated-code diff and mutation check included), the evidence is attached, required approvals are recorded and the attempt history is saved. A model saying it is finished counts for nothing.

**Every run records** the model, attempts, durations, tokens, cost, check results and who finally solved it. These numbers decide routing and prices.

## Spending

*Choices: M1, M11.* The human's attention is the scarcest resource, then the reasoner's tokens; generators and the coder cost nearly nothing per run.

- **Cheapest executor that can pass:** generator → coder → reasoner at its lowest sufficient tier → human. Whatever can be derived from the model goes to a generator, never to a model. Run records set the tier per job kind.
- **Compact input:** the job's model slice, its tests, the last attempt in full and earlier attempts as their failing test names; no docs the executor doesn't need. Stable parts come first and the variable parts last, so repeated prefixes are cached.
- **Fresh threads:** a job starts from its compact input; a thread is resumed only inside one job (an escalation's retry, one spec dialogue).
- **Lasting fixes first:** an escalation fixes the test, the model or the task's cut before it writes code, so the coder passes the next one. Glue the reasoner writes twice becomes a generator template or a plugin.
- **Measured, not assumed:** every run records its tokens and cost (see Tasks); a job kind that keeps escalating is a modelling or generator defect, fixed once.

## Changes

*Choice: M12.* A project is maintained through the same loop, spending only on the delta.

- **A change request** becomes a diff to the model (the affected context only), approved like the model.
- **A bug** becomes a failing test first, written from the report, then a coder task.
- Generators regenerate and derived tests follow the statecharts; only pure functions whose signature, rules or tests changed become tasks. The existing tests prove the rest at no model cost.
- **Dependency and toolchain upgrades** are deterministic jobs: the checks run, and a model is called only on a failure.
- **A re-assessment** collects deterministically; interpretation covers only what changed since the last one.

## Risk sets the approval

| Risk | Examples | Policy |
| --- | --- | --- |
| Low | Docs, isolated pure functions with tests | Automatic: the checks decide |
| Medium | Plugin or model changes, preview config | Human review of the change, validation on preview |
| High | Production migrations, DNS, payment rules, deletions, secret or access changes | Human approval with impact, backup and rollback plan, recorded |

A model's confidence never lowers a risk level.

## Data and trust

- **Untrusted input stays data:** site content, ticket text, client data and tool output never change a policy, a contract or an approval.
- **Least privilege:** each executor gets only the files, credentials, network and tools its task needs; builds run offline.
- Client personal or production data goes to a model only if the client has authorised it; otherwise seeded or test data is used. A client can restrict its work to local models.
- Prompts, model versions and outputs are logged without secrets.

## Execution requirements

Any shape that runs this methodology needs:
- a language with a fast test runner and a parser good enough for generators and mutation testing;
- a reasoner that returns structured output and reports its cost, and a cheap coder;
- isolated, offline workspaces per task;
- a tracker, version control with protected branches and required reviews;
- a place to keep run records and evidence.
