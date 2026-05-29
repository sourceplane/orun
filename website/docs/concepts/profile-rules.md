---
title: Profile rules
---

Profile rules provide conditional execution profile selection based on which trigger fired. They allow a single component subscription to behave differently depending on the CI event — for example, running `plan-only` on pull requests but `apply` on merge to main.

## Motivation

Without profile rules, you would need separate environments or external logic to control profile selection. Profile rules keep this decision declarative and co-located with the subscription.

## Syntax

```yaml
subscribe:
  environments:
    - name: dev-preview
      profile: plan-only
      profileRules:
        - profile: apply
          when:
            triggerRef: github-push-main
```

The `profile` field is the **fallback/default** — it is used when no rule matches (including when no trigger is active, such as local `orun plan` runs).

`profileRules` is an ordered list of conditional overrides evaluated top-to-bottom (**first-match-wins**).

## How it works

1. **TriggerBinding decides**: should this environment participate in this plan?
2. **Profile rule decides**: if this component is in this environment, which profile should it use?

These are separate decisions. Profile rules do not affect environment activation — they only influence behavior *within* an already-activated environment.

### Evaluation flow

```
--trigger github-push-main
  → activates dev-preview (via environment activation.triggerRefs)
  → component subscribes to dev-preview
  → profileRule matches triggerRef: github-push-main
  → selected profile: apply

--trigger github-pull-request
  → activates dev-preview
  → component subscribes to dev-preview
  → no profileRule matches
  → selected profile: plan-only (fallback)
```

## Full example

```yaml
apiVersion: sourceplane.io/v1
kind: Component

metadata:
  name: network-foundation

spec:
  type: terraform
  domain: platform-foundation

  subscribe:
    environments:
      - name: dev-preview
        profile: plan-only
        profileRules:
          - profile: apply
            when:
              triggerRef: github-push-main

      - name: stage-preview
        profile: plan-only
        profileRules:
          - profile: apply
            when:
              triggerRef: github-tag-release
```

## Plan output

When a profile rule fires, the plan records both the selected profile and the trigger that caused it:

```json
{
  "profile": "terraform.apply",
  "profileSource": "subscription-rule",
  "profileRuleTriggerRef": "github-push-main"
}
```

The DAG view also annotates this:

```
├─ network-foundation
│  └─ dev-preview [terraform.apply via github-push-main]
│     └─ apply
```

## Validation rules

| Rule | Reason |
|------|--------|
| `profile` is required when `profileRules` exists | Always have a deterministic fallback |
| `profileRules[].profile` must exist in the composition | Prevent runtime surprises |
| `when.triggerRef` must exist in `automation.triggerBindings` | Avoid typo-based silent behavior |
| First-match-wins (order matters) | Deterministic evaluation |

## Conditions

For v1, only `triggerRef` is supported in the `when` block:

```yaml
when:
  triggerRef: github-push-main
```

The `triggerRef` must reference a key declared in `automation.triggerBindings`.

## When not to use profile rules

- If you need different *environments* per trigger, use environment activation (`activation.triggerRefs`) instead
- If you want *all* components in an environment to use the same profile, set it as the composition's `defaultProfile`
- Profile rules are best for cases where a single environment needs different behavior depending on the triggering event
- To change whether a `dependsOn` edge blocks execution (e.g. parallel PR plans), use [Dependency rules](./dependency-rules.md) — they operate on the DAG, not the steps
