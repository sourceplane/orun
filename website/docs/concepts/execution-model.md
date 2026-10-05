---
title: Execution model
description: How orun executes exactly what the plan says, through a runner you choose - so the discipline compiled into the plan does not depend on which CI system or machine runs the job.
---

The plan is where the standards were checked; execution's job is to do exactly what the
plan says and nothing else. orun runs a compiled plan through a swappable runner and
records every step, so discipline does not depend on which CI system or machine runs the
job.

`orun` keeps planning and execution separate on purpose. `plan` produces an immutable DAG, and `run` consumes that DAG through an explicit execution backend.

## Execute is the default

`orun run` executes steps immediately. Add `--dry-run` to preview without running.

```bash
# Execute (default)
orun run

# Preview only
orun run --dry-run
```

Dry-run mode is useful in review-heavy environments because it lets you inspect:

- job ordering
- resolved working directories
- chosen runner backend
- retries, timeouts, and step phases

## Supported runners

| Runner | What it does | When to use it |
| --- | --- | --- |
| `local` | Executes each `run:` step through `sh -c` on the host | Local development and machines that already have the required binaries |
| `docker` | Pulls the job image, mounts the workspace at `/workspace`, and executes inside a container | CI or review flows where you want stronger environment isolation |
| `github-actions` | Executes GitHub Actions-style `use:` steps and compatible workflow commands | Plans that embed Actions behavior or need compatibility with the GitHub Actions execution model |

If a step contains `use:`, the local executor fails fast and asks you to rerun with `--gha` or `--runner github-actions`.

## Runner resolution order

`run` chooses its backend in a stable order: `--gha`, then `--runner`, then
`ORUN_RUNNER`, then the GitHub Actions runner when `GITHUB_ACTIONS=true` or the plan
contains a `use:` step, then `local`. See [runners](../execute/runners.md#selection-order).

## Concurrent job execution

Jobs that have no dependency relationship execute concurrently. The degree of parallelism is controlled by `plan.execution.concurrency` in the compiled plan, and can be overridden at runtime:

```bash
orun run --concurrency 4
```

Setting `--concurrency 1` forces strictly sequential execution, which is useful for debugging.

### Concurrent output

When `--concurrency` is greater than 1, each result line carries its component and environment prefix inline (e.g., `platform-shared·production/verify-turbo-package`). This replaces the group-header model used in sequential mode, which produces empty or interleaved headers under concurrency.

## Step phases and ordering

Steps can declare `phase` and `order` attributes.

- `phase`: `pre`, `main`, or `post`
- `order`: ascending integer inside a phase

Execution stays deterministic:

1. all `pre` steps
2. all `main` steps
3. all `post` steps

Within a phase, `orun` sorts by `order` and then preserves declaration order.

## Execution records and state

Each `orun run` records an immutable **execution** in the content-addressed object
model under `.orun/objectmodel/`: the execution node and its jobs, attempts, steps,
and per-step log blobs, with `executions/latest` (and `executions/by-id/<exec-id>`)
moved to point at the sealed run. While the run is in flight it is published under
`executions/live/<exec-id>` so live readers can follow it. See
[State model](../concepts/state-model.md) for the full layout.

That model enables:

- **Resumable execution** — run again with the same `--exec-id` and already-completed
  jobs are skipped (and carry their prior step logs forward)
- **Job-level retry** — `--job <id> --retry` re-runs only that job; on remote
  state it also re-opens the job's failed claim and waits out failed upstream
  dependencies, so a CI "rerun failed jobs" resumes cleanly — see
  [Resume-aware CI reruns](../cli/orun-run.md#resume-aware-ci-reruns)
- **Immutable logs** — `orun logs` reads the sealed log blobs
- **Parallel-safe CI** — each run gets its own `exec-id`, content-addressed and
  collision-free

Use `ORUN_EXEC_ID` or `--exec-id` to pin an ID from CI for traceability.

## Working directory rules

By default, each job runs in its own resolved job path. Use `--workdir` to override that behavior globally:

```bash
orun run --workdir ./examples
```

When the GitHub Actions backend is selected and `--workdir` is not explicitly set, `orun` uses `GITHUB_WORKSPACE` when that variable is available.

## Runtime environment variables

During execution, `orun` injects runner context into the step environment:

- `ORUN_CONTEXT`
- `ORUN_RUNNER`

That gives steps a consistent way to understand whether they are running locally, in a container, or through the GitHub Actions-compatible backend.

## CI artifacts

On GitHub Actions, `orun plan --artifact github` and `orun run --artifact github` upload
immutable shards of execution evidence (the plan, job results, logs) without any
`actions/upload-artifact` steps, and `orun github` inspects or imports them. See
[GitHub Actions artifacts](../architecture/github-artifacts.md).
