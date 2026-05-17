---
title: Configuration
---

`orun` configuration is split across three user-facing surfaces:

1. `intent.yaml`
2. discovered `component.yaml` manifests
3. composition sources declared by `intent.compositions`
4. local CLI config in `~/.orun/config.yaml` for backend defaults and repo links

## Intent file

Minimal example:

```yaml
apiVersion: sourceplane.io/v1
kind: Intent

metadata:
  name: demo

env:
  OWNER: sourceplane

discovery:
  roots:
    - services/

environments:
  development:
    parameterDefaults:
      "*":
        region: us-east-1
    env:
      AWS_REGION: us-east-1
```

The intent file is where you define environments, discovery roots, groups, selectors, defaults, and optional inline components. Root-level `env` provides global environment variables shared across all environments.

It also declares where compositions come from:

```yaml
compositions:
  sources:
    - name: example-platform
      kind: dir
      path: ./compositions
```

## Component manifest

Minimal example:

```yaml
apiVersion: sourceplane.io/v1
kind: Component

metadata:
  name: network-foundation

spec:
  type: terraform
  domain: platform-foundation
  env:
    REPO: network-foundation
  subscribe:
    environments: [development, staging, production]
  parameters:
    stackName: network-foundation
    terraformDir: .
```

Components carry type-specific inputs, root-level environment variables, labels, overrides, and dependency declarations. Root-level `env` applies across all subscribed environments and can be overridden per subscription.

## Composition sources

Declare composition sources in the intent and plan directly against that intent:

```bash
orun plan --intent intent.yaml
```

Each source can be a local package directory, a packaged archive, or an OCI reference. Orun resolves those sources into a cache and writes a lock file under `<intent-dir>/.orun/compositions.lock.yaml`.

`--config-dir` still works as a compatibility fallback for legacy folder-shaped compositions.

## Environment-specific control

Use CLI flags when you want to scope the effective configuration at compile time:

- `--env` to filter a single environment
- `--changed` and related flags for change-aware planning
- `--view` for DAG-focused render views

Read [environment variables](./environment-variables.md) if you want to control configuration through the shell.

## Environment promotion

Declare promotion dependencies between environments to establish deployment ordering:

```yaml
environments:
  staging:
    activation:
      triggerRefs: [github-push-main]
    promotion:
      dependsOn:
        - environment: preview
          strategy: same-component      # default
          condition: success            # default
          satisfy: same-plan-or-previous-success  # default
          match:
            revision: source            # default
    parameterDefaults:
      "*":
        lane: verify
```

| Field | Values | Default | Meaning |
| --- | --- | --- | --- |
| `environment` | string (required) | — | Environment that must succeed first |
| `strategy` | `same-component`, `environment-barrier` | `same-component` | How to link jobs across environments |
| `condition` | `success` | `success` | Required outcome in the dependency environment |
| `satisfy` | `same-plan`, `previous-success`, `same-plan-or-previous-success` | `same-plan-or-previous-success` | Whether to use DAG edges, gates, or both |
| `match.revision` | `source` | `source` | How to match prior success evidence |

See [environment promotion](../concepts/environment-promotion.md) for detailed behavior.

## Remote state configuration

Add an `execution.state` block to `intent.yaml` to enable remote state coordination via orun-backend:

```yaml
execution:
  state:
    mode: remote
    backendUrl: https://orun-backend.example.com
```

| Field | Values | Meaning |
| --- | --- | --- |
| `mode` | `local` (default) or `remote` | Where execution state is stored |
| `backendUrl` | URI | URL of the orun-backend instance (required when `mode: remote`) |

The `backendUrl` can also be supplied via `--backend-url` or `ORUN_BACKEND_URL`; those take priority over the intent file.

When neither the flag, environment variable, nor intent file sets a backend URL, `orun` falls back to `~/.orun/config.yaml`:

```yaml
backend:
  url: https://orun-api.example.workers.dev

repos:
  - backendUrl: https://orun-api.example.workers.dev
    repoFullName: sourceplane/orun
    namespaceId: "123456789"
```

`orun cloud link` writes the `repos` entries used for local session-authenticated remote-state runs.

When `mode: remote` is set, all three commands that read execution state (`run`, `status`, `logs`) automatically use the backend without requiring `--remote-state` on the command line.
