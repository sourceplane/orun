# Trigger-aware dependency policy (`dependencyMode` / `dependencyRules`)

**Goal:** Add a plan-time, trigger-aware dependency policy so the
compiled DAG explicitly shows whether component `dependsOn` edges are
**enforced** (block in the executor), **advisory** (carried as
metadata but not blocking), or **disabled** (ignored) — selected by
trigger context. PR plans can run jobs in parallel while still
documenting the underlying dependency for reviewers; merge/release
plans keep enforced edges.

## Why this design (not profile-driven)

`profileRules` already exists for changing **which steps run** in a
component under a trigger. Dependency edges are graph semantics, not
step semantics. Keeping the two separate preserves the clean split:

```
TriggerBinding    -> which environments/components are planned
ProfileRules      -> which execution behaviour/profile is used
DependencyRules   -> which dependency edges are enforced  <-- NEW
Plan DAG          -> explicit, deterministic, reviewable result
```

## Modes

| Mode       | DAG behaviour                                              | Typical use            |
|------------|------------------------------------------------------------|------------------------|
| `enforced` | Normal `dependsOn` edges. Default everywhere.              | merge / release        |
| `advisory` | Edge moved into `advisoryDependsOn`; `dependsOn` is empty. | PR validation          |
| `disabled` | Dependency dropped entirely.                               | Rare escape hatch      |

Default is `enforced`. Production semantics never weaken without an
explicit opt-in.

## Schema additions

Environment-level default:

```yaml
environments:
  pull-request:
    activation:
      triggerRefs: [github-pull-request]
    dependencyMode: advisory
  staging:
    activation:
      triggerRefs: [github-push-main]
    dependencyMode: enforced
```

Subscription-level override (per component × environment):

```yaml
spec:
  subscribe:
    environments:
      - name: dev-preview
        profile: pull-request
        dependencyMode: advisory
        dependencyRules:
          - when: { triggerRef: github-push-main }
            mode: enforced
          - when: { triggerRef: github-tag-release }
            mode: enforced
```

Precedence (highest first):

1. Subscription `dependencyRules` matching a fired triggerRef
2. Subscription `dependencyMode`
3. Environment `dependencyMode`
4. Built-in default `enforced`

## Plan output

Advisory:

```json
{
  "id": "api.dev-preview.verify",
  "dependsOn": [],
  "dependencyMode": "advisory",
  "advisoryDependsOn": ["database.dev-preview.verify"],
  "dependencySource": "subscription-rule",
  "dependencyRuleTriggerRef": "github-pull-request"
}
```

Enforced:

```json
{
  "id": "api.staging.verify",
  "dependsOn": ["database.staging.verify"],
  "dependencyMode": "enforced",
  "dependencySource": "environment-default"
}
```

`orun plan --view dag` annotates advisory edges as
`(advisory dependency)` instead of `(depends on)`; the dependencies
view shows both kinds and notes the dependency source / rule trigger.

## Implementation phases

### Phase 1 — model + composition resolver
- `model.Environment.DependencyMode string`
- `model.EnvironmentSubscription.DependencyMode string`
- `model.EnvironmentSubscription.DependencyRules []DependencyRule`
- `model.DependencyRule{ When DependencyRuleWhen; Mode string }`
- `model.DependencyMode` constants: `enforced`, `advisory`, `disabled`.
- `composition.ResolveDependencyMode(env, sub, matchedTriggers)`
  returns `ResolvedDependencyMode{Mode, Source, RuleTriggerRef}`.

### Phase 2 — expander + planner wiring
- `ComponentInstance.DependencyMode/Source/RuleTriggerRef`
  populated by `Expander.resolveDependencyMode` using existing
  `matchedTriggers`.
- `JobInstance` gains `DependencyMode`, `DependencySource`,
  `DependencyRuleTriggerRef`, `AdvisoryDependsOn`.
- `JobPlanner.resolveDependencies` branches on mode:
  - `enforced` → as today (append to `DependsOn`)
  - `advisory` → append to `AdvisoryDependsOn`
  - `disabled` → drop

### Phase 3 — renderer + viewer
- `PlanJob` gains the four new fields with `omitempty` JSON tags.
- `viewer.ViewDAG` renders advisory deps as
  `(advisory dependency) <id>`.
- `viewer.ViewDependencies` lists `advisoryDependsOn` separately and
  prints the source / rule trigger.

### Phase 4 — validation
- `trigger.ValidateDependencyRules`:
  - mode ∈ {enforced, advisory, disabled}
  - environment mode validated as enum
  - `dependencyRules[].when.triggerRef` must exist in
    `automation.triggerBindings`
- Wire into `cmd/orun/command_validate.go` (alongside
  `ValidateProfileRules`).

### Phase 5 — tests
- `internal/composition/dependency_test.go` covering precedence and
  defaults.
- Planner test: advisory mode keeps DependsOn empty but populates
  AdvisoryDependsOn.
- Trigger validate test: unknown triggerRef + bad mode rejected.

### Phase 6 — docs + release
- `website/docs/concepts/dependency-rules.md`
- Update `concepts/plan-dag.md` (advisory-edge note) and
  `concepts/profile-rules.md` (link to dependency-rules).
- Sidebar entry under Concepts.
- Release notes `website/docs/release-notes/v2.8.0.md`.
- Sidebar release-notes entry, newest first.

### Phase 7 — release
- PR, squash merge, tag `v2.8.0`, watch release-oci workflow.

## Compatibility

- New fields are additive and omitempty; old plans round-trip unchanged.
- Default mode is `enforced` → existing intents behave identically.
- No flag changes to `orun plan` / `orun run`; behaviour is selected
  at compile time by trigger context, so the DAG itself is the
  audit surface.
