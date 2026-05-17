---
title: Runtime environment
---

orun resolves environment variables at plan time and injects them into every job and step at runtime. This page explains how `env` declarations are merged, what ORUN-prefixed variables are injected automatically, and the full precedence order.

## Declaring environment variables

Environment variables can be declared at four levels, giving platform teams and component authors fine-grained control over runtime context.

### Intent root-level env

Define global environment variables shared across all environments and components:

```yaml
# intent.yaml
apiVersion: sourceplane.io/v1
kind: Intent

metadata:
  name: aws-admin

env:
  OWNER: sourceplane
  ORGANIZATION: sourceplane
```

### Intent environment-level env

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

### Component root-level env

Define environment variables that apply to a component across all its subscribed environments:

```yaml
# component.yaml
spec:
  type: terraform
  env:
    REPO: aws-admin
    SERVICE: github-iam
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
| 1 (lowest) | Intent root `env` | `env.OWNER` |
| 2 | Intent environment `env` | `environments.dev.env.AWS_REGION` |
| 3 | Component root `env` | `spec.env.REPO` |
| 4 | Component subscription `env` | `subscribe.environments[name=dev].env.STACK_NAME` |
| 5 | ORUN runtime variables | `ORUN_ENVIRONMENT`, `ORUN_COMPONENT` |
| 6 | Step-level `env` | Step definition in composition |
| 7 (highest) | OS environment | Inherited from parent process |

### Example resolution

Given:

```yaml
# intent.yaml
env:
  OWNER: sourceplane
  ORGANIZATION: sourceplane

environments:
  dev:
    env:
      AWS_REGION: us-east-1
      NAMESPACE_PREFIX: dev-
```

```yaml
# component.yaml (api-platform)
env:
  REPO: aws-admin

subscribe:
  environments:
    - name: dev
      env:
        STACK_NAME: api-platform
```

The resolved runtime environment for the `api-platform` component in `dev` is:

```
OWNER=sourceplane              # from intent root
ORGANIZATION=sourceplane       # from intent root
AWS_REGION=us-east-1           # from intent environment
NAMESPACE_PREFIX=dev-          # from intent environment
REPO=aws-admin                 # from component root
STACK_NAME=api-platform        # from component subscription
ORUN_ENVIRONMENT=dev           # injected by runtime
ORUN_COMPONENT=api-platform    # injected by runtime
ORUN_PLAN_ID=abc1234           # injected by runtime
ORUN_JOB_ID=api-platform.dev.deploy
```

## Reserved ORUN_ prefix

User-defined environment variables must not start with `ORUN_`. This prefix is reserved for runtime-injected system variables. If a user-declared env key uses the `ORUN_` prefix, plan generation will fail with a validation error.

This applies to all four declaration levels (intent root, environment, component root, and subscription).

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
| `ORUN_ENV` | Path to env file for persisting variables across steps (alias for `GITHUB_ENV`) |

## Template interpolation

Env values support the same template variables as component parameters:

```yaml
environments:
  staging:
    env:
      NAMESPACE: "{{ .environment }}-{{ .component }}"
      DOMAIN_PREFIX: "{{ .group }}"
```

Available variables: `{{ .environment }}`, `{{ .component }}`, `{{ .group }}`.

## Relationship to parameters

`env` and `parameters` serve different purposes:

- **parameters** (`spec.parameters` in component.yaml) are configuration values used for template rendering in composition steps. They appear in `PlanJob.parameters` and are accessed via `{{ .parameters.fieldName }}` in templates.
- **env** is a flat `map[string]string` of shell environment variables injected at runtime. They appear in `PlanJob.env` and are accessible via `{{ .env.VAR_NAME }}` in templates.

## Plan output

The resolved environment variables appear in the plan under each job's `env` field. Keys are sorted alphabetically for deterministic output:

```json
{
  "jobs": [
    {
      "id": "api-platform.dev.deploy",
      "component": "api-platform",
      "environment": "dev",
      "env": {
        "AWS_REGION": "us-east-1",
        "NAMESPACE_PREFIX": "dev-",
        "ORGANIZATION": "sourceplane",
        "OWNER": "sourceplane",
        "REPO": "aws-admin",
        "STACK_NAME": "api-platform"
      },
      "parameters": {
        "stackName": "api-platform",
        "terraformDir": ".",
        "terraformVersion": "1.9.8"
      }
    }
  ]
}
```

## Dynamic environment propagation with ORUN_ENV

Steps can persist environment variables for subsequent steps by appending `KEY=VALUE` pairs to the file at `$ORUN_ENV`:

```bash
echo "DEPLOY_SHA=$(git rev-parse HEAD)" >> "$ORUN_ENV"
```

This uses the same file-based mechanism as GitHub Actions' `$GITHUB_ENV`. Both variables point to the same file — writing to either one has the same effect. Using `ORUN_ENV` is preferred in compositions because it is portable and does not depend on GitHub-specific naming.

Multi-line values use heredoc syntax:

```bash
cat >> "$ORUN_ENV" <<'HEREDOC'
MULTI_LINE_VAR<<EOF
line one
line two
EOF
HEREDOC
```

Variables written to `$ORUN_ENV` are available to all subsequent steps in the same job but not to other jobs.
