---
title: Compiler pipeline
---

`arx` follows a deterministic compiler pipeline from desired state to executable plan.

## Stages

| Stage | What happens |
| --- | --- |
| Load | Parse intent, component manifests, and composition assets |
| Normalize | Canonicalize component and environment fields |
| Validate | Enforce schema constraints for each component type |
| Expand | Materialize environment × component instances |
| Plan | Bind compositions to jobs and resolve dependency edges |
| DAG checks | Detect cycles and compute execution order |
| Render | Materialize the final JSON or YAML plan |

## Why the pipeline is explicit

This separation gives you clear failure boundaries:

- schema errors fail during validation
- dependency errors fail during planning or DAG checks
- runtime issues fail only after a valid plan exists

That structure also makes the planner easier to test and reason about in CI.

## Commands and stages

- `validate` focuses on load, normalize, and validate
- `debug` exposes internal views of planning stages
- `plan` executes the full compile pipeline and writes the artifact
- `run` operates only on the compiled plan

Read [execution runtime](./execution-runtime.md) for what happens after the plan is rendered.