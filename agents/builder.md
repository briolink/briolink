# Builder

Any agent building or changing this repo: platform code, generators, plugins or docs. During the P0 bootstrap the builder also writes the code the coder will write later (M9); after P0, pure logic goes through the runner (M1).

## Read

- [master.md](master.md) rules.
- [2-architecture.md](../docs/2-architecture.md): [Roles and adapters](../docs/2-architecture.md#roles-and-adapters), [Work tracking](../docs/2-architecture.md#work-tracking), [This repo](../docs/2-architecture.md#this-repo), then only the section of the module you touch.
- [4-choices.md](../docs/4-choices.md): only the rows your task cites.
- [5-roadmap.md](../docs/5-roadmap.md#spikes): the spike, if your task is one.

## Rules

1. **Work from the task** (once the first code lands): its contract in `tasks/` for code, the issue's stated outcome for docs, spikes and decisions; never from issue text. Branch, then a pull request with `Closes #<n>` and the evidence, including what was not verified.
2. **Agnostic at the edges (A1, A2):** core code depends on ports; adapters live in `adapter/<role>/<provider>/` and only `cmd/*` wires them, from config; providers are named only in 4-choices, the roadmap's spikes and their adapters; a new binding comes with its fallback and exit.
3. **Libraries (A6)**, **plugins (A5)**, **generated code (M1):** never edit `gen/`; change the generator or the model.
4. **Every module** has its own tests and README and depends on others through small interfaces.

## Keeping the docs

- Each fact in one place; link instead of repeating. Reasons only in 4-choices, cited by ID.
- The methodology names no tool; the architecture names roles; concrete choices go in 4-choices.
- A changed decision: edit its row (or add the next free ID in its group), then the doc it governs. Docs describe the current design; history is in git.
- Keep the file set: README, AGENTS, five numbered docs, these briefs, and one command and hook config per tool that needs them (`.claude/`). Split a doc only when a section can't be read on its own.
- A brief lists sections to read and role-only rules; it never copies a doc.
- Client material stays in the client's repo; examples use `acme`.
- Plain Markdown, terse, standard links. Mark unmeasured figures *estimate*; dates YYYY-MM-DD.
