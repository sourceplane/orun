---
title: Runtime environment
---

orun resolves environment variables at plan time and injects them into every job and step at runtime. This page explains how `env` declarations are merged, what ORUN-prefixed variables are injected automatically, and the full precedence order.

## Declaring environment variables

### Intent-level env

Define environment variables that apply to all components running in a given environment:

```yaml
# intent.yaml
environments:
  dev:
    env:
      AWS_REGION: us-east-1
      TF_LOG: WARN
      NAMESPACE_PREFIX: dev-
  production:
    env:
      AWS_REGION: us-west-2
      TF_LOG: ERROR
```

### Component subscription-level env

Override or extend environment variables for a specific component within an environment:

```yaml
# component.yaml
spec:
  type: terraform
  subscribe:
    environments:
      - name: dev
        env:
          STACK_NAME: api-platform
          TF_VAR_replicas: "1"
      - name: production
        env:
          STACK_NAME: api-platform
          TF_VAR_replicas: "3"
```

## Merge precedence

Environment variables are merged once during plan compilation. When the same key appears at multiple levels, higher-precedence values win:

| Priority | Source | Example |
| --- | --- | --- |
| 1 (lowest) | Intent environment `env` | `environments.dev.env.AWS_REGION` |
| 2 | Component subscription `env` | `subscribe.environments[name=dev].env.AWS_REGION` |
| 3 | ORUN runtime variables | `ORUN_ENVIRONMENT`, `ORUN_COMPONENT` |
| 4 | Step-level `env` | Step definition in composition |
| 5 (highest) | OS environment | Inherited from parent process |

### Example resolution

Given:

```yaml
# intent.yaml
environments:
  dev:
    env:
      AWS_REGION: us-east-1
      TF_LOG: WARN
```

```yaml
# component.yaml (api-platform)
subscribe:
  environments:
    - name: dev
      env:
        AWS_REGION: eu-west-1
        STACK_NAME: api-platform
```

The resolved runtime environment for the `api-platform` component in `dev` is:

```
AWS_REGION=eu-west-1          # subscription overrides intent
TF_LOG=WARN                   # from intent (no override)
STACK_NAME=api-platform       # from subscription
ORUN_ENVIRONMENT=dev          # injected by runtime
ORUN_COMPONENT=api-platform   # injected by runtime
ORUN_PLAN_ID=abc1234          # injected by runtime
ORUN_JOB_ID=api-platform.dev.deploy
```

## ORUN-prefixed runtime variables

These are injected automatically by the runner and cannot be overridden by user-declared `env`:

| Variable | Description |
| --- | --- |
| `ORUN_CONTEXT` | Runtime context label (`local`, `container`, `ci`) |
| `ORUN_RUNNER` | Resolved runner name |
| `ORUN_EXEC_ID` | Execution ID for the current run |
| `ORUN_PLAN_ID` | Plan checksum short-hash |
| `ORUN_JOB_ID` | Current job ID |
| `ORUN_JOB_UID` | Stable job UID |
| `ORUN_JOB_RUN_ID` | Cross-job traceability identifier |
| `ORUN_ENVIRONMENT` | Environment name for the current job |
| `ORUN_COMPONENT` | Component name for the current job |

## Template interpolation

Env values support the same template variables as component inputs:

```yaml
environments:
  staging:
    env:
      NAMESPACE: "{{ .environment }}-{{ .component }}"
      DOMAIN_PREFIX: "{{ .group }}"
```

Available variables: `{{ .environment }}`, `{{ .component }}`, `{{ .group }}`.

## Relationship to inputs

`env` and `inputs` serve different purposes:

- **inputs** (`spec.inputs` in component.yaml) are configuration values used for template rendering in composition steps. They appear in `PlanJob.config`.
- **env** is a flat `map[string]string` of shell environment variables injected at runtime. They appear in `PlanJob.env`.

When no `env` is declared, `PlanJob.env` falls back to the component inputs for backwards compatibility with existing compositions that reference inputs as environment variables.

## Plan output

The resolved environment variables appear in the plan under each job's `env` field:

```json
{
  "jobs": [
    {
      "id": "api-platform.dev.deploy",
      "component": "api-platform",
      "environment": "dev",
      "env": {
        "AWS_REGION": "eu-west-1",
        "TF_LOG": "WARN",
        "STACK_NAME": "api-platform"
      },
      "config": {
        "stackName": "api-platform",
        "terraformDir": ".",
        "terraformVersion": "1.9.8"
      }
    }
  ]
}
```
