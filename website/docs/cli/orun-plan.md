---
title: orun plan
---

`orun plan` compiles intent, component discovery, and compositions into an immutable execution DAG.

## Usage

```bash
orun plan [component]
```

Pass a **component name** as a positional argument to restrict the plan to that component — equivalent to `--component <name>`:

```bash
orun plan network-foundation
```

When run without `--intent`, `orun` automatically discovers `intent.yaml` by walking up the directory tree to the git root. The plan is always **global** — it always includes all components. Use the positional argument or `--component` to explicitly restrict compilation to specific components.

Restrict environments with `--env <list>` (comma-separated), or make the all-environments default explicit with `--all-envs` (`--env` and `--all-envs` are mutually exclusive). A scoped plan is **self-contained**: dependency or promotion edges pointing at environments/components left out of the selection are pruned, reported as a warning, and recorded under `metadata.selection.prunedEdges`. The selection itself is stamped on the plan as `metadata.selection` (`envs`, `components`, `mode`, `allEnvs`).

Every successful compile produces an immutable **`PlanRevision`** — the pairing of
the compiled plan with the trigger occurrence that produced it — recorded in the
content-addressed object model under `.orun/objectmodel/`, with the
`revisions/latest` ref moved to point at it. `orun run` resolves the latest
revision automatically, so you rarely need to specify an output path. See
[State model](../concepts/state-model.md) for the full layout.

Bare `orun plan` invocations resolve a `system.manual` trigger so the
revision/trigger chain is unbroken even for ad-hoc local runs.

Before compiling, `orun plan` refreshes the object-model catalog so change
detection and the cockpit see current component data. Use `--no-catalog-refresh`
to skip that step, or `--catalog-strict` to fail the plan when the catalog cannot
be resolved.

## Common examples

Generate a plan (auto-discovers intent):

```bash
orun plan
```

Generate a plan scoped to one component (positional argument):

```bash
orun plan network-foundation
```

Generate a plan from a specific intent file:

```bash
orun plan -i examples/intent.yaml
```

Generate with an explicit output path (for backwards compatibility):

```bash
orun plan -i examples/intent.yaml -o /tmp/orun-plan.json
```

Generate a named plan that can be referenced by name later:

```bash
orun plan -i examples/intent.yaml --name release-candidate
```

Generate YAML output:

```bash
orun plan -i examples/intent.yaml -o plan.yaml -f yaml
```

Filter to one environment:

```bash
orun plan -i examples/intent.yaml --env staging
```

Filter to one component:

```bash
orun plan -i examples/intent.yaml --component api-edge-worker
```

Preview the dependency graph while compiling:

```bash
orun plan -i examples/intent.yaml --view dag
```

Focus on changed components:

```bash
orun plan -i examples/intent.yaml --changed --base main
```

Since v2.9.0 `--changed` only includes components whose files
actually changed. Dependencies are pulled in only when their edge
declares `include: always` — see
[Dependency rules → Include policy](../concepts/dependency-rules.md#include-policy-plan-selection).

Debug how `--changed` resolved its git refs:

```bash
orun plan -i examples/intent.yaml --changed --explain
```

Use trigger bindings for CI-driven environment activation:

```bash
orun plan --from-ci github --event-file "$GITHUB_EVENT_PATH"
```

Simulate a named trigger locally:

```bash
orun plan --trigger github-pull-request --base main --head HEAD
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path (auto-discovered if not set) |
| `--output`, `-o` | Explicit output path (optional; defaults to `.orun/plans/`) |
| `--format`, `-f` | Output format: `json` or `yaml` |
| `--name` | Give the plan a memorable name for later reference via `--plan <name>` |
| `--debug` | Enable debug logging during planning |
| `--env`, `-e` | Restrict compilation to specific environments (comma-separated) |
| `--all-envs` | Compile all environments explicitly (mutually exclusive with `--env`) |
| `--component` | Restrict compilation to one or more components (repeatable) |
| `--view`, `-v` | Render a view such as `dag`, `dependencies`, or `component=<name>` |
| `--changed` | Enable change-aware filtering |
| `--base` | Base git ref for change detection |
| `--head` | Head git ref for change detection |
| `--files` | Explicit changed-file list |
| `--uncommitted` | Scope to uncommitted changes |
| `--untracked` | Scope to untracked files |
| `--explain` | Print how `--changed` resolved its base and head refs |
| `--no-catalog-refresh` | Skip the pre-plan catalog refresh; plan without catalog context |
| `--push-catalog` | After planning, sync the resolved catalog snapshot to the configured backend and advance the head (like `catalog refresh --push`). Requires a configured backend; conflicts with `--no-catalog-refresh` |
| `--catalog-strict` | Fail the plan on catalog resolution errors |
| `--trigger` | Named trigger binding for environment activation |
| `--from-ci` | CI provider for event normalization (e.g. `github`) |
| `--event-file` | Path to provider event JSON file |
| `--artifact` | Artifact backend for uploading plan shard from CI (`github`) |
| `--github-output` | Write `matrix`, `plan_id`, `exec_id` to `$GITHUB_OUTPUT` (for GitHub Actions matrix) |

### GitHub Actions usage

When run inside GitHub Actions with `--artifact github`, `orun plan`:

1. Derives an execution ID from `GITHUB_RUN_ID`, `GITHUB_RUN_ATTEMPT`, and the plan checksum
2. Writes a plan shard (manifest.json + plan.json + metadata) to a temp directory
3. Uploads the shard as a GitHub Actions artifact using the embedded `@actions/artifact` helper
4. With `--github-output`, writes `matrix`, `plan_id`, and `exec_id` to `$GITHUB_OUTPUT` for downstream matrix jobs

Example:
```bash
orun plan \
  --from-ci github \
  --event-file "$GITHUB_EVENT_PATH" \
  --artifact github \
  --github-output
```

This replaces the need for `actions/upload-artifact@v4` and manual `jq` piping in CI workflows.

## Output contract

The generated plan contains explicit jobs, dependency edges, step phases, labels, and runtime metadata. Read [plan schema](../reference/plan-schema.md) for the full structure.

Recorded plans can be inspected with `orun get plans` and `orun describe plan <id>`, which read the plan nodes from the object model.

Use `--config-dir` only when you need to load legacy folder-shaped compositions instead of intent-declared packages.

See [context-aware discovery](../concepts/context-discovery.md) for how `orun run` and `orun get jobs` auto-filter by component when run from inside a component directory.
