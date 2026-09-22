---
title: Compiler pipeline
description: The six deterministic stages that turn platform, component, and golden-path intent into plan.json, and which commands stop at which stage.
---

`orun plan` is a compiler: a pure function from intent, discovered
components, locked compositions, and the trigger context to `plan.json`.
It runs six stages in a fixed order, and identical inputs produce a
byte-identical plan.

## Stages

| Stage | Name | What happens |
| --- | --- | --- |
| 0 | Load and validate | Parse `intent.yaml`, the discovered `component.yaml` files, and the composition sources; validate each against its JSON schema and fail fast |
| 1 | Normalize | Resolve wildcards, default missing fields, canonicalize dependency references and environment subscriptions |
| 2 | Expand | Materialize the environment × component matrix and merge group and environment `parameterDefaults` with component `parameters` and policies |
| 3 | Bind | Match each component's type to a composition, select the profile the trigger asks for, and render step templates |
| 4 | Resolve | Convert component dependencies into job dependencies, detect cycles, and compute execution order |
| 5 | Materialize | Emit `plan.json` with every reference concrete and a content checksum |

## Why the pipeline is explicit

Each stage has a clear failure boundary:

- schema errors fail in stage 0, before anything is expanded;
- policy violations fail in stage 2, when the plan is built rather than when it runs;
- dependency errors fail in stage 4;
- runtime problems can only occur after a valid plan exists.

Every implicit default, policy merge, and dependency edge becomes explicit
in the output, which is what makes a plan diff in a pull request a faithful
preview of behaviour.

## Commands and stages

| Command | Stops after |
| --- | --- |
| `orun validate` | Stage 0, plus the normalization needed to validate components against their composition schemas |
| `orun intent explain`, `orun intent render` | Stage 1, for the effective intent after presets and defaults |
| `orun component` | Stage 2, showing one merged component |
| `orun debug` | Prints every stage's intermediate representation |
| `orun plan` | Stage 5, writing the plan and sealing it into the object model |
| `orun run` | Consumes a materialized plan; never re-runs the compiler |

Read [execution runtime](./execution-runtime.md) for what happens after the
plan is materialized, and [internals](./internals.md) for the packages that
implement each stage.
