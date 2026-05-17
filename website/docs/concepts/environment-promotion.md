---
title: Environment Promotion
---

# Environment Promotion

Environment promotion dependencies define ordering and gating relationships between environments. They express the deployment pipeline flow — for example, staging must succeed before production can run.

This is separate from component-level `dependsOn`, which expresses service-to-service dependencies within an environment.

## Syntax

Add `promotion.dependsOn` to any environment that requires prior environment success:

```yaml
environments:
  preview:
    activation:
      triggerRefs:
        - github-pull-request
    parameterDefaults:
      "*":
        lane: verify

  staging:
    activation:
      triggerRefs:
        - github-push-main
    promotion:
      dependsOn:
        - environment: preview
    parameterDefaults:
      "*":
        lane: verify

  production:
    activation:
      triggerRefs:
        - github-tag-release
    promotion:
      dependsOn:
        - environment: staging
    parameterDefaults:
      "*":
        lane: release
```

With defaults applied, each entry is equivalent to:

```yaml
promotion:
  dependsOn:
    - environment: staging
      strategy: same-component
      condition: success
      satisfy: same-plan-or-previous-success
      match:
        revision: source
```

## How It Compiles

The behavior depends on whether the dependency environment is active in the same plan.

### Same-plan (both environments active)

When both environments are activated by the same trigger (or selected with `--env`), promotion dependencies compile into normal DAG edges:

```
web-app@dev.deploy  →  web-app@staging.deploy
api@dev.deploy      →  api@staging.deploy
```

The plan output uses standard `dependsOn`:

```json
{
  "id": "web-app.staging.deploy",
  "dependsOn": ["web-app.dev.deploy"]
}
```

### Cross-plan (dependency environment not active)

When environments are activated by different triggers (e.g., PR activates preview, push activates staging), the dependency environment won't be in the same plan. In this case, promotion compiles into **gates** — evidence checks that require prior success:

```json
{
  "id": "web-app.production.release",
  "dependsOn": [],
  "gates": [
    {
      "type": "environment-promotion",
      "environment": "staging",
      "component": "web-app",
      "condition": "success",
      "match": { "revision": "source" }
    }
  ]
}
```

Gates require that the same component succeeded in the referenced environment for the same source revision before execution is allowed.

## Strategies

### `same-component` (default)

Each component in the target environment depends only on the same component in the dependency environment:

```
web-app@dev  →  web-app@staging
api@dev      →  api@staging
```

Components not subscribed to both environments are gracefully skipped.

### `environment-barrier`

Every job in the target environment waits for every job in the dependency environment:

```
web-app@dev  ─┬─→  web-app@staging
api@dev      ─┘ ┌→  api@staging
web-app@dev  ───┘
api@dev      ────→  api@staging
```

Use this for strict release-train semantics where the entire environment must be green before promotion.

## Satisfy Modes

The `satisfy` field controls how the dependency is checked:

| Mode | Behavior |
|------|----------|
| `same-plan` | Requires both environments in the same plan. Errors if the dependency environment is not active. |
| `previous-success` | Always uses prior evidence gates, even if both environments happen to be active. |
| `same-plan-or-previous-success` | Uses DAG edges when both are active, falls back to gates otherwise. **(default)** |

## Match

The `match` field controls how prior success evidence is validated:

```yaml
match:
  revision: source
```

`revision: source` means the gate checks that the same source commit (or the commit pointed to by a tag) succeeded in the prior environment.

## Interaction with Trigger Bindings

Promotion dependencies work alongside [trigger bindings](./trigger-bindings.md) and environment activation:

1. **Trigger activation** decides which environments are active in a given plan
2. **Promotion dependencies** decide ordering and gating between those environments
3. **Component dependencies** decide ordering within the active set

A typical setup:

```yaml
automation:
  triggerBindings:
    github-pull-request:
      on:
        provider: github
        event: pull_request
      plan:
        scope: changed

    github-push-main:
      on:
        provider: github
        event: push
        branches: [main]
      plan:
        scope: changed

    github-tag-release:
      on:
        provider: github
        event: push
        tags: ["v*"]
      plan:
        scope: full

environments:
  preview:
    activation:
      triggerRefs: [github-pull-request]
    parameterDefaults:
      "*":
        lane: verify

  staging:
    activation:
      triggerRefs: [github-push-main]
    promotion:
      dependsOn:
        - environment: preview
    parameterDefaults:
      "*":
        lane: verify

  production:
    activation:
      triggerRefs: [github-tag-release]
    promotion:
      dependsOn:
        - environment: staging
    parameterDefaults:
      "*":
        lane: release
```

This gives you:
- PR → preview only (validate changes)
- Push to main → staging only (gate: preview must have passed for this commit)
- Tag → production only (gate: staging must have passed for this commit)

## Validation

Orun validates promotion dependencies at normalization time:

- Referenced environments must exist
- Self-references are rejected
- Cycles are detected (e.g., A → B → A)
