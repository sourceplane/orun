---
title: Composition contract
---

A composition source exports self-describing documents. The primary contract is a `Composition` document grouped under a Stack package.

## Composition document

A `Composition` document defines both the validation contract and the execution contract for a type. It lives at `compositions/<type>/compositions.yaml` within a Stack package.

Required fields:

- `metadata.name`
- `spec.type`
- `spec.defaultJob`
- `spec.inputSchema`
- `spec.jobs`

Example:

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: Composition
metadata:
  name: helm
spec:
  type: helm
  defaultJob: deploy
  inputSchema:
    $schema: http://json-schema.org/draft-07/schema#
    type: object
    properties:
      type:
        const: helm
      inputs:
        type: object
        properties:
          chart:
            type: string
          timeout:
            type: string
  jobs:
    - name: deploy
      runsOn: ubuntu-22.04
      timeout: 15m
      retries: 2
      steps:
        - name: deploy
          run: helm upgrade --install {{.Component}} {{.chart}}
```

`defaultJob` must be explicit. Orun no longer relies on implicit "first job wins" behavior for packaged compositions.

## Stack manifest

A `Stack` manifest (`stack.yaml`) declares package metadata and an optional OCI registry target. Compositions are discovered automatically by walking the directory tree for `compositions.yaml` files — no explicit `spec.compositions` listing is required.

```yaml
apiVersion: orun.io/v1
kind: Stack
metadata:
  name: my-platform-stack
  version: 1.0.0
  description: Platform compositions
  owner: my-org
registry:
  host: ghcr.io
  namespace: my-org
  repository: my-platform-stack
  visibility: public
```

Package layout:

```text
my-platform/
├── stack.yaml
└── compositions/
    ├── terraform/
    │   └── compositions.yaml
    └── helm-chart/
        └── compositions.yaml
```

To pin specific files instead of relying on auto-discovery, add an explicit `spec.compositions` list:

```yaml
spec:
  compositions:
    - path: compositions/terraform/compositions.yaml
    - path: compositions/helm-chart/compositions.yaml
```

## Template inputs

Job steps resolve against merged component data. Common template values include:

- `.Component`
- merged input fields such as `.chart`, `.namespacePrefix`, or `.workspace`

Keep templates simple and deterministic. If the same input is required across many components, express it as schema or job defaults instead of repeating it in every component manifest.

## Execution semantics

Jobs can declare:

- `runsOn`
- `timeout`
- `retries`
- `labels`
- `steps`

Steps can declare:

- `run` or `use`
- `phase` and `order`
- `retry`
- `timeout`
- `onFailure`

The compiler resolves those fields into the plan artifact, and the runtime consumes them later.

The legacy `<type>/schema.yaml` plus `<type>/job.yaml` layout is still accepted through `--config-dir`, but new authoring should target Stack packages with `compositions.yaml` documents.
