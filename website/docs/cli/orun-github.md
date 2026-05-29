---
title: orun github
---

`orun github` inspects GitHub Actions artifact shards and workflow runs — no `actions/download-artifact` step required.

## Prerequisites

Requires one of:
- `GITHUB_TOKEN` env var
- `GH_TOKEN` env var
- `gh auth token` (authenticated `gh` CLI)

The repository is resolved from `GITHUB_REPOSITORY` or inferred from the `origin` git remote.

## Commands

| Command | Purpose |
| --- | --- |
| `orun github runs` | List workflow runs and their artifact shards |
| `orun github pull` | Download all shards and hydrate into `.orun/executions/` |
| `orun github status` | Quick remote status via artifact name parsing |
| `orun github logs` | Download specific job shard logs |

---

### `orun github runs`

List workflow runs with artifact shard counts. Three levels of detail:

| Level | What happens | Speed |
|-------|-------------|-------|
| 1 (default) | List runs + parse exec-id, role, status from artifact names | Fast |
| 2 (`--details`) | Download plan shard manifests for exact status | Medium |
| 3 (`orun github pull`) | Full shard download + hydrate | Slowest |

```bash
orun github runs
orun github runs --branch main --failed
orun github runs --sha 0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b --details --limit 5
```

**Flags:**

| Flag | Default | Meaning |
| --- | --- | --- |
| `--workflow` | `orun.yaml` | Workflow filename filter |
| `--branch` | | Branch filter |
| `--sha` | | Commit SHA filter |
| `--failed` | `false` | Show only failed runs |
| `--limit` | `10` | Max runs to show |
| `--details` | `false` | Download manifests for accurate status |

---

### `orun github pull`

Download all Orun artifact shards from a GitHub Actions workflow run, synthesize the execution, and hydrate into the local `.orun/executions/` layout. Once hydrated, existing `orun status`, `orun logs`, and `orun describe` can inspect the execution offline.

```bash
# Pull the latest run for the current branch
orun github pull --latest

# Pull by execution ID
orun github pull --exec-id gh-26185145757-1-a1b2c3d4

# Pull the latest failed run
orun github pull --failed

# Include raw (unredacted) logs
orun github pull --failed --include-raw

# Pull to a custom .orun directory
orun github pull --latest --orun-dir /tmp/my-orun
```

**Flags:**

| Flag | Default | Meaning |
| --- | --- | --- |
| `--run-id` | `0` | Explicit GitHub run ID |
| `--exec-id` | | Execution ID (`gh-<run>-<attempt>-<sha>`) |
| `--sha` | | Pull latest run for this SHA |
| `--branch` | | Pull latest run for this branch |
| `--latest` | `false` | Pull the latest run |
| `--failed` | `false` | Pull the latest failed run |
| `--include-raw` | `false` | Include unredacted logs |
| `--orun-dir` | `.` | Target working directory. A `.orun/` subdirectory is created/used inside it (e.g. `--orun-dir /tmp/foo` writes to `/tmp/foo/.orun/`). For back-compat, a path that already ends in `.orun` is also accepted as-is. |

> **SHA note:** `--sha` is forwarded to the GitHub API, which expects the **full 40-char commit SHA**. Short SHAs (`abc123`) return *"no runs found"*. Use `git rev-parse HEAD` to expand.

**Resolution order** (when no explicit `--run-id` is given):

1. `--exec-id`: parse `gh-{run_id}-{attempt}-{sha}`, fetch that run
2. `--sha`: list runs for the full SHA, pick the latest
3. `--branch`: latest run on that branch
4. `--failed`: latest run with `conclusion=failure`
5. `--latest` / default: latest run for the current branch

---

### `orun github status`

Lightweight remote status: lists artifact shards grouped by execution ID without downloading full shard contents. Accepts the same resolution flags as `pull` / `logs`.

```bash
orun github status                                   # latest run on current branch
orun github status --latest --branch main
orun github status --exec-id gh-26606158847-1-896da6b09c29
orun github status --run-id 26606158847
```

**Flags:** `--run-id`, `--exec-id`, `--sha` (full SHA), `--branch`, `--latest`, `--failed` — same semantics as `orun github pull`.

---

### `orun github logs`

Download specific job artifact shard logs from a workflow run.

```bash
# Logs from the latest run
orun github logs --latest

# Logs for a specific execution and job
orun github logs --exec-id gh-26185145757-1-a1b2c3d4 --job my-job-id

# Logs from the latest failed run
orun github logs --failed
```

**Flags:**

| Flag | Default | Meaning |
| --- | --- | --- |
| `--run-id` | `0` | Explicit GitHub run ID |
| `--exec-id` | | Execution ID (`gh-<run>-<attempt>-<sha>`) |
| `--sha` | | Latest run for this SHA |
| `--branch` | | Latest run for this branch |
| `--failed` | `false` | Latest failed run |
| `--latest` | `false` | Latest run |
| `--job` | | Job ID to fetch logs for |

> **`--job` matching:** Currently a substring match against the GitHub artifact name (e.g. `--job job_896da6b` or `--job api-edge-worker`), not a structured component/env/job lookup. Use `orun github runs --details` to discover available shard names. See improvements doc.

## Artifact naming

All Orun artifacts use the naming convention:

```
orun.v1.<exec-id>.<role>.<suffix>.<status>
```

Examples:
```
orun.v1.gh-26185145757-1-a1b2c3d4.plan.a1b2c3d4.created
orun.v1.gh-26185145757-1-a1b2c3d4.job.7f6a9c21d4e8b012.failed
```

Components:
- `orun.v1` — fixed prefix and version
- `gh-{run_id}-{attempt}-{sha}` — execution ID
- `plan` or `job` — shard role
- `{suffix}` — plan short SHA or job UID
- `{status}` — `created`, `completed`, `failed`, `cancelled`, etc.

## Partial hydration

When some job shards are missing (cancelled run, in-progress execution), `orun github pull` produces `status: "partial"` rather than failing. Missing shards appear as "pending" in the synthesized state.

## Security

- Default hydration includes only redacted logs. Use `--include-raw` for trusted users.
- The GitHub token is never logged or persisted.
- Artifact ZIP extraction includes path traversal defense.
- Private repo pull requires `Actions: read` fine-grained token permission.

## See also

- [Execution model: CI artifacts](../concepts/execution-model.md#ci-artifacts)
- [GitHub artifacts workflow example](https://github.com/sourceplane/orun/blob/main/docs/examples/github-artifacts-workflow.yaml)