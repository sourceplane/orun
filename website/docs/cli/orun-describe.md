---
title: orun describe
---

`orun describe` shows detailed information about a specific run, plan, job, or component.

## Usage

```bash
orun describe <resource> [name]
```

Supported resources: `run`, `plan`, `job`, `component`.

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
orun describe job api-edge-worker@production.deploy
```

Describe a component:

```bash
orun describe component web-app
```

## Slash notation

`describe` also accepts slash notation directly on the parent command:

```bash
orun describe run/latest
orun describe plan/release-candidate
orun describe job/api-edge-worker@production.deploy
orun describe component/web-app
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

## Related commands

- `orun status` — compact live view of an execution
- `orun logs` — raw step output
- `orun get` — listing views for plans, jobs, components, environments
