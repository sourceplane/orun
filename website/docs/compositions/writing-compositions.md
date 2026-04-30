---
title: Writing compositions
---

Author a new composition package when you want to introduce a new component type without changing the core planner.

## 1. Create a Stack package root

The recommended format is a `stack.yaml` manifest with a `compositions/` subdirectory. Each composition type lives in its own subdirectory containing a single `compositions.yaml` file.

```text
my-platform/
├── stack.yaml
└── compositions/
    └── mytype/
        └── compositions.yaml
```

## 2. Define the Stack manifest

`stack.yaml` describes the package and an optional OCI registry target. When `spec.compositions` is omitted, the packager auto-discovers every `compositions.yaml` file by walking the directory tree — no explicit path listing is needed.

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

To pin specific files instead of using auto-discovery, add a `spec.compositions` block:

```yaml
spec:
  compositions:
    - path: compositions/mytype/compositions.yaml
    - path: compositions/othertype/compositions.yaml
```

## 3. Define the composition document

Start by expressing the type contract and jobs in a single `Composition` document at `compositions/mytype/compositions.yaml`.

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
orun pack --root ./my-platform
orun validate --intent examples/intent.yaml
orun compositions mytype --intent examples/intent.yaml
orun plan --intent examples/intent.yaml --output /tmp/mytype-plan.json
```

In the consuming intent, declare the package under `compositions.sources` using `kind: dir`, `kind: archive`, or `kind: oci`.

## 6. Publish to a registry

After validating locally, publish the Stack to an OCI registry so others can reference it remotely:

```bash
orun login ghcr.io
orun publish --root ./my-platform
```

Teams consuming the stack reference it by OCI ref in their `intent.yaml`:

```yaml
compositions:
  sources:
    - name: my-platform
      kind: oci
      ref: oci://ghcr.io/my-org/my-platform-stack:1.0.0
```

See [Stacks](../concepts/stacks.md) for the full packaging and distribution guide.

## Authoring guidelines

- Keep schemas strict enough to reject invalid inputs early.
- Put reusable defaults in schemas or jobs, not in every component.
- Prefer explicit step phases and timeouts when ordering matters.
- Set `defaultJob` intentionally instead of depending on job order.
- Test the composition with dry-run first, then execute through the runtime you expect to use in CI.

The legacy `--config-dir` flow still works for folder-shaped compositions, but new authoring should target Stack packages with `compositions.yaml` documents.
