---
title: Compositions
---

Compositions are the execution contract between desired state and runtime behavior. A component type such as `helm` or `terraform` resolves to a self-describing `Composition` document exported by a declared composition source.

## Declaring composition sources

The primary workflow is to declare sources in `intent.yaml`:

```yaml
compositions:
  sources:
    - name: platform-core
      kind: dir
      path: ./packages/platform-core
```

Supported source kinds are `dir`, `archive`, and `oci`.

## Package shape

The repository quick-start package looks like this:

```text
examples/packages/platform-core/
├── gluon.yaml
├── compositions/
│   ├── charts.yaml
│   ├── helm.yaml
│   ├── helmCommon.yaml
│   └── terraform.yaml
└── docs/
    └── README.md
```

`gluon.yaml` is a `CompositionPackage` document that maps exported composition names to files inside the package.

## What lives in a composition

Each exported `Composition` document carries:

- `spec.type` for the component type name
- `spec.defaultJob` for explicit default job binding
- `spec.inputSchema` for input validation
- `spec.jobs` for portable job definitions

## Example: Helm composition

The repository's packaged `helm` composition still defines a deploy workflow with retries, timeouts, and templated commands:

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
gluon compositions --intent examples/intent.yaml
gluon compositions helm --intent examples/intent.yaml
gluon compositions package build --root examples/packages/platform-core --output /tmp/platform-core.tgz
```

The legacy `--config-dir` flag still works for folder-shaped compositions during migration, but packaged sources are the recommended path.

Read [composition contract](../compositions/composition-contract.md) when you are ready to author your own type.