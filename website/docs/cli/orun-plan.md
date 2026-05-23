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

The generated plan is saved to `.orun/plans/{checksum}.json` and also written as `.orun/plans/latest.json`. `orun run` resolves `latest` automatically, so you rarely need to specify an output path.

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
| `--env`, `-e` | Restrict compilation to one environment |
| `--component` | Restrict compilation to one or more components (repeatable) |
| `--view`, `-v` | Render a view such as `dag`, `dependencies`, or `component=<name>` |
| `--changed` | Enable change-aware filtering |
| `--base` | Base git ref for change detection |
| `--head` | Head git ref for change detection |
| `--files` | Explicit changed-file list |
| `--uncommitted` | Scope to uncommitted changes |
| `--untracked` | Scope to untracked files |
| `--explain` | Print how `--changed` resolved its base and head refs |
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

Plans stored in `.orun/plans/` can be inspected with `orun get plans` and `orun describe plan <id>`.

Use `--config-dir` only when you need to load legacy folder-shaped compositions instead of intent-declared packages.

See [context-aware discovery](../concepts/context-discovery.md) for how `orun run` and `orun get jobs` auto-filter by component when run from inside a component directory.
