# Core Architecture and Extensibility

This document explains the core runtime flow and how to extend `orun` in a CNCF-style Go CLI pattern.

## Runtime Flow (Compiler Pipeline)

`orun plan` runs a six-stage compiler (the same six stages named in the README and in
[the compiler pipeline page](../website/docs/architecture/compiler-pipeline.md)):

| Phase | Name | What it does |
|---|---|---|
| 0 | **Load & Validate** | Parse intent, component manifests, and composition assets; validate against the JSON schemas; fail fast |
| 1 | **Normalize** | Resolve wildcards, default missing fields, canonicalize component/environment fields and dependency defaults |
| 2 | **Expand** | Materialize the environment × component matrix and merge parameters and policies |
| 3 | **Bind** | Match each component type to its composition job template and render step templates |
| 4 | **Resolve** | Convert component dependencies into job dependencies, detect cycles, and topologically order the DAG |
| 5 | **Materialize** | Emit the immutable plan (`json`/`yaml`) with every reference concrete |

The pipeline is intentionally split into focused packages under [internal](../internal):

- [internal/loader](../internal/loader/loader.go) and [internal/schema](../internal/schema/validator.go): load & validate.
- [internal/normalize](../internal/normalize/intent.go): normalize.
- [internal/expand](../internal/expand/expander.go): expand — environment expansion + merge logic.
- [internal/planner](../internal/planner/planner.go): bind — job binding and dependency edges.
- [internal/planner/graph.go](../internal/planner/graph.go): resolve — cycle detection and topological sort.
- [internal/render](../internal/render/plan.go): materialize — deterministic output rendering.

The full package map, including the subsystems added since this document was
written, is in [internals](../website/docs/architecture/internals.md).

## Step Phases and Ordering

Job steps now support optional `phase` and `order` attributes.

- `phase`: `pre`, `main`, `post` (default: `main`)
- `order`: integer used inside each phase (default: `0`)

Execution ordering is deterministic:

1. `pre`
2. `main`
3. `post`

Within each phase, steps are ordered by `order` ascending, then by declaration order.

This keeps runtime execution linear while making pre/post hooks explicit and extensible.

Component-level step overrides are also supported via `overrides.steps` in intent:

- Match by `name`
- Replace the base step entirely
- If a step name does not exist in the base job, it is appended

After overrides are applied, planner ordering is resolved by `phase` + `order` + declaration order.


## Extending the CLI with New Subcommands

CNCF-style guidance for new commands:

1. Keep command wiring in dedicated command files under [cmd/orun](../cmd/orun).
2. Place business logic in `internal/*` packages, not in Cobra handlers.
3. Keep each command focused on one user intent (`plan`, `validate`, `debug`, etc.).
4. Reuse pipeline stages instead of duplicating parsing/normalization logic.

Current command structure:

- [cmd/orun/commands_root.go](../cmd/orun/commands_root.go): root command + registration.
- [cmd/orun/command_plan.go](../cmd/orun/command_plan.go): `plan` command wiring.
- [cmd/orun/command_run.go](../cmd/orun/command_run.go): `run` command wiring and plan loading.
- [cmd/orun/command_validate.go](../cmd/orun/command_validate.go): `validate` command wiring.
- [cmd/orun/command_debug.go](../cmd/orun/command_debug.go): `debug` command wiring.
- [cmd/orun/command_component.go](../cmd/orun/command_component.go): `component` command wiring.
- [cmd/orun/command_compositions.go](../cmd/orun/command_compositions.go): `compositions` command wiring.

### Minimal pattern

- Add a new `cobra.Command` near existing command declarations.
- Register it in `init()` with clear flags and short help text.
- Implement command logic by calling existing internal packages.

Example extension targets:

- `lint`: policy-only checks without plan rendering.
- `graph`: export DAG as DOT/Mermaid.
- `explain <component>`: explain merged config and dependency path.

The new `run` flow already follows this pattern:

- CLI parsing stays in [cmd/orun/command_run.go](../cmd/orun/command_run.go)
- execution behavior lives in [internal/runner/runner.go](../internal/runner/runner.go)

So adding future runtime commands such as `apply`, `resume`, or `cancel` can reuse the same runtime package with minimal Cobra changes.

## Package Contracts

To keep future additions safe:

- Treat `internal/model` as stable contracts between stages.
- Keep `expand` stage pure (input normalized intent → output component instances).
- Keep `render` stage side-effect free except file writing API.
- Prefer deterministic iteration (sorted keys, explicit order inputs).

This keeps behavior predictable across CI environments and aligns with typical CNCF tool expectations.