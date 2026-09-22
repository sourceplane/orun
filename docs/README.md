# Contributor documentation

This directory holds long-form architecture and design documents for people
working **on** orun. User-facing documentation (concepts, guides, the CLI
reference, release notes) lives on the documentation site, whose source is
under [`website/docs/`](../website/docs/).

| Document | What it covers |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | The six-stage compiler pipeline, the runner, the object model under `.orun/`, and how the packages under `internal/` fit together |
| [CORE-ARCHITECTURE.md](CORE-ARCHITECTURE.md) | A short statement of the core model: intent in, deterministic plan out, converge per commit |
| [scaffolding.md](scaffolding.md) | The Blueprint model behind `orun new` and `orun baseline new`: inputs, sources, modules, phases, hooks, the output gate, provenance and upgrade |
| [plans/](plans/) | Dated implementation plans for larger pieces of work |
| [examples/](examples/) | Supporting files referenced from the documents above |
| [archive/](archive/) | Shipped proposals and one-off reviews, kept for the record |

Design proposals for new work start as a spec under [`specs/`](../specs/),
one directory per epic, and stay there as the as-built record. See
[CONTRIBUTING.md](../CONTRIBUTING.md#design-proposals-and-specs).

The `context-for-ai/` directory at the repository root is a separate set of
orientation documents written for coding agents working in this codebase.
