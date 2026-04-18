---
title: Internals
---

The `arx` codebase keeps command wiring, compilation stages, and runtime execution in separate packages so the planner remains testable and extensible.

## Top-level structure

| Area | Responsibility |
| --- | --- |
| `cmd/arx` | Cobra command wiring and flag definitions |
| `internal/loader` | Load intent, component manifests, and composition assets |
| `internal/schema` | Compile and enforce JSON schema validation |
| `internal/normalize` | Canonicalize input structures before expansion |
| `internal/expand` | Materialize environment × component instances |
| `internal/planner` | Bind jobs, resolve dependencies, topologically order the DAG |
| `internal/render` | Materialize deterministic plan output |
| `internal/runner` | Execute a compiled plan with state tracking |
| `internal/executor` | Backend adapters for local, Docker, and GitHub Actions execution |
| `internal/gha` | GitHub Actions-compatible execution engine |
| `internal/git` | Change-detection helpers |
| `internal/ui` | Terminal formatting and presentation helpers |

## Design constraints

- planning stages should be deterministic
- runtime behavior should stay explicit in the plan artifact
- command handlers should stay thin
- internal model contracts should be stable between stages

That split is what lets `arx` keep the compile boundary clean while still supporting multiple execution backends.