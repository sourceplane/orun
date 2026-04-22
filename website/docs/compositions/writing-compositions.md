---
title: Writing compositions
---

Author a new composition package when you want to introduce a new component type without changing the core planner.

## 1. Create a package root

```text
my-platform/
├── gluon.yaml
└── compositions/
    └── mytype.yaml
```

The package manifest exports one or more composition documents.

## 2. Define the package manifest

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: CompositionPackage
metadata:
  name: my-platform
spec:
  version: 1.0.0
  exports:
    - composition: mytype
      path: compositions/mytype.yaml
```

## 3. Define the composition document

Start by expressing the type contract and jobs in a single `Composition` document.

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: Composition
metadata:
  name: mytype
spec:
  type: mytype
  defaultJob: apply
  inputSchema:
    $schema: http://json-schema.org/draft-07/schema#
    type: object
    properties:
      type:
        const: mytype
      inputs:
        type: object
        properties:
          target:
            type: string
          timeout:
            type: string
        required:
          - target
  jobs:
    - name: apply
      runsOn: ubuntu-22.04
      steps:
        - name: apply
          run: mytool apply {{.target}}
```

That makes validation fail early before you have to debug runtime behavior.

## 4. Test with an example component

Create a sample component manifest and point it at the new type.

```yaml
apiVersion: sourceplane.io/v1
kind: Component

metadata:
  name: demo

spec:
  type: mytype
  subscribe:
    environments: [development]
  inputs:
    target: demo-service
```

## 5. Validate and inspect

```bash
gluon compositions package build --root ./my-platform --output /tmp/my-platform.tgz
gluon validate --intent examples/intent.yaml
gluon compositions mytype --intent examples/intent.yaml
gluon plan --intent examples/intent.yaml --output /tmp/mytype-plan.json
```

In the consuming intent, declare the package under `compositions.sources` using `kind: dir`, `kind: archive`, or `kind: oci`.

## Authoring guidelines

- Keep schemas strict enough to reject invalid inputs early.
- Put reusable defaults in schemas or jobs, not in every component.
- Prefer explicit step phases and timeouts when ordering matters.
- Set `defaultJob` intentionally instead of depending on job order.
- Test the composition with dry-run first, then execute through the runtime you expect to use in CI.

The legacy `--config-dir` flow still works for folder-shaped compositions, but new authoring should target packaged `Composition` documents.