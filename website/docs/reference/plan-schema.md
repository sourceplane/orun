---
title: Plan schema
---

The plan schema defines the artifact produced by `gluon plan` and consumed by `gluon run`.

## Top-level fields

| Field | Meaning |
| --- | --- |
| `apiVersion` | `gluon.io/v1` |
| `kind` | Always `Plan` |
| `metadata` | Name, description, namespace, generation timestamp, checksum |
| `execution` | Concurrency, fail-fast behavior, and state-file name |
| `spec.jobBindings` | Optional metadata about bound jobs |
| `jobs` | The concrete execution DAG |

## Job fields

Each job can include:

- `id`
- `name`
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
  "apiVersion": "gluon.io/v1",
  "kind": "Plan",
  "metadata": {
    "name": "demo",
    "generatedAt": "2026-01-01T00:00:00Z",
    "checksum": "sha256-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  },
  "execution": {
    "concurrency": 4,
    "failFast": true,
    "stateFile": ".gluon-state.json"
  },
  "jobs": []
}
```

Treat the plan as an immutable artifact. Do not hand-edit it unless you are debugging the runtime itself.