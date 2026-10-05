---
title: Compositions
description: A composition is a golden path written as code - the schema a component must satisfy, the jobs it runs, and the execution profiles allowed in each lane - so "how we deploy Terraform here" is enforced, not a wiki page.
---

A composition is a **golden path written as code**. It is how a platform team turns "this
is how we build and ship Terraform here" from a wiki page into a contract the platform
checks: the parameters a component of this type must provide, the jobs it runs, and the
profiles it may run under in each lane. Every component that declares `type: terraform`
gets exactly that path, and changing the path is one reviewed change in one place.

Compositions are the *how* to [intent](./intent-model.md)'s *what*. A `component.yaml`
names a type; the composition for that type decides everything about how it executes.

## The four parts of a golden path

A composition is split into four kinds of document, so each concern can change on its own:

| Kind | The standard it holds | Changes when |
|------|---------|-------------|
| `ComponentSchema` | What a component of this type must look like: its required parameters and their shapes | The contract with application teams changes |
| `JobTemplate` | The steps that build, check, and ship it, each tagged with a capability | The implementation changes |
| `ExecutionProfile` | Which capabilities run in a lane, with which step overrides | Pull-request, verify, or release behaviour changes |
| `Composition` | The type name and how the other three fit together: default job, default profile, the profile catalogue | You introduce or rename a type |

The `Composition` document is the facade that references the others by name:

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

The full field reference is the [composition contract](../compositions/composition-contract.md).

## How a golden path is enforced

- **The schema is checked at plan time.** `orun plan` validates every component's merged
  parameters against its type's `ComponentSchema`. A component missing a required
  parameter, or carrying one of the wrong shape, fails the plan before anything runs.
- **Only declared profiles can run.** A subscription that names a profile the composition
  does not define fails the plan. Application teams choose among the lanes the platform
  team offers; they cannot invent new ones.
- **Profiles select capabilities, not step IDs.** A `JobTemplate` tags each step with a
  `capability`, and a profile lists the capabilities it runs:

  ```yaml
  # JobTemplate step
  - id: plan
    name: plan
    capability: terraform.plan
    run: terraform plan -no-color

  # ExecutionProfile
  spec:
    jobs:
      validate:
        includeCapabilities:
          - terraform.setup
          - terraform.plan
  ```

  Renaming or adding a step does not silently change which steps a lane runs.
- **Overrides stay inside the profile.** A profile can patch a step's `run`, `with`, or
  `env` without copying it:

  ```yaml
  spec:
    jobs:
      validate:
        stepOverrides:
          plan:
            run: terraform -chdir={{.parameters.terraformDir}} plan -no-color -lock=false
  ```

- **The plan shows the result.** Every rendered step lands in `plan.json`, so a change to a
  golden path is visible as a plan diff in every repository that adopts it.

A profile can also declare a `policies` block (`requireCleanGitTree`,
`requirePinnedTerraformVersion`, `requireApproval`). orun records it but does not enforce it
yet; see [standards](./standards.md#declared-not-yet-enforced).

## Where compositions come from

A repository declares its composition sources in `intent.yaml`:

```yaml
compositions:
  sources:
    - name: platform
      kind: oci
      ref: ghcr.io/acme/platform-stack:v1.4.0
    - name: local
      kind: dir
      path: ./compositions
```

Source kinds are `dir`, `archive`, and `oci`. Compositions are packaged and published as a
**Stack**, which is how a golden path travels between repositories and gets versioned; see
[Stacks](./stacks.md). A source can carry a `digest:` that `orun plan` verifies, so a
repository changes golden paths only when it changes the pin.

## Versioning, lifecycle, and effects

Golden paths evolve. Three fields on the `Composition` make that evolution explicit:

```yaml
spec:
  version: 2.3.0        # semver, layered on the content digest
  lifecycle: stable     # stable | beta | deprecated
  effects:              # what running this path contributes to the catalog
    integrations:
      cloudflare: { product: workers }
    provides: [default/orun/cloudflare-edge]   # Resource entities
    exposes: [default/orun/edge-http-api]      # API entities
    scorecards:
      satisfies: [has-deploy-pipeline]
```

- **`version` and `lifecycle`** are recorded on the composition's catalog entity, so
  "which services are still on a deprecated golden path?" is a catalog query, not an audit.
  orun does not print a warning for a deprecated composition.
- **`effects`** declare a path's consequences once: every component that adopts the type
  gets its integrations, provided Resources, and exposed APIs linked into the
  [service catalog](./service-catalog.md) without authoring them per component.

## Authoring

Split-kind authoring, one file per document under `compositions/<type>/`, is recommended
for any composition with more than one profile. Simple compositions can still keep
everything in a single file with an inline schema and jobs.

- [Writing compositions](../compositions/writing-compositions.md) walks through encoding a
  golden path and publishing it.
- [Composition examples](../compositions/composition-examples.md) tours the example Stack
  in this repository, which defines eleven types.

## Inspecting compositions from the CLI

```bash
orun compositions --intent examples/intent.yaml            # every resolved type
orun compositions terraform --intent examples/intent.yaml  # one type, with its profiles
orun component network-foundation --intent examples/intent.yaml --long
```

Next: [Stacks](./stacks.md), to package and version a golden path, or the
[composition contract](../compositions/composition-contract.md) to author one.
