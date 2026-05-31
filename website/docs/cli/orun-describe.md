---
title: orun describe
---

`orun describe` shows detailed information about a specific run, plan, job, or component.

## Usage

```bash
orun describe <resource> [name]
```

Supported resources: `run`, `plan`, `job`, `component`, `revision`, `trigger`, `execution`.

## Common examples

Describe the latest execution:

```bash
orun describe run
orun describe run latest
```

Describe a specific execution:

```bash
orun describe run my-plan-20240601-a1b2c3
```

Describe the latest plan:

```bash
orun describe plan
```

Describe a plan by name or checksum prefix:

```bash
orun describe plan release-candidate
orun describe plan a1b2c3
```

Describe a job from the latest plan:

```bash
orun describe job api-edge-worker@production.verify-deploy-cloudflare-worker-turbo
```

Describe a component:

```bash
orun describe component network-foundation
```

Describe a `PlanRevision` (latest or by key):

```bash
orun describe revision latest
orun describe revision rev-pr139-def456a-p8f31c09
```

Describe the `TriggerOccurrence` for the latest revision (or pinned by
trigger name):

```bash
orun describe trigger latest
orun describe trigger github-pull-request
```

Describe a specific execution (`run-NNN` under a revision, or a legacy
exec id):

```bash
orun describe execution run-001
orun describe execution my-plan-20240601-a1b2c3
```

## Slash notation

`describe` also accepts slash notation directly on the parent command:

```bash
orun describe run/latest
orun describe plan/release-candidate
orun describe job/api-edge-worker@production.verify-deploy-cloudflare-worker-turbo
orun describe component/network-foundation
```

## Output

### `describe run`

Shows full execution metadata including plan reference, status, timing, trigger, and a per-job breakdown with status, duration, and any errors.

### `describe plan`

Shows plan ID, generated timestamp, checksum, concurrency settings, composition sources, and a full job list with dependency edges.

### `describe job`

Shows component, environment, composition, working directory, timeout, retries, dependencies, and step details (run commands or `use:` references). If an execution record exists, also shows the job's runtime state.

### `describe component`

Equivalent to `orun component <name> --long`. Shows the merged view with all inputs, labels, overrides, and per-environment instances.

### `describe revision`

Renders the `revision.json` + `manifest.json` pair for the resolved
`PlanRevision`. Includes the trigger summary, plan hash, job count,
on-disk path, and the latest execution's status. `latest` resolves to
the newest revision in `refs/latest-revision.json`.

### `describe trigger`

Renders the `TriggerOccurrence` (`trigger.json`) for the resolved
revision. `latest` follows the same ref as `describe revision latest`;
passing a trigger name resolves through `refs/triggers/<name>/latest.json`.

### `describe execution`

Renders the `ExecutionRun` (`execution.json` + `snapshot.latest.json`)
for the resolved execution. Falls back to the legacy
`.orun/executions/<id>/` tree when the new layout has no match. See
[State model](../concepts/state-model.md) for the resolution chain.

## Related commands

- `orun status` — compact live view of an execution
- `orun logs` — raw step output
- `orun get` — listing views for plans, jobs, components, environments
