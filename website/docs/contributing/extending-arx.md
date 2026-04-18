---
title: Extending arx
---

`arx` is designed so new commands, stages, and runtime capabilities can be added without collapsing the planner architecture.

## Add a new CLI command

Follow the existing command pattern under `cmd/arx`:

1. keep Cobra wiring in a dedicated `command_*.go` file
2. place business logic in `internal/*`
3. reuse existing stage packages instead of duplicating parsing or normalization

That keeps the command layer readable and testable.

## Extend the planner

Prefer changes that preserve the current package contracts:

- `internal/model` stays the stage contract
- `expand` remains a pure transformation from normalized input to component instances
- `render` remains deterministic and side-effect free except for file writing

## Add a new runner backend

Execution backends live behind the executor interface in `internal/executor`.

When adding a backend:

1. implement the executor methods
2. register the backend in the executor registry
3. document backend-specific semantics in the runtime docs
4. add tests for preparation, step execution, and cleanup behavior

## Typical extension ideas

- `graph` output for richer DAG visualizations
- `lint` for policy-only validation passes
- new composition types for additional platform domains
- extra execution backends when the current local, Docker, and GitHub Actions set is not enough