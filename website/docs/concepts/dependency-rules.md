---
title: Dependency rules
---

Dependency rules let you control whether `dependsOn` edges in the plan DAG are **enforced** (block execution), **advisory** (recorded but non-blocking), or **disabled** (omitted entirely), conditional on which trigger fired.

This is the dependency-graph equivalent of [Profile rules](./profile-rules.md): profile rules change *what steps run inside a job*, dependency rules change *whether one job waits for another*.

## Motivation

For pull-request validation you usually want fast, parallel feedback — there is no point waiting for `database` to plan before `api` can plan, because nothing is being applied. For merges to main or tag-based releases, the dependency must be enforced because real apply ordering matters.

Without dependency rules you'd either:

- duplicate environments per trigger (verbose, drift-prone), or
- mutate the DAG at execution time (breaks the "plan is the audit artifact" model).

Dependency rules keep the policy at plan time so `orun plan --view dag` shows the resulting graph deterministically.

## Modes

| Mode       | DAG behavior                                                | Typical use case                         |
|------------|-------------------------------------------------------------|------------------------------------------|
| `enforced` | Normal `dependsOn` edges (blocks executor)                  | Merges, releases, production deploys     |
| `advisory` | Edges recorded as `advisoryDependsOn` metadata, not blocking | PR validation, speculative previews      |
| `disabled` | Dependency edge omitted entirely                            | Emergency bypass / explicit independence |

`enforced` is the built-in default.

## Syntax

### Environment-level default

```yaml
environments:
  dev-preview:
    activation:
      triggerRefs: [github-pull-request]
    dependencyMode: advisory

  staging:
    activation:
      triggerRefs: [github-push-main]
    dependencyMode: enforced
```

### Component-subscription override

```yaml
spec:
  subscribe:
    environments:
      - name: dev-preview
        profile: plan-only
        dependencyMode: advisory
        dependencyRules:
          - mode: enforced
            when:
              triggerRef: github-push-main
          - mode: enforced
            when:
              triggerRef: github-tag-release
```

`dependencyMode` is the fallback when no rule matches; `dependencyRules` is an ordered first-match-wins list.

## Precedence

When the planner computes an instance's effective dependency mode it walks this chain and stops at the first match:

1. `subscription.dependencyRules[]` whose `when.triggerRef` matched the active trigger
2. `subscription.dependencyMode`
3. `environment.dependencyMode`
4. built-in default `enforced`

The selected source is recorded on every job for auditability:

```json
{
  "dependencyMode": "advisory",
  "dependencySource": "subscription-rule",
  "dependencyRuleTriggerRef": "github-pull-request"
}
```

`dependencySource` can be `"default"`, `"environment"`, `"subscription"`, or `"subscription-rule"`.

## Plan output

For a pull-request plan with advisory mode the job retains both views:

```json
{
  "id": "api@dev-preview.verify",
  "dependsOn": [],
  "advisoryDependsOn": ["database@dev-preview.verify"],
  "dependencyMode": "advisory",
  "dependencySource": "subscription-rule",
  "dependencyRuleTriggerRef": "github-pull-request"
}
```

For the same component on push-to-main:

```json
{
  "id": "api@staging.verify",
  "dependsOn": ["database@staging.verify"],
  "dependencyMode": "enforced",
  "dependencySource": "environment"
}
```

`orun plan --view dag` annotates the environment header with the mode and `--view dependencies` distinguishes blocking vs advisory edges:

```
└─ api (api/dev-preview)
  ├─ depends-on: shared-secrets@dev-preview.verify
  └─ advisory:   database@dev-preview.verify
   mode: advisory (rule:github-pull-request)
```

## Validation

Validated at plan time (not run time):

| Rule | Reason |
|------|--------|
| `dependencyMode` must be `enforced`, `advisory`, or `disabled` | Catch typos before they suppress edges |
| `dependencyRules[].mode` is required and must be valid | First-match-wins must not produce empty modes |
| `dependencyRules[].when.triggerRef` must exist in `automation.triggerBindings` | Avoid silent fall-through |

Run `orun validate` to surface these errors.

## Relationship to other features

| Concern | Mechanism |
|---------|-----------|
| Which environments are active for a trigger | `environments[].activation.triggerRefs` |
| Which profile runs inside an active environment | `subscribe.environments[].profile` + [`profileRules`](./profile-rules.md) |
| Which `dependsOn` edges block execution | `dependencyMode` / `dependencyRules` (this page) |

Keeping these axes independent keeps the compiled plan DAG the single source of truth.

## When not to use dependency rules

- If components are genuinely independent in *all* contexts, remove the `dependsOn` declaration instead of marking it `disabled`.
- If you want different *steps*, use profile rules — dependency rules do not change what runs, only what waits.
- For sequencing across environments (e.g. dev before staging) use `environment.promotion.dependsOn`, which is its own promotion-aware mechanism.
