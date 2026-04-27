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