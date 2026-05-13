---
title: Composition contract
---

A composition source exports self-describing documents. The primary contract is a `Composition` document grouped under a Stack package. Compositions support both inline authoring (single file) and split-kind authoring (multiple files).

## Document kinds

| Kind | Purpose | Required fields |
|------|---------|----------------|
| `Composition` | Type binding, defaults, references | `metadata.name`, `spec.type`, `spec.defaultJob`, `spec.jobs` |
| `ComponentSchema` | Input validation contract | `metadata.name`, `spec.type`, `spec.schema` |
| `JobTemplate` | Reusable execution template | `metadata.name`, `spec.steps` |
| `ExecutionProfile` | Behavior overlay per context | `metadata.name`, `spec.jobs` |

## Composition document

A `Composition` is the public facade for a component type. It can define everything inline or reference split-kind documents via `schemaRef`, `templateRef`, and `profileRef`.

Required fields (inline):

- `metadata.name`
- `spec.type`
- `spec.defaultJob`
- `spec.inputSchema` or `spec.schemaRef`
- `spec.jobs`

Example (split-kind):

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

Example (inline):

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
  jobs:
    - name: deploy
      runsOn: ubuntu-22.04
      timeout: 15m
      retries: 2
      steps:
        - name: deploy
          run: helm upgrade --install {{.Component}} {{.chart}}
```

`defaultJob` must be explicit. Orun no longer relies on implicit "first job wins" behavior.

## ComponentSchema document

Defines the JSON Schema validation contract for component inputs. Referenced by `schemaRef.name` in the Composition.

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: ComponentSchema
metadata:
  name: terraform-component
spec:
  type: terraform
  schema:
    $schema: http://json-schema.org/draft-07/schema#
    type: object
    required: [name, type, inputs]
    properties:
      name:
        type: string
      type:
        const: terraform
      inputs:
        type: object
        required: [stackName, terraformDir]
        properties:
          stackName:
            type: string
          terraformDir:
            type: string
          terraformVersion:
            type: string
        additionalProperties: false
    additionalProperties: true
```

## JobTemplate document

Defines a reusable job with capability-tagged steps. Referenced by `templateRef.name` in the Composition's `jobs` list.

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: JobTemplate
metadata:
  name: terraform-validate
spec:
  description: Validate Terraform stacks
  runsOn: ubuntu-22.04
  timeout: 20m
  retries: 0
  labels:
    scope: infra
  capabilities:
    - terraform.setup
    - terraform.fmt
    - terraform.init
    - terraform.validate
    - terraform.plan
  steps:
    - id: setup
      name: setup
      capability: terraform.setup
      use: hashicorp/setup-terraform@v4
      with:
        terraform_version: "{{.terraformVersion}}"
    - id: fmt
      name: fmt
      capability: terraform.fmt
      run: terraform fmt -check
      onFailure: stop
    - id: init
      name: init
      capability: terraform.init
      run: terraform init -backend=false
      onFailure: stop
    - id: validate
      name: validate
      capability: terraform.validate
      run: terraform validate -no-color
      onFailure: stop
    - id: plan
      name: plan
      capability: terraform.plan
      run: terraform plan -no-color
      onFailure: stop
```

The `capability` field on each step enables semantic selection by profiles.

## ExecutionProfile document

Defines which steps to run and how to override them for a specific execution context. Referenced by `profileRef.name` in the Composition's `profiles` list.

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: ExecutionProfile
metadata:
  name: terraform-pull-request
spec:
  description: Fast PR validation with speculative planning
  jobs:
    validate:
      includeCapabilities:
        - terraform.setup
        - terraform.fmt
        - terraform.init
        - terraform.validate
        - terraform.plan
      stepOverrides:
        init:
          run: terraform init -backend=false
        plan:
          run: terraform plan -no-color -lock=false
```

Profiles can also declare `policies` for enforcement:

```yaml
spec:
  policies:
    requireCleanGitTree: true
    requirePinnedTerraformVersion: true
    requireApproval: true
```

### Profile step selection

Profiles select which steps to run using one of two mechanisms:

- **`includeCapabilities`** (recommended): Selects steps by their `capability` tag. This is resilient to step ID renames.
- **`stepsEnabled`** (legacy): Selects steps by their `id` or `name`. Works with inline compositions that don't use capabilities.

### Profile step overrides

`stepOverrides` patches specific step fields without duplicating the entire step definition:

```yaml
stepOverrides:
  init:
    run: terraform init -backend=false
  plan:
    with:
      lock: "false"
```

## Stack manifest

A `Stack` manifest (`stack.yaml`) declares package metadata and an optional OCI registry target.

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

Package layout (split-kind):

```text
my-platform/
├── stack.yaml
└── compositions/
    └── terraform/
        ├── composition.yaml
        ├── schema.yaml
        ├── jobs/
        │   └── terraform-validate.yaml
        └── profiles/
            ├── terraform-pull-request.yaml
            ├── terraform-verify.yaml
            └── terraform-release.yaml
```

## Template inputs

Job steps resolve against merged component data. Common template values:

- `.Component` — component name
- `.Environment` — current environment name
- merged input fields such as `.terraformDir`, `.chart`, `.nodeVersion`

## Execution semantics

Jobs can declare: `runsOn`, `timeout`, `retries`, `labels`, `capabilities`, `steps`.

Steps can declare: `run` or `use`, `capability`, `phase`, `order`, `with`, `env`, `shell`, `working-directory`, `timeout`, `retry`, `onFailure`.
