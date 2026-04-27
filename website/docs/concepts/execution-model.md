---
title: Execution model
---

`gluon` keeps planning and execution separate on purpose. `plan` produces an immutable DAG, and `run` consumes that DAG through an explicit execution backend.

## Execute is the default

`gluon run` executes steps immediately. Add `--dry-run` to preview without running.

```bash
# Execute (default)
gluon run

# Preview only
gluon run --dry-run
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

`run` chooses its backend in a stable order:

1. `--gha`
2. `--runner`
3. `GLUON_RUNNER`
4. `GITHUB_ACTIONS=true`
5. Auto-detection when the compiled plan contains a `use:` step
6. Fallback to `local`

## Concurrent job execution

Jobs that have no dependency relationship execute concurrently. The degree of parallelism is controlled by `plan.execution.concurrency` in the compiled plan, and can be overridden at runtime:

```bash
gluon run --concurrency 4
```

Setting `--concurrency 1` forces strictly sequential execution, which is useful for debugging.

## Step phases and ordering

Steps can declare `phase` and `order` attributes.

- `phase`: `pre`, `main`, or `post`
- `order`: ascending integer inside a phase

Execution stays deterministic:

1. all `pre` steps
2. all `main` steps
3. all `post` steps

Within a phase, `gluon` sorts by `order` and then preserves declaration order.

## Execution records and state

Each `gluon run` creates an isolated execution record under `.gluon/executions/{exec-id}/`:

```
.gluon/
  executions/
    latest          → symlink to most recent exec
    my-plan-20240601-a1b2c3/
      metadata.json     # timing, user, trigger, job counts
      state.json        # per-job and per-step completion status
      logs/
        job-id/
          step-id.log   # raw step output
```

That structure enables:

- **Resumable execution** — already-completed jobs are skipped
- **Job-level retry** — `--job <id> --retry` clears only that job's state
- **Immutable logs** — `gluon logs` reads from the execution record
- **Parallel-safe CI** — each run gets its own `exec-id`, avoiding shared state collisions

Use `GLUON_EXEC_ID` or `--exec-id` to pin an ID from CI for traceability.

### Migration from legacy state

If you have a pre-v0.10 `.gluon-state.json` file, `gluon run` automatically migrates it into the new structure on first execution.

## Working directory rules

By default, each job runs in its own resolved job path. Use `--workdir` to override that behavior globally:

```bash
gluon run --workdir ./examples
```

When the GitHub Actions backend is selected and `--workdir` is not explicitly set, `gluon` uses `GITHUB_WORKSPACE` when that variable is available.

## Runtime environment variables

During execution, `gluon` injects runner context into the step environment:

- `GLUON_CONTEXT`
- `GLUON_RUNNER`

That gives steps a consistent way to understand whether they are running locally, in a container, or through the GitHub Actions-compatible backend.
