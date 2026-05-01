---
title: orun run
---

`orun run` executes the jobs and steps from a compiled plan. **Execution is the default** — add `--dry-run` to preview without running.

When run from inside a component directory, `orun run` automatically scopes execution to that component and its transitive dependencies. Use `--all` to run all jobs.

## Usage

```bash
orun run [component|planhash]
```

The optional positional argument controls what gets run:

- **component name** — generates a fresh plan scoped to that component and runs it immediately
- **plan hash or name** — runs the matching saved plan from `.orun/plans/`
- _(omitted)_ — generates a fresh plan from the current intent and runs it

```bash
# Generate and run a fresh plan scoped to one component
orun run network-foundation

# Run a previously saved plan by name or hash prefix
orun run release-candidate
orun run a1b2c3

# Generate and run a full fresh plan
orun run
```

## Common examples

Preview the execution order without running:

```bash
orun run --dry-run
```

Generate and run a fresh plan for one component immediately:

```bash
orun run api-edge-worker
```

Execute a saved plan by name (legacy `--plan` form still works but is deprecated):

```bash
orun run my-plan
```

Execute a specific plan file:

```bash
orun run --plan /tmp/plan.json
```

Execute inside Docker:

```bash
orun run --runner docker
```

Force GitHub Actions compatibility mode:

```bash
orun run --gha
```

Show full step logs instead of the compact summary view:

```bash
orun run --verbose
```

Run only one job:

```bash
orun run --job network-foundation@development.validate-terraform
```

Retry a failed job (clears its saved state first):

```bash
orun run --job network-foundation@development.validate-terraform --retry
```

Override the working directory for every job:

```bash
orun run --workdir ./examples
```

Filter to one environment or component at runtime:

```bash
orun run --env staging
orun run --component api-edge-worker
```

Run all jobs when inside a component directory (override auto-scoping):

```bash
orun run --all
```

Override plan concurrency:

```bash
orun run --concurrency 4
```

When `--concurrency` is greater than 1, result lines carry inline component and environment labels (e.g., `platform-shared·production/verify-turbo-package`) so each line is self-describing without group headers.

Process up to 3 components concurrently while keeping the dashboard grouped:

```bash
orun run --concurrency 8 --component-concurrency 3
```

Pin an execution ID for CI/parallel-safe tracking:

```bash
orun run --exec-id ci-run-${GITHUB_RUN_ID}
```

Run detached and monitor separately:

```bash
orun run --background
orun status --watch
```

Run only changed components (useful in PRs):

```bash
orun run --changed --base main
```

Debug how `--changed` resolved its git refs:

```bash
orun run --changed --explain
```

## Flags

### Execution flags

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Preview what would run without executing |
| `--verbose` | Expand full step logs instead of the compact summary view |
| `--workdir` | Override the working directory for all jobs |
| `--job` | Run only one job ID (matches plan job ID or prefix) |
| `--retry` | Clear existing state for `--job` before running |
| `--runner` | Execution backend: `local`, `docker`, or `github-actions` |
| `--gha` | Shortcut for `--runner github-actions` |
| `--exec-id` | Execution ID for resume or CI tracing (auto-generated if not set) |
| `--concurrency` | Override plan concurrency (0 uses the plan's value) |
| `--component-concurrency` | Max components processed concurrently (default 1; 0 = unlimited). Default 1 keeps the dashboard component-grouped |
| `--component` | Filter jobs to a specific component (repeatable) |
| `--env`, `-e` | Filter jobs to a specific environment |
| `--all` | Disable CWD-based component scoping; run all jobs |
| `--json` | Output execution summary in JSON format |
| `--isolation` | Per-job workspace isolation: `auto` (on when concurrency > 1), `workspace` (always on), or `none` (legacy shared tree). Default: `auto` |
| `--keep-workspaces` | Preserve per-job staged workspaces after the run for debugging |
| `--background` | Run the plan detached and return immediately. Track progress with `orun status --watch` |

### Plan selection flags

| Flag | Meaning |
| --- | --- |
| `--plan`, `-p` | Plan reference: file path, name, or checksum prefix _(deprecated — pass the reference as a positional argument instead)_ |

### Change detection flags

These flags generate a fresh plan scoped to changed components before running. They are ignored when the positional argument resolves to a saved plan.

| Flag | Meaning |
| --- | --- |
| `--changed` | Generate plan scoped to changed components (requires git) |
| `--base` | Base git ref for change detection |
| `--head` | Head git ref for change detection |
| `--files` | Explicit comma-separated changed-file list (overrides git diff) |
| `--uncommitted` | Scope to uncommitted changes |
| `--untracked` | Scope to untracked files |
| `--explain` | Print how `--changed` resolved its base and head refs |

:::note Deprecated flag
`--job-id` is a deprecated alias for `--job`. Use `--job` in new scripts.
:::

## Backend resolution

`run` chooses its backend in this order:

1. `--gha`
2. `--runner`
3. `ORUN_RUNNER`
4. `GITHUB_ACTIONS=true`
5. Auto-detection when the plan contains a `use:` step
6. Default to `local`

## State and execution records

Each run creates an execution record under `.orun/executions/{exec-id}/`:

- `state.json` — per-job and per-step completion status
- `metadata.json` — timing, user, trigger, and job counts
- `logs/{job}/{step}.log` — raw step output

That structure lets `run`:

- skip already-completed jobs on resume
- retry a single failed job with `--job` and `--retry`
- record immutable per-execution logs accessible via `orun logs`

If a `.orun-state.json` file exists from a pre-v0.10 installation, `orun run` automatically migrates it on the first execution.

## Plan resolution

The positional argument is resolved in this order:

| Argument | Resolves to |
| --- | --- |
| _(omitted)_ | Generates a fresh plan from `intent.yaml`, then runs it |
| `my-plan` | `.orun/plans/my-plan.json` (saved plan by name) |
| `a1b2c3` | Any plan whose checksum starts with `a1b2c3` (saved plan by hash prefix) |
| `./plan.json` | Explicit file path (when it exists on disk) |
| `network-foundation` | Generates a fresh plan scoped to that component, then runs it (when not a saved plan) |

The legacy `--plan` flag accepts the same values and is still supported, but the positional form is preferred.

## Scope mismatch warning

When a plan was generated with CWD-based scoping (e.g., from the `api` component directory) and you run it from a different component directory, `orun run` prints a warning:

```
warning: plan was generated for [api, common-services] but current scope is [web, common-services]
```

To avoid the mismatch, either regenerate the plan from your current directory or use `--all`.

See [context-aware discovery](../concepts/context-discovery.md) for full details on auto-scoping behavior.
