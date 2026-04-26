---
title: Intent model
---

`gluon` treats intent as the desired-state layer. It captures the platform policy, environment defaults, component subscriptions, and discovery roots that define **what** should happen, not **how** steps should execute.

## Inputs that make up intent

The planning boundary is built from four inputs:

1. `intent.yaml`
2. Discovered `component.yaml` manifests
3. Composition sources declared under `intent.compositions`
4. Optional CLI scoping such as `--env` or change-detection flags

The output of those inputs is a compiled `plan.json`.

## Structure of the intent file

```yaml
apiVersion: sourceplane.io/v1
kind: Intent

metadata:
  name: microservices-deployment

compositions:
  sources:
    - name: platform-core
      kind: dir
      path: ./packages/platform-core

discovery:
  roots:
    - services/
    - infra/
    - deploy/

groups:
  platform:
    policies:
      isolation: strict
    defaults:
      namespacePrefix: platform-

environments:
  production:
    selectors:
      domains: [platform]
    defaults:
      replicas: 3
    policies:
      requireApproval: "true"
```

### `discovery`

Discovery roots are scanned recursively for `component.yaml` files relative to the location of the intent file.

`intent.yaml` also serves as the workspace root marker for [context-aware discovery](./context-discovery.md). When you run `gluon` from any subdirectory in your repo, it walks up the directory tree to find `intent.yaml` and uses its location to resolve all relative paths.

### `groups`

Groups define platform-owned defaults and policy domains. They are the right place for non-negotiable constraints and common defaults that should apply across many components.

### `environments`

Environments define selectors, defaults, and policies for a target environment such as `development`, `staging`, or `production`.

### `components`

Intent can also declare inline components. In practice, many teams combine inline components with discovered component manifests when they want both central declarations and repo-local ownership.

## Discovered component manifests

Component manifests carry type-specific inputs and dependency edges:

```yaml
apiVersion: sourceplane.io/v1
kind: Component

metadata:
  name: web-app

spec:
  type: helm
  domain: platform
  subscribe:
    environments: [development, staging, production]
  inputs:
    chart: oci://mycompany.azurecr.io/helm/charts/default
  overrides:
    steps:
      - name: verify
        phase: post
        order: 10
        run: kubectl get deployment -n platform-web-app web-app
  dependsOn:
    - component: common-services
      scope: same-environment
      condition: success
```

## Merge model

At compile time, `gluon` merges configuration in a stable order from lowest to highest precedence:

1. Type defaults from the composition input schema
2. Job defaults from the resolved composition job definition
3. Group defaults
4. Environment defaults
5. Component inputs and overrides

Policies are not treated as ordinary defaults. They are enforced as platform constraints.

## Why this split matters

- App teams can declare intent without owning runner details.
- Platform teams can evolve packaged schemas and job definitions independently.
- Reviewers can diff desired state separately from execution steps.
- The compiled plan remains deterministic because all implicit defaults are resolved before runtime.

The legacy `--config-dir` flag still works during migration, but packaged composition sources are the recommended model.

Read [compositions](./compositions.md) next to see how component types bind to executable jobs.