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

execution:
  profiles:
    dry-run:
      description: Non-mutating checks only
      compositionRef: terraform
      plan:
        scope: changed
      controls:
        terraform:
          apply:
            enabled: false

automation:
  triggers:
    - name: github-push-main
      on:
        provider: github
        event: push
        branches: [main]
      plan:
        profile: verify

groups:
  platform:
    policies:
      isolation: strict
    defaults:
      namespacePrefix: platform-

environments:
  production:
    execution:
      profile: release
      profiles:
        cloudflare-worker: deploy
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

### `execution`

The `execution` block defines [profiles](./profiles.md) — named configurations that control which steps run:

```yaml
execution:
  profiles:
    dry-run:
      compositionRef: terraform
      plan:
        scope: changed
      controls:
        terraform:
          apply:
            enabled: false
```

Profiles can also be loaded from stack-level files via convention-over-configuration. See [profiles](./profiles.md) for the full model.

### `automation`

The `automation` block defines [triggers](./automation.md) that connect CI events to profile selection:

```yaml
automation:
  triggers:
    - name: github-push-main
      on:
        provider: github
        event: push
        branches: [main]
      plan:
        profile: verify

  triggerBindings:
    - name: github-pull-request
      on:
        provider: github
        event: pull_request
        actions: [opened, synchronize, reopened, ready_for_review]
      plan:
        scope: changed
        base: main
```

See [automation](./automation.md) for trigger matching, CI auto-detection, and override policies.

### `groups`

Groups define platform-owned defaults and policy domains. They are the right place for non-negotiable constraints and common defaults that should apply across many components.

### `environments`

Environments define selectors, defaults, and policies for a target environment such as `development`, `staging`, or `production`.

Environments can also declare execution settings that select profiles per-type:

```yaml
environments:
  production:
    execution:
      profile: release
      profiles:
        cloudflare-worker: deploy
        cloudflare-pages: deploy
    defaults:
      namespacePrefix: prod-
```

The `execution.profile` sets the default profile for all composition types in that environment. The `execution.profiles` map overrides specific composition types.

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

### Rich subscription format

Components can specify per-environment profile and trigger binding overrides using the array subscription format:

```yaml
spec:
  type: terraform
  subscribe:
    - environments: [dev]
      profile: dry-run
    - environments: [staging, production]
      profile: release
      triggerBindings: [github-push-main]
```

Each entry can override the profile and trigger bindings for the listed environments, giving fine-grained control over how each component×environment pair executes.

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
