---
title: Terraform state on the platform
---

Remote runs export a complete `TF_HTTP_*` environment to every step of a
component job, pointing Terraform's `http` state backend at Orunbase's
native Terraform state store. Compositions need **no** `-backend-config`
plumbing, no `aws-actions/configure-aws-credentials` step, and no tenant AWS
account (S3 bucket + OIDC roles) for state:

```hcl
terraform {
  backend "http" {}   # everything comes from the environment
}
```

## What gets exported

On **remote runs with a resolved (org, project) scope**, every step of a
**component job** (a job with both a component and an environment) receives:

| Variable | Value |
|---|---|
| `TF_HTTP_ADDRESS` | `{backendURL}/v1/organizations/{org}/projects/{project}/state/tfstate/{component}/{environment}` |
| `TF_HTTP_LOCK_ADDRESS` | same address |
| `TF_HTTP_UNLOCK_ADDRESS` | same address |
| `TF_HTTP_LOCK_METHOD` | `LOCK` |
| `TF_HTTP_UNLOCK_METHOD` | `UNLOCK` |
| `TF_HTTP_USERNAME` | `orun` |
| `TF_HTTP_PASSWORD` | the run token — **re-minted per step**, so a long run never hands Terraform an expired token; registered with the redactor |
| `TF_HTTP_RETRY_MAX` | `4` |

State is addressed per `(component, environment)` within the linked project,
so every component/environment pair gets its own state document and lock —
the same isolation an S3 key-per-component layout gave you, without the
bucket.

## Scope and failure posture

- In CI, the scope is the OIDC-token-bound scope; locally it is the linked
  workspace/project.
- Local runs and non-component jobs get nothing.
- Wiring is best-effort but failure is loud: without a resolved org/project no
  `TF_HTTP_*` env is set, and `terraform init` fails on the empty backend
  config rather than silently targeting the wrong tenant.

## See also

- [`orun run`](../cli/orun-run.md) — remote-state runs
- [Distributed execution with remote state](../examples/remote-state-matrix.md)
- [State model](../concepts/state-model.md) — orun's own object model (distinct from Terraform state)
- [Secrets](../concepts/secrets.md) — `secretOutputs`, the companion channel for values Terraform produces
