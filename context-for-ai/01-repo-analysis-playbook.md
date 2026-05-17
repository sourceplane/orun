# Repo analysis playbook

Use this playbook when an AI agent enters an application or platform repo that uses Orun concepts. The goal is to build an accurate model before editing.

## First pass

Inspect these files and directories first:

| Path or pattern | Purpose | Orun relevance |
| --- | --- | --- |
| `intent.yaml`, `intent.yml` | Repo-level desired state | Primary planning boundary |
| `component.yaml`, `component.yml` | Component declarations | Discovered ownership units |
| `.orun/compositions.lock.yaml` | Resolved composition digests | Reproducibility evidence |
| `.orun/component-tree.yaml` | Cached discovery tree | Useful for quick inspection, but regenerate from source when unsure |
| `compositions/`, `stacks/`, `platform/` | Composition packages | Execution contracts and schemas |
| `stack.yaml` | Stack package manifest | Composition package metadata and registry target |
| `.github/workflows/` | CI entrypoints | Look for `orun plan`, `orun run`, and trigger payload usage |
| `README.md`, `docs/`, `website/docs/` | Human documentation | Must stay aligned with component model |
| `services/`, `apps/`, `packages/`, `infra/`, `charts/`, `deploy/`, `website/` | Common component roots | Often contain discovered component manifests |

Do not assume the names above are exhaustive. The actual source of truth is `intent.discovery.roots`.

## Non-destructive commands

Run these when available:

```bash
orun --help
orun validate --intent intent.yaml
orun component --intent intent.yaml
orun component --intent intent.yaml --long
orun compositions --intent intent.yaml
orun compositions --intent intent.yaml --long
orun plan --intent intent.yaml --view dag
orun plan --intent intent.yaml --view dependencies
orun plan --intent intent.yaml --output /tmp/orun-plan.json
```

If the repo still has a historical CLI name, use equivalent commands and document the compatibility.

If a command fails because dependencies or credentials are missing, keep going with file inspection. Record the failed command and the reason.

## Build a repo map

Create a table like this in your notes before editing:

| Path | Purpose | Orun relevance | Safe to edit? | Notes |
| --- | --- | --- | --- | --- |
| `intent.yaml` | Repo control plane | Environments, compositions, discovery, automation | Yes, carefully | Changes affect the whole plan |
| `apps/api/component.yaml` | API component | Typed component declaration | Yes | Validate against its composition schema |
| `compositions/terraform/` | Terraform contract | Schema, job template, profiles | Yes, high impact | Affects every `terraform` component |
| `.orun/` | Generated state/cache | Lock files, plans, executions | Usually no | Do not edit generated plans as source |

## Inventory checklist

Identify:

- Intent metadata and namespace.
- Discovery roots.
- Composition sources, source kinds, and lock digests.
- Environments and trigger activation rules.
- Groups, domains, defaults, and policies.
- Components, their paths, types, domains, subscriptions, labels, inputs, and dependencies.
- Component types and the compositions they bind to.
- Profiles used by each component/environment subscription.
- Trigger bindings and CI workflows that call Orun.
- Existing plan examples and whether they are generated artifacts or docs fixtures.

## Component inventory table

Use this shape when summarizing a target repo:

| Component | Type | Path | Domain | Environments | Profile(s) | Depends on | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `network-foundation` | `terraform` | `infra/network` | `platform-foundation` | `dev`, `staging`, `production` | `pull-request`, `verify`, `release` | none | Foundation dependency |

## Composition inventory table

Use this shape:

| Type | Source | Default job | Default profile | Profiles | Key inputs | Used by |
| --- | --- | --- | --- | --- | --- | --- |
| `terraform` | `platform` | `validate` | `verify` | `pull-request`, `verify`, `release` | `stackName`, `terraformDir`, `terraformVersion` | infra components |

## Reading generated files

Generated files are useful evidence, but they are not usually the source of truth.

- Read `.orun/compositions.lock.yaml` to see resolved sources.
- Read plan JSON to inspect concrete jobs and dependencies.
- Do not manually patch generated plans to "fix" behavior. Change intent, components, or compositions and regenerate.

