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
  subscribe:
    environments: [development, staging, production]
  inputs:
    stackName: network-foundation
    terraformDir: .
```

Components carry type-specific inputs, labels, overrides, and dependency declarations.

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

## Execution profiles

Profiles are named execution configurations that control which steps run. Define them inline in `intent.yaml`:

```yaml
execution:
  profiles:
    dry-run:
      description: Non-mutating checks only
      compositionRef: terraform
      plan:
        scope: changed
      controls:
        terraform:
          apply:
            enabled: false

    verify:
      description: Full verification with plan generation
      plan:
        scope: changed
      controls:
        terraform:
          plan:
            enabled: true
          apply:
            enabled: false

    release:
      description: Release-grade execution
      plan:
        scope: full
      controls:
        terraform:
          apply:
            enabled: true
```

Profiles can also be loaded from stack packages via convention-over-configuration. See [execution profiles](../concepts/profiles.md) for the full model.

## Automation triggers

Triggers connect CI events to profile selection. Define them in `intent.yaml`:

```yaml
automation:
  triggers:
    - name: github-push-main
      on:
        provider: github
        event: push
        branches: [main]
      plan:
        profile: verify

  triggerBindings:
    - name: github-pull-request
      on:
        provider: github
        event: pull_request
        actions: [opened, synchronize, reopened, ready_for_review]
      plan:
        scope: changed
        base: main
```

Triggers can also be loaded from stack packages (`triggers/*.yaml`). See [automation](../concepts/automation.md) for trigger matching and CI auto-detection.

## Environment execution settings

Each environment can declare profile selection and control overrides:

```yaml
environments:
  production:
    execution:
      profile: release
      profiles:
        cloudflare-worker: deploy
        cloudflare-pages: deploy
      controlOverrides:
        terraform:
          apply:
            enabled: true
    defaults:
      namespacePrefix: prod-
```

| Field | Meaning |
| --- | --- |
| `execution.profile` | Default profile for all composition types in this environment |
| `execution.profiles.<type>` | Override profile for a specific composition type |
| `execution.controlOverrides.<type>` | Direct control overrides (highest precedence after component-level) |
