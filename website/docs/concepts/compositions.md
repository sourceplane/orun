---
title: Compositions
---

Compositions are the execution contract between desired state and runtime behavior. A component type such as `helm` or `terraform` resolves to a self-describing `Composition` document exported by a declared composition source.

## Declaring composition sources

The primary workflow is to declare sources in `intent.yaml`:

```yaml
compositions:
  sources:
    - name: example-platform
      kind: dir
      path: ./compositions
```

Supported source kinds are `dir`, `archive`, and `oci`.

## Package shape — Stack format

The recommended package format uses a `stack.yaml` manifest and a `compositions/` subdirectory tree. Each composition type lives in its own subdirectory and contains a single `compositions.yaml` file:

```text
my-platform/
├── stack.yaml
└── compositions/
    ├── terraform/
    │   └── compositions.yaml
    ├── helm-values/
    │   └── compositions.yaml
    ├── helm-chart/
    │   └── compositions.yaml
    └── cloudflare-worker/
        └── compositions.yaml
```

`stack.yaml` declares package metadata and an optional OCI registry target. When `spec.compositions` is omitted, the packager automatically discovers every `compositions.yaml` file by walking the directory tree — no explicit path listing is needed.

```yaml
apiVersion: orun.io/v1
kind: Stack
metadata:
  name: my-platform-stack
  version: 1.0.0
  description: Platform compositions for my-platform
  owner: my-org
registry:
  host: ghcr.io
  namespace: my-org
  repository: my-platform-stack
  visibility: public
```

See [Stacks](stacks.md) for the full Stack concept guide including packaging and remote distribution.

## What lives in a composition

Each exported `Composition` document carries:

- `spec.type` for the component type name
- `spec.defaultJob` for explicit default job binding
- `spec.inputSchema` for input validation
- `spec.jobs` for portable job definitions

## Example: Helm composition

```yaml
spec:
  type: helm
  defaultJob: deploy
  inputSchema:
    type: object
    properties:
      inputs:
        type: object
        properties:
          chart:
            type: string
  jobs:
    - name: deploy
      runsOn: ubuntu-22.04
      timeout: 15m
      retries: 2
      steps:
        - name: deploy
          run: helm upgrade --install {{.Component}} {{.chart}} --namespace {{.namespacePrefix}}{{.Component}}
```

## Why compositions scale well

- They keep execution logic centralized instead of duplicating shell scripts across repositories.
- They let platform teams publish stricter schemas without changing every intent file.
- They make plan generation deterministic because source resolution is explicit and lockable.
- They support multiple runtime backends because the compile step is separate from execution.

## Inspecting compositions from the CLI

```bash
orun compositions --intent examples/intent.yaml
orun compositions terraform --intent examples/intent.yaml
orun pack --root examples/compositions
orun publish --root examples/compositions
```

The legacy `--config-dir` flag still works for folder-shaped compositions during migration, but packaged Stack sources are the recommended path.

Read [Stacks](stacks.md) to understand the packaging and distribution model, or [composition contract](../compositions/composition-contract.md) when you are ready to author your own type.
