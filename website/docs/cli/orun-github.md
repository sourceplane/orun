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
orun github runs --sha abc123 --details --limit 5
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
| `--orun-dir` | `.` | Target `.orun` directory |

**Resolution order** (when no explicit `--run-id` is given):

1. `--exec-id`: parse `gh-{run_id}-{attempt}-{sha}`, fetch that run
2. `--sha`: list runs for the SHA, pick the latest
3. `--failed`: list runs with `conclusion=failure`, pick the latest
4. Default: latest run for the current branch

---

### `orun github status`

Lightweight remote status: lists artifact shards grouped by execution ID without downloading full shard contents.

```bash
orun github status
```

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
- [GitHub artifacts workflow example](../examples/github-artifacts-workflow.yaml)