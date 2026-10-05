---
title: Composition examples
description: "A tour of the example Stack shipped in this repository: eleven golden paths for Terraform, Helm, Cloudflare, and turbo, each with its schema, jobs, and lane profiles."
---

The example Stack in this repository is a working set of golden paths: eleven component types, each with a schema that every component of that type must satisfy and profiles for each lane. It is the quickest way to see a platform's standards written as code.

The repository ships one packaged example-platform Stack using the split-kind authoring model. It covers the quick start, GitHub Actions-compatible execution, and multi-root repository discovery.

## Packaged examples

| Package | Exports | Purpose |
| --- | --- | --- |
| `examples/compositions` | `terraform`, `helm-values`, `helm-chart`, `cloudflare-worker-turbo`, `cloudflare-worker`, `cloudflare-pages`, `cloudflare-pages-turbo`, `cloudflare-pages-terraform`, `cloudflare-pages-turbo-terraform`, `turbo-package`, `workspace` | Embedded example-platform Stack used by `examples/intent.yaml` |

Each composition type lives at `examples/compositions/compositions/<type>/` using split-kind authoring:

```text
examples/compositions/compositions/terraform/
├── composition.yaml          # Composition facade with refs
├── schema.yaml               # ComponentSchema for input validation
├── jobs/
│   └── terraform-validate.yaml    # JobTemplate with capability-tagged steps
└── profiles/
    ├── terraform-pull-request.yaml  # ExecutionProfile for PR
    ├── terraform-verify.yaml        # ExecutionProfile for verification
    └── terraform-release.yaml       # ExecutionProfile with policies
```

The `stack.yaml` at the package root uses auto-discovery — no explicit path listing is needed.

## Split-kind authoring pattern

Every composition in the example stack demonstrates the split-kind pattern where each concern has its own file:

- **Composition** references schema, jobs, and profiles by name
- **ComponentSchema** validates component inputs independently
- **JobTemplate** defines steps with `capability` tags for semantic selection
- **ExecutionProfile** selects behavior via `includeCapabilities` and `stepOverrides`

## GitHub Actions-compatible composition

The Terraform example shows how a `JobTemplate` uses a GitHub Actions `use:` step with capability tagging:

```yaml
# jobs/terraform-validate.yaml
apiVersion: sourceplane.io/v1alpha1
kind: JobTemplate
metadata:
  name: terraform-validate
spec:
  capabilities:
    - terraform.setup
    - terraform.validate
  steps:
    - id: setup
      name: setup
      capability: terraform.setup
      use: hashicorp/setup-terraform@v4
      with:
        terraform_version: "{{.parameters.terraformVersion}}"
        terraform_wrapper: "false"
    - id: validate
      name: validate
      capability: terraform.validate
      run: terraform -chdir={{.parameters.terraformDir}} validate -no-color
      onFailure: stop
```

The corresponding profile selects which capabilities to include and can override step behavior:

```yaml
# profiles/terraform-pull-request.yaml
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
          run: terraform -chdir={{.parameters.terraformDir}} init -backend=false -input=false
        plan:
          run: terraform -chdir={{.parameters.terraformDir}} plan -no-color -lock=false
```

## Profile policies

The release profile declares profile policies. orun records them but does not enforce them yet; see [standards](../concepts/standards.md#declared-not-yet-enforced).

```yaml
# profiles/terraform-release.yaml
spec:
  policies:
    requireCleanGitTree: true
    requirePinnedTerraformVersion: true
    requireApproval: true
```

## Composition types included

| Type | Job | Profiles | Scope |
|------|-----|----------|-------|
| `terraform` | validate | pull-request, verify, release | infra |
| `helm-chart` | render | lint-only, verify | delivery |
| `helm-values` | render | lint-only, verify | delivery |
| `cloudflare-pages` | verify-deploy | pull-request, verify, deploy | delivery |
| `cloudflare-pages-turbo` | verify-deploy | pull-request, verify, deploy | delivery |
| `cloudflare-pages-terraform` | verify-reconcile | pull-request, verify, release | infra |
| `cloudflare-pages-turbo-terraform` | verify-reconcile | pull-request, verify, release | infra |
| `cloudflare-worker` | verify-deploy | pull-request, verify, deploy | delivery |
| `cloudflare-worker-turbo` | verify-deploy | pull-request, verify, deploy | delivery |
| `turbo-package` | verify | quick-check, verify | verify |
| `workspace` | verify | quick-check, full | smoke |
