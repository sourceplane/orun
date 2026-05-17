---
title: Plan DAG
---

The plan is the compiled artifact produced by `orun plan`. It is the boundary between planning and execution.

## What the plan contains

A rendered plan includes:

- metadata such as name, namespace, timestamp, and checksum
- execution settings such as concurrency, fail-fast behavior, and state-file name
- concrete jobs with stable IDs like `web-app@production.deploy`
- ordered steps with `run` or `use` instructions
- fully resolved dependencies between jobs
- promotion gates for cross-plan environment dependencies

## Example structure

```json
{
  "apiVersion": "orun.io/v1",
  "kind": "Plan",
  "metadata": {
    "name": "microservices-deployment",
    "checksum": "sha256-..."
  },
  "execution": {
    "concurrency": 4,
    "failFast": true,
    "stateFile": ".orun-state.json"
  },
  "jobs": [
    {
      "id": "web-app@production.deploy",
      "runsOn": "ubuntu-22.04",
      "dependsOn": ["common-services@production.deploy"],
      "steps": [
        {
          "id": "deploy",
          "phase": "main",
          "run": "helm upgrade --install ..."
        }
      ]
    }
  ]
}
```

## Why the plan is important

The plan is where all implicit behavior becomes explicit. That gives you a stable artifact to:

- review in pull requests
- diff between intent revisions
- archive as a deployment record
- execute through different backends without recompiling

## Plan views during compilation

The `plan` command can render alternate views without changing the compiled output:

```bash
orun plan --view dag
orun plan --view dependencies
orun plan --view component=web-app
```

Use those views for review and debugging when you need to understand the dependency graph before execution.

## Promotion gates

When an environment declares a [promotion dependency](./environment-promotion.md) on another environment that is not active in the same plan, the plan includes **gates** on affected jobs:

```json
{
  "id": "web-app.production.release",
  "dependsOn": [],
  "gates": [
    {
      "type": "environment-promotion",
      "environment": "staging",
      "component": "web-app",
      "condition": "success",
      "match": { "revision": "source" }
    }
  ]
}
```

Gates are evidence checks that require the component to have previously succeeded in the referenced environment for the same source revision. When both environments are in the same plan, promotion compiles into standard `dependsOn` edges instead.

Read [execution model](./execution-model.md) next to see how plans are previewed and executed.