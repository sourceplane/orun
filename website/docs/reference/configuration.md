---
title: Configuration
---

`arx` configuration is split across three user-facing surfaces:

1. `intent.yaml`
2. discovered `component.yaml` manifests
3. composition assets referenced by `--config-dir`

## Intent file

Minimal example:

```yaml
apiVersion: sourceplane.io/v1
kind: Intent

metadata:
  name: demo

discovery:
  roots:
    - services/

environments:
  development:
    defaults:
      region: us-east-1
```

The intent file is where you define environments, discovery roots, groups, selectors, defaults, and optional inline components.

## Component manifest

Minimal example:

```yaml
apiVersion: sourceplane.io/v1
kind: Component

metadata:
  name: web-app

spec:
  type: helm
  domain: platform
  subscribe:
    environments: [development, staging, production]
  inputs:
    chart: oci://registry.example.com/charts/web-app
```

Components carry type-specific inputs, labels, overrides, and dependency declarations.

## Composition assets

Pass the composition directory through the global `--config-dir` flag:

```bash
arx plan --intent intent.yaml --config-dir assets/config/compositions
```

Each type directory contains a schema and a job registry. That is where type validation and runtime job definitions live.

## Environment-specific control

Use CLI flags when you want to scope the effective configuration at compile time:

- `--env` to filter a single environment
- `--changed` and related flags for change-aware planning
- `--view` for DAG-focused render views

Read [environment variables](./environment-variables.md) if you want to control configuration through the shell.