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

- **Terms check pending:** Anthropic's current terms for unattended use on a subscription were unclear (sources disagreed as of 2026-10). If they rule it out, the adapter switches to an API key and keeps recording costs.
- **Swap:** any reasoning model with structured output and cost reporting, through the `Reasoner` port.

## Spikes

- [ ] Coder: tokens per second and time per task through Ollama while the GPU serves other work
- [ ] Reasoner: the subscription login stays valid unattended; JSON output gives cost and session ID; `--max-budget-usd` stops a run
- [ ] Reasoner: Anthropic's terms for unattended use on a subscription checked, answer recorded in decision 58

## What the adapters record

Per call, for the control plane's metrics:
- model;
- prompt and spec version;
- tokens;
- cost;
- duration;
- result.
