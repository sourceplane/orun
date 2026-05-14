---
title: Intent model
---

`orun` treats intent as the desired-state layer. It captures the platform policy, environment defaults, component subscriptions, and discovery roots that define **what** should happen, not **how** steps should execute.

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
    - name: example-platform
      kind: dir
      path: ./compositions

discovery:
  roots:
    - apps/
    - infra/
    - deploy/
    - charts/
    - packages/
    - website/

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

`intent.yaml` also serves as the workspace root marker for [context-aware discovery](./context-discovery.md). When you run `orun` from any subdirectory in your repo, it walks up the directory tree to find `intent.yaml` and uses its location to resolve all relative paths.

### `groups`

Groups define platform-owned defaults and policy domains. They are the right place for non-negotiable constraints and common defaults that should apply across many components.

### `environments`

Environments define selectors, defaults, and policies for a target environment such as `development`, `staging`, or `production`.

Environments can optionally declare `activation.triggerRefs` to specify which trigger bindings activate them for CI-driven planning:

```yaml
environments:
  development:
    activation:
      triggerRefs:
        - github-pull-request
    defaults:
      namespacePrefix: dev-
```

See [trigger bindings](./trigger-bindings.md) for full details.

### `automation`

The `automation` section declares trigger bindings that map CI provider events to environment activation:

```yaml
automation:
  triggerBindings:
    github-pull-request:
      on:
        provider: github
        event: pull_request
        actions: [opened, synchronize]
        baseBranches: [main]
      plan:
        scope: changed
        base: pull_request.base.sha
        head: pull_request.head.sha
```

Trigger bindings are opt-in. Existing intent files without `automation` continue to work unchanged.

### `components`

Intent can also declare inline components. In practice, many teams combine inline components with discovered component manifests when they want both central declarations and repo-local ownership.

## Discovered component manifests

Component manifests carry type-specific inputs and dependency edges:

```yaml
apiVersion: sourceplane.io/v1
kind: Component

metadata:
  name: network-foundation

spec:
  type: terraform
  domain: platform-foundation
  subscribe:
    environments: [development, staging, production]
  inputs:
    stackName: network-foundation
    terraformDir: .
    terraformVersion: 1.9.8
```

## Merge model

At compile time, `orun` merges configuration in a stable order from lowest to highest precedence:

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
