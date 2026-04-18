# Core Architecture and Extensibility

This document explains the core runtime flow and how to extend `arx` in a CNCF-style Go CLI pattern.

## Runtime Flow (Compiler Pipeline)

`arx` follows a deterministic compile pipeline:

1. **Load**: parse intent and composition assets.
2. **Normalize**: canonicalize component/environment fields and dependency defaults.
3. **Validate**: validate each component against its composition schema.
4. **Expand**: materialize environment × component instances.
5. **Plan**: bind component instances to job definitions and resolve job dependencies.
6. **DAG checks**: detect cycles and topologically order jobs.
7. **Render**: materialize immutable plan output (`json`/`yaml`).

The pipeline is intentionally split into focused packages under [internal](../internal):

- [internal/loader](../internal/loader/loader.go): loading and schema compilation.
- [internal/normalize](../internal/normalize/intent.go): intent canonicalization.
- [internal/expand](../internal/expand/expander.go): environment expansion + merge logic.
- [internal/planner](../internal/planner/planner.go): job binding and dependency edges.
- [internal/planner/graph.go](../internal/planner/graph.go): cycle detection and topological sort.
- [internal/render](../internal/render/plan.go): deterministic output rendering.

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

1. Keep command wiring in dedicated command files under [cmd/arx](../cmd/arx).
2. Place business logic in `internal/*` packages, not in Cobra handlers.
3. Keep each command focused on one user intent (`plan`, `validate`, `debug`, etc.).
4. Reuse pipeline stages instead of duplicating parsing/normalization logic.

Current command structure:

- [cmd/arx/commands_root.go](../cmd/arx/commands_root.go): root command + registration.
- [cmd/arx/command_plan.go](../cmd/arx/command_plan.go): `plan` command wiring.
- [cmd/arx/command_run.go](../cmd/arx/command_run.go): `run` command wiring and plan loading.
- [cmd/arx/command_validate.go](../cmd/arx/command_validate.go): `validate` command wiring.
- [cmd/arx/command_debug.go](../cmd/arx/command_debug.go): `debug` command wiring.
- [cmd/arx/command_component.go](../cmd/arx/command_component.go): `component` command wiring.
- [cmd/arx/command_compositions.go](../cmd/arx/command_compositions.go): `compositions` command wiring.

### Minimal pattern

- Add a new `cobra.Command` near existing command declarations.
- Register it in `init()` with clear flags and short help text.
- Implement command logic by calling existing internal packages.

Example extension targets:

- `lint`: policy-only checks without plan rendering.
- `graph`: export DAG as DOT/Mermaid.
- `explain <component>`: explain merged config and dependency path.

The new `run` flow already follows this pattern:

- CLI parsing stays in [cmd/arx/command_run.go](../cmd/arx/command_run.go)
- execution behavior lives in [internal/runner/runner.go](../internal/runner/runner.go)

So adding future runtime commands such as `apply`, `resume`, or `cancel` can reuse the same runtime package with minimal Cobra changes.

## Package Contracts

To keep future additions safe:

- Treat `internal/model` as stable contracts between stages.
- Keep `expand` stage pure (input normalized intent → output component instances).
- Keep `render` stage side-effect free except file writing API.
- Prefer deterministic iteration (sorted keys, explicit order inputs).

This keeps behavior predictable across CI environments and aligns with typical CNCF tool expectations.