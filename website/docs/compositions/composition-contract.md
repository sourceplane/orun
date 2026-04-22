---
title: Composition contract
---

A composition source exports self-describing documents. The primary contract is a `Composition` document, optionally grouped under a `CompositionPackage` manifest.

## Composition document

A `Composition` document defines both the validation contract and the execution contract for a type.

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

`defaultJob` must be explicit. Gluon no longer relies on implicit "first job wins" behavior for packaged compositions.

## Composition package document

A `CompositionPackage` groups exported compositions and makes package contents discoverable.

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: CompositionPackage
metadata:
  name: platform-core
spec:
  version: 1.0.0
  exports:
    - composition: helm
      path: compositions/helm.yaml
    - composition: terraform
      path: compositions/terraform.yaml
```

## Template inputs

Job steps still resolve against merged component data. In the built-in examples, common template values include:

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

The legacy `<type>/schema.yaml` plus `<type>/job.yaml` layout is still accepted through `--config-dir`, but new authoring should target packaged `Composition` documents.