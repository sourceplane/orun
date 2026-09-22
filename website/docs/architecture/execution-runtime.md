---
title: Execution runtime
description: How orun run turns an immutable plan into jobs — executor backends, per-job workspace isolation, lifecycle hooks, and remote coordination through the state backend.
---

After planning, `orun` switches from compiler behavior to runtime behavior. The runtime (`internal/runner`) reads the immutable plan, orders jobs, persists state, and delegates each step to an executor backend.

## Runtime responsibilities

- verify the plan checksum against saved state
- compute topological execution order
- print dry-run or live execution summaries
- persist step and job state when execution is enabled
- stage a private working tree per job when isolation is on
- delegate each step to the selected executor
- emit job and step lifecycle events to whoever is listening

## Per-job workspace isolation

`orun run --isolation <mode>` controls how each job's working tree is materialized (`runner.IsolationMode`):

| Mode | Behavior |
| --- | --- |
| `auto` (default) | Stage a private tree per job when the effective concurrency is greater than 1 |
| `workspace` | Always stage a private tree per job |
| `none` | Share the source tree across all jobs (the legacy behavior) |

Staging copies the source tree into a job-private directory using copy-on-write where the filesystem allows it — `clonefile(2)` on APFS, the `FICLONE` ioctl on Linux filesystems that support reflinks — and falls back to a byte copy elsewhere. Regenerable trees such as `node_modules`, `.turbo`, `dist`, and `.terraform` are skipped because the job recreates them. Isolation is skipped for `--dry-run` and when `--workdir` pins a directory explicitly; if staging fails the runner warns and falls back to the shared tree. `--keep-workspaces` leaves the staged directories behind for debugging.

## Lifecycle hooks

`runner.RunnerHooks` lets external code observe the run without coupling the runner to a backend. The hooks are synchronous and expected to be fast:

| Hook | Fires |
| --- | --- |
| `BeforeJob` | Before a job starts; returning `skipExec` treats the job as already complete |
| `OnJobStart` | Once a job begins, with the job's context and cancel function |
| `OnStepStart` | When a step begins, with its 1-based index and the job's step count |
| `AfterStepLog` | After each step completes, with the step record and its output |
| `AfterStepTerminal` | When a step reaches `completed` or `failed` |
| `AfterJobTerminal` | When a job reaches a terminal state |

The step-level hooks feed live rendering — the cockpit and `orun status --watch` key step progress off them instead of re-reading the working tree. The job-level hooks are how remote coordination and the object model attach to a run. The sealed run record remains the source of truth; hooks never are.

## Remote coordination

A run coordinates through the state backend when `--remote-state` is passed, `ORUN_REMOTE_STATE=true` is set, or the intent declares `execution.state.mode: remote`. `cmd/orun/command_run.go` then initialises the backend, calls `InitRun`, and installs hooks for per-job claim, heartbeat, log upload, and the terminal update:

- `BeforeJob` claims the job through the coordinator; a job another runner already completed is skipped, and a claim that finds a stale lease (the heartbeat timeout is five minutes) is retried.
- `OnJobStart` starts a heartbeat keyed to the job's lifetime and cancels the job if the server reports the lease was lost.
- `AfterStepLog` uploads log chunks attributed to the step.
- `AfterJobTerminal` reports the terminal status so dependent jobs are released.

Native event-sourced coordination is the default; `ORUN_COORDINATION=legacy` selects the retired relational path. `--local` forces local filesystem state for one run when the backend is unavailable.

## Executor backends

### Local executor

Runs `run:` steps through `sh -c` on the host. It is the simplest backend and the best default for local development.

### Docker executor

Ensures the image is available, mounts the workspace at `/workspace`, and executes inside a container. It uses `job.runsOn` as the image source.

### GitHub Actions executor

Uses the internal GitHub Actions engine to support `use:` steps, workflow command files, post-step handling, and GitHub Actions environment semantics.

#### Per-job environment isolation

Each job gets its own temp directory, `HOME`, `RUNNER_TEMP`, and file-command directories. This prevents jobs running concurrently from colliding on environment state.

#### Per-job action isolation

Remote actions are materialized into each job's temp directory before execution. Files are hardlinked from the shared on-disk cache — a zero-cost operation on the same filesystem. If the cache and temp directories are on different filesystems, a full copy is made automatically.

This means:
- The shared action cache is read-only during execution.
- A job cannot corrupt a cached action or affect a sibling job's copy.
- Local actions (workspace-relative paths) are used directly without copying.

#### Action reference caching

Resolving a mutable ref (e.g., `actions/setup-node@v4`) to a pinned SHA uses a three-tier cache: an in-memory map shared across jobs in the same process (with singleflight deduplication), an on-disk file under `~/.orun/actions/`, and the GitHub REST API as a final fallback. This eliminates redundant API calls under high concurrency.

## Phase boundaries

Execution stays linear but explicit:

1. `pre`
2. `main`
3. `post`

Within each phase, `order` and declaration order determine the exact step sequence.

## Failure behavior

- `failFast` is read from the plan execution block
- step-level `retry` values are honored
- `onFailure: continue` lets later steps run after a non-fatal failure
- job state is persisted only when execution is enabled

That keeps dry-run side-effect free while still letting execute mode resume safely.

## Related

- [`orun run`](../cli/orun-run.md) — every flag, including `--isolation`, `--remote-state`, and `--local`.
- [Runners](../execute/runners.md) — choosing between the local, Docker, and GitHub Actions backends.
- [Internals](./internals.md) — the runtime, workflow, and object-model packages.
- [Workflow actions](../concepts/workflow-actions.md) — `workflow:` steps and approval gates the runner executes.