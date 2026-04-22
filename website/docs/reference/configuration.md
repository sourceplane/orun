---
title: Configuration
---

`gluon` configuration is split across three user-facing surfaces:

1. `intent.yaml`
2. discovered `component.yaml` manifests
3. composition sources declared by `intent.compositions`

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

It also declares where compositions come from:

```yaml
compositions:
  sources:
    - name: platform-core
      kind: dir
      path: ./packages/platform-core
```

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

## Composition sources

Declare composition sources in the intent and plan directly against that intent:

```bash
gluon plan --intent intent.yaml
```

Each source can be a local package directory, a packaged archive, or an OCI reference. Gluon resolves those sources into a cache and writes a lock file under `<intent-dir>/.gluon/compositions.lock.yaml`.

`--config-dir` still works as a compatibility fallback for legacy folder-shaped compositions.

## Environment-specific control

Use CLI flags when you want to scope the effective configuration at compile time:

- `--env` to filter a single environment
- `--changed` and related flags for change-aware planning
- `--view` for DAG-focused render views

Read [environment variables](./environment-variables.md) if you want to control configuration through the shell.