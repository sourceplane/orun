---
title: Plan schema
---

The plan schema defines the artifact produced by `orun plan` and consumed by `orun run`.

## Top-level fields

| Field | Meaning |
| --- | --- |
| `apiVersion` | `orun.io/v1` |
| `kind` | Always `Plan` |
| `metadata` | Name, description, namespace, generation timestamp, checksum, profile, trigger |
| `execution` | Concurrency, fail-fast behavior, and state-file name |
| `spec.jobBindings` | Optional metadata about bound jobs |
| `jobs` | The concrete execution DAG |

## Metadata fields

| Field | Meaning |
| --- | --- |
| `metadata.name` | Plan name |
| `metadata.generatedAt` | ISO 8601 generation timestamp |
| `metadata.checksum` | SHA-256 digest of the plan content |
| `metadata.profile` | Name of the execution profile used to generate this plan (empty if none) |
| `metadata.trigger` | Name of the automation trigger that initiated plan generation (empty if manual) |

## Job fields

Each job can include:

- `id`
- `uid` — deterministic unique identifier derived from plan digest + job key
- `name`
- `displayName` — human-readable name (e.g., "Terraform fmt")
- `checkName` — CI check name format: "component · environment · DisplayName"
- `component`
- `environment`
- `composition`
- `jobRegistry`
- `job`
- `runsOn`
- `path`
- `dependsOn`
- `timeout`
- `retries`
- `env`
- `labels`
- `config`

### Job identity fields

Three fields added for CI/CD integration:

| Field | Format | Purpose |
| --- | --- | --- |
| `uid` | `job_<plan-short>_<job-hash>` | Stable cross-plan identifier for deduplication |
| `displayName` | Title case from job name | Human-readable label (hyphens → spaces, capitalized) |
| `checkName` | `component · environment · DisplayName` | Formatted for GitHub Checks and similar APIs |

## Step fields

Each step must declare `id` and one of:

- `run`
- `use`

Steps can also declare:

- `name`
- `phase`
- `order`
- `with`
- `env`
- `shell`
- `working-directory`
- `timeout`
- `retry`
- `onFailure`

## Minimal example

```json
{
  "apiVersion": "orun.io/v1",
  "kind": "Plan",
  "metadata": {
    "name": "demo",
    "generatedAt": "2026-01-01T00:00:00Z",
    "checksum": "sha256-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  },
  "execution": {
    "concurrency": 4,
    "failFast": true,
    "stateFile": ".orun-state.json"
  },
  "jobs": []
}
```

Treat the plan as an immutable artifact. Do not hand-edit it unless you are debugging the runtime itself.