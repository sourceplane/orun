---
title: Execution model
---

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

`run` chooses its backend in a stable order:

1. `--gha`
2. `--runner`
3. `ORUN_RUNNER`
4. `GITHUB_ACTIONS=true`
5. Auto-detection when the compiled plan contains a `use:` step
6. Fallback to `local`

## Concurrent job execution

Jobs that have no dependency relationship execute concurrently. The degree of parallelism is controlled by `plan.execution.concurrency` in the compiled plan, and can be overridden at runtime:

```bash
orun run --concurrency 4
```

Setting `--concurrency 1` forces strictly sequential execution, which is useful for debugging.

### Action isolation

When multiple jobs run concurrently and both use the same remote action (e.g., `actions/setup-node@v4`), each job works from its own isolated copy of that action's directory. The shared on-disk cache is never modified during execution, so jobs cannot race on the same files.

Isolation is zero-cost by default: files are hardlinked from the cache into each job's temp directory. A full copy only happens when the job temp directory is on a different filesystem than the cache.

Local actions (paths starting with `./`) are not isolated — they are used directly from the workspace.

### Action reference caching

Resolving a non-SHA action reference (e.g., `v4`) to a commit SHA requires a GitHub API call. orun avoids redundant API calls through a three-tier cache:

| Tier | Scope | What it stores |
| --- | --- | --- |
| In-memory | Current process | `repo@ref` → SHA map, deduplicated via singleflight |
| On-disk | Persistent across runs | SHA written to `~/.orun/actions/<repo>/refs/<ref>` |
| API | Fallback | GitHub REST API `/repos/<owner>/<repo>/commits/<ref>` |

This ensures that `--concurrency 4` with many jobs using the same action versions never triggers API rate limits, even with large platform repositories containing dozens of concurrent jobs.

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

Each `orun run` creates an isolated execution record under `.orun/executions/{exec-id}/`:

```
.orun/
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
- **Immutable logs** — `orun logs` reads from the execution record
- **Parallel-safe CI** — each run gets its own `exec-id`, avoiding shared state collisions

Use `ORUN_EXEC_ID` or `--exec-id` to pin an ID from CI for traceability.

### Migration from legacy state

If you have a pre-v0.10 `.orun-state.json` file, `orun run` automatically migrates it into the new structure on first execution.

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

When running in GitHub Actions, `orun` can produce immutable shard artifacts that capture execution evidence (plan, job results, logs) without requiring `actions/upload-artifact` steps in workflow YAML.

### How it works

Each `orun` invocation produces one shard:

- **Plan shard** — `orun plan --artifact github` writes a plan shard (manifest, plan.json, checksums) and uploads it as a GitHub Actions artifact.
- **Job shard** — `orun run --artifact github` uploads a job shard after the runner completes, even on failure. The original exit code is preserved.

Shards use the naming convention `orun.v1.<exec-id>.<role>.<suffix>.<status>` and are uploaded via an embedded `@actions/artifact` Node.js helper.

### CLI inspection

The `orun github` command tree provides remote inspection without downloading full artifacts:

- `orun github runs` — list workflow runs with artifact shard counts
- `orun github status` — lightweight remote status via artifact name parsing
- `orun github pull` — full download, synthesize, and hydrate into local `.orun/executions/`
- `orun github logs` — download specific job shard logs

### Partial hydration

When some job shards are missing (e.g., cancelled run), hydration produces `status: "partial"` rather than failing. Missing shards are recorded as "pending" in the synthesized state:

```
EXECUTION gh-26185145757-1-a1b2c3d4  ◐ partial  13/18 shards
```

### Env-based activation

Set the following environment variables in your GitHub Actions workflow to enable artifact upload without CLI flags:

- `ORUN_ARTIFACT_BACKEND=github` — select the GitHub store
- `ORUN_ARTIFACT_UPLOAD=true` — enable upload
- `ORUN_ARTIFACT_RETENTION_DAYS=14` — override artifact retention
