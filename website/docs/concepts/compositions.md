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

The recommended package format uses a `stack.yaml` manifest and a `compositions/` subdirectory tree. Each composition type lives in its own subdirectory using split-kind authoring:

```text
my-platform/
├── stack.yaml
└── compositions/
    ├── terraform/
    │   ├── composition.yaml
    │   ├── schema.yaml
    │   ├── jobs/
    │   │   └── terraform-validate.yaml
    │   └── profiles/
    │       ├── terraform-pull-request.yaml
    │       ├── terraform-verify.yaml
    │       └── terraform-release.yaml
    └── helm-chart/
        ├── composition.yaml
        ├── schema.yaml
        ├── jobs/
        │   └── helm-chart-render.yaml
        └── profiles/
            ├── helm-chart-lint-only.yaml
            └── helm-chart-verify.yaml
```

`stack.yaml` declares package metadata and an optional OCI registry target. When `spec.compositions` is omitted, the packager automatically discovers composition files by walking the directory tree — no explicit path listing is needed.

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

## Document kinds

Compositions use a multi-kind authoring model where each concern has its own document type:

| Kind | Purpose | Changes when |
|------|---------|-------------|
| `Composition` | Type binding, defaults, references | You introduce or rename a component type |
| `ComponentSchema` | Input validation contract | Component contract changes |
| `JobTemplate` | Steps, capabilities, runner defaults | Runtime implementation changes |
| `ExecutionProfile` | Behavior overlay per trigger/env | PR/release/env behavior changes |

The `Composition` document is the public facade that references the other kinds by name:

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: Composition
metadata:
  name: terraform
spec:
  type: terraform
  description: Terraform validation jobs for infra components
  schemaRef:
    name: terraform-component
  defaultJob: validate
  defaultProfile: verify
  jobs:
    - name: validate
      templateRef:
        name: terraform-validate
  profiles:
    - name: pull-request
      profileRef:
        name: terraform-pull-request
    - name: verify
      profileRef:
        name: terraform-verify
    - name: release
      profileRef:
        name: terraform-release
```

## Capability-based step selection

`JobTemplate` steps are tagged with a `capability` field. Profiles select which steps to run using `includeCapabilities` rather than brittle step ID lists:

```yaml
# JobTemplate step
- id: plan
  name: plan
  capability: terraform.plan
  run: terraform plan -no-color

# ExecutionProfile selects it
spec:
  jobs:
    validate:
      includeCapabilities:
        - terraform.setup
        - terraform.plan
```

This makes profiles resilient to step ID renames and additions.

## Profile step overrides

Profiles can patch specific step fields without duplicating the entire step definition:

```yaml
spec:
  jobs:
    validate:
      stepOverrides:
        init:
          run: terraform init -backend=false
        plan:
          run: terraform plan -no-color -lock=false
```

## Profile policies

Release profiles can enforce rules before execution:

```yaml
spec:
  policies:
    requireCleanGitTree: true
    requirePinnedTerraformVersion: true
    requireApproval: true
```

## Inline authoring

For simpler compositions, everything can live in a single file with inline schemas and jobs. This is still supported but split-kind is recommended for compositions with multiple profiles or reusable jobs.

## Why compositions scale well

- They keep execution logic centralized instead of duplicating shell scripts across repositories.
- They let platform teams publish stricter schemas without changing every intent file.
- They make plan generation deterministic because source resolution is explicit and lockable.
- They support multiple runtime backends because the compile step is separate from execution.
- Split-kind authoring lets each concern evolve independently with clear ownership boundaries.

## Inspecting compositions from the CLI

```bash
orun compositions --intent examples/intent.yaml
orun compositions terraform --intent examples/intent.yaml
orun pack --root examples/compositions
orun publish --root examples/compositions
```

Read [Stacks](stacks.md) to understand the packaging and distribution model, or [composition contract](../compositions/composition-contract.md) when you are ready to author your own type.
