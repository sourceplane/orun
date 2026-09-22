# GitHub Artifacts

Orun can produce immutable GitHub Actions artifact shards from CI execution — plan evidence, job results, and logs — without requiring `actions/upload-artifact` steps in your workflow YAML.

## How it works

Each Orun invocation produces one immutable shard:

| Shard type | Command | Contents |
|------------|---------|----------|
| **Plan shard** | `orun plan --artifact github` | `manifest.json`, `plan.json`, `checksums.json`, metadata |
| **Job shard** | `orun run --artifact github` | `manifest.json`, `state.json`, step logs |

Shards follow the naming convention: `orun.v1.<exec-id>.<role>.<suffix>.<status>`

Upload uses the official `@actions/artifact` package via an embedded Node.js helper — the same package that powers `actions/upload-artifact`.

## Artifact names

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

## Three levels of remote inspection

| Level | Command | What happens |
|-------|---------|-------------|
| 1 | `orun github runs` | List workflow runs + artifact names only. Parses exec-id, role, status from names. Fast. |
| 2 | `orun github status` | Download plan shard manifests only (no logs). Exact status. |
| 3 | `orun github pull` | Full shard download + hydrate into `.orun/executions/`. |

## CLI commands

### List workflow runs

```bash
orun github runs
orun github runs --branch main --failed
orun github runs --sha abc123 --details
```

### Pull and hydrate

```bash
orun github pull --latest
orun github pull --exec-id gh-26185145757-1-a1b2c3d4
orun github pull --failed --include-raw
```

### Quick status

```bash
orun github status
```

### Download logs

```bash
orun github logs --latest
orun github logs --exec-id gh-26185145757-1-a1b2c3d4 --job my-job-id
```

## Partial hydration

When some job shards are missing (cancelled run, in-progress execution), hydration produces `status: "partial"` rather than failing. Missing shards appear as "pending":

```
EXECUTION gh-26185145757-1-a1b2c3d4  ◐ partial  13/18 shards
```

## Environment variables

| Variable | Purpose |
|----------|---------|
| `ORUN_ARTIFACT_BACKEND=github` | Select GitHub store |
| `ORUN_ARTIFACT_UPLOAD=true` | Enable upload in CI |
| `ORUN_ARTIFACT_RETENTION_DAYS=14` | Override retention days |
| `ORUN_SKIP_ARTIFACT_UPLOAD=true` | Disable upload for debugging |
| `ORUN_EXEC_ID` | Set by plan output for downstream jobs |

## Workflow example

See [docs/examples/github-artifacts-workflow.yaml](./examples/github-artifacts-workflow.yaml) for a complete workflow template.

## Security

- Default hydration includes only redacted logs. Use `--include-raw` for trusted users.
- The GitHub token is never logged or persisted.
- Artifact ZIP extraction includes path traversal defense.
- Local pull requires `Actions: read` fine-grained token permission for private repos.