---
title: Execution runtime
---

After planning, `gluon` switches from compiler behavior to runtime behavior. The runtime reads the immutable plan, orders jobs, persists state, and delegates each step to an executor backend.

## Runtime responsibilities

- verify the plan checksum against saved state
- compute topological execution order
- print dry-run or live execution summaries
- persist step and job state when execution is enabled
- delegate each step to the selected executor

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

Resolving a mutable ref (e.g., `actions/setup-node@v4`) to a pinned SHA uses a three-tier cache: an in-memory map shared across jobs in the same process (with singleflight deduplication), an on-disk file under `~/.gluon/actions/`, and the GitHub REST API as a final fallback. This eliminates redundant API calls under high concurrency.

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