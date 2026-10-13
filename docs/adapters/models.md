# Adapter: models (coder and reasoner)

Current bindings for the **coder model** and **reasoner model** roles ([bindings.md](../bindings.md)). The runner talks to both through the `Coder` and `Reasoner` ports, so swapping a model means a new adapter plus a config change ([platform.md](../platform.md#runner)).

## Coder: `qwen3-coder:30b` through Ollama

- **Where:** Ollama on the ops host's GPU (MoE model, mostly in 16 GB of VRAM, ~2 GB spilled to RAM). The adapter speaks Ollama's HTTP API on the private interface.
- **Good at:** pure functions under tests. In the pilot it passed those, but failed on glue code and on edge rules the tests didn't pin down (decision 39).
- **To measure in P0:**
  - tokens per second, and time per task while the GPU serves other work;
  - first-pass rate per task kind.
- **Prompt input:** compact JSON, only the task's slice of the spec (decision 68).
- **Swap:** any local model served over an HTTP API. Bind it per task kind if one model is better at certain tasks.

## Reasoner: Claude through headless Claude Code

**Invocation (decision 58):**

| Need | Flag |
| --- | --- |
| Non-interactive, on the founder's subscription login | `claude -p`, without `--bare` (`--bare` ignores the subscription and needs an API key) |
| Session ID and a cost estimate in the result | `--output-format json` |
| Results the runner can validate | `--json-schema` |
| Tools limited to the task workspace | `--allowedTools` |
| Unattended runs | `--permission-mode dontAsk --permission-prompts none` |
| One thread per task or spec | `--resume <session>` |
| A spending cap per run | `--max-budget-usd` |

**Plan and terms (decision 71, checked 2026-10-13):**
- **Pro subscription, no API key.** `claude -p` and the Agent SDK draw from the plan's usage limits; Anthropic's help center states they can be used with a Claude plan. Pro has no monthly API credits (those are Max and Team only, and only through an API key).
- **Limits:** when the plan's limit is reached, the runner queues reasoner work until it resets; the coder model keeps working.
- **Fallback, no API key:** if unattended use is blocked or the login expires, the control plane turns each reasoner job into a hand-off package (task, attempts, failures, expected output). The founder runs it in Claude Code through the editor extension, and the result returns through the repo.
- **Accepted risk:** using a consumer plan for client work is a grey area in the Consumer Terms (a reading, not legal advice).
- **Swap:** any reasoning model with structured output and cost reporting, through the `Reasoner` port.

## Spikes

- [ ] Coder: tokens per second and time per task through Ollama while the GPU serves other work
- [ ] Reasoner: the subscription login stays valid unattended; JSON output gives cost and session ID; `--max-budget-usd` stops a run
- [x] Reasoner: Anthropic's terms checked; answer recorded in decision 71
- [ ] Reasoner: queueing when the plan's limit is reached, and the hand-off fallback, work end to end

## What the adapters record

Per call, for the control plane's metrics:
- model;
- prompt and spec version;
- tokens;
- cost;
- duration;
- result.
