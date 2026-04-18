---
title: Compositions
---

Compositions are the execution contract between desired state and runtime behavior. A component type such as `helm` or `terraform` resolves to a composition directory that contains the schema and job registry for that type.

## Composition directory shape

The built-in repository layout looks like this:

```text
assets/config/compositions/
├── charts/
│   ├── job.yaml
│   └── schema.yaml
├── helm/
│   ├── job.yaml
│   └── schema.yaml
├── helmCommon/
│   ├── job.yaml
│   └── schema.yaml
└── terraform/
    ├── job.yaml
    └── schema.yaml
```

## What lives in a composition

### `schema.yaml`

The schema defines the valid inputs for the component type. That is where platform teams capture validation rules, defaults, and domain-specific constraints.

### `job.yaml`

The job registry defines the executable jobs for the type. A composition can expose one job or several, each with its own `runsOn`, timeout, retries, labels, and step list.

## Example: Helm composition

The repository's `helm` composition defines a deploy workflow with retries, timeouts, and templated commands:

```yaml
jobs:
  - name: deploy
    runsOn: ubuntu-22.04
    timeout: 15m
    retries: 2
    steps:
      - name: deploy
        run: helm upgrade --install {{.Component}} {{.chart}} --namespace {{.namespacePrefix}}{{.Component}}
```

The matching schema declares fields such as `chart`, `timeout`, `namespacePrefix`, and `pullPolicy`.

## Why compositions scale well

- They keep execution logic centralized instead of duplicating shell scripts across repositories.
- They let platform teams publish stricter schemas without changing every intent file.
- They make plan generation deterministic because component type resolution is explicit.
- They support multiple runtime backends because the compile step is separate from execution.

## Inspecting compositions from the CLI

```bash
arx compositions --config-dir assets/config/compositions
arx compositions helm --config-dir assets/config/compositions
arx compositions list helm --config-dir assets/config/compositions --long --expand-jobs
```

Read [composition contract](../compositions/composition-contract.md) when you are ready to author your own type.