# Briolink

Briolink builds software with AI under strict guardrails, and runs what it builds.

**The idea.** A reasoning model designs: it writes the domain model, the statecharts that define how things behave, and the tests. Generators turn that model into all the mechanical code. A small local model writes only the pure business logic. Automated checks decide when a task is done, and anything the machines can't solve goes to a human. People approve *what* the system must do; the machines prove the code does it.

**What it offers**, on one pipeline:
- **Assess:** an evidence-backed audit of an existing website, with honest coverage and a prioritised backlog.
- **Build:** modernize that site, or build a new app from a specification.
- **Host:** run the result fast and cheaply, on the client's own accounts, with continuous backups and failover.

## The docs

Read in order:

1. [Methodology](docs/1-methodology.md): the method, independent of any tool.
2. [Architecture](docs/2-architecture.md): how Briolink runs it.
3. [Hosting](docs/3-hosting.md): how the results are served and operated.
4. [Choices](docs/4-choices.md): every decision and why, including today's providers and what happens if one fails.
5. [Roadmap](docs/5-roadmap.md): where we are and what's next.

AI agents start at [AGENTS.md](AGENTS.md), which sends each kind of agent to a short brief in [agents/](agents/) listing only what it needs to read.

## Status

Design. The code arrives with the first stage of the roadmap (P0, the pipeline core).
