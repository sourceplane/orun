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

## Include policy (plan selection)

Since v2.9.0, `dependsOn` separates two orthogonal questions:

1. **Ordering** — should A wait for B when both are in the plan? (existing `dependencyMode` / `condition`)
2. **Inclusion** — should B be pulled into the plan when only A was selected by `--changed`? (new `include`)

The new `include` field controls inclusion:

| Value         | Plan selection behavior                                              | Best for                                                |
|---------------|----------------------------------------------------------------------|---------------------------------------------------------|
| `if-selected` | Only add an ordering edge if the dependency is already in the plan   | Default — most component dependencies                   |
| `always`      | Pull the dependency into the plan, then add the ordering edge        | Migrations, codegen, shared infra, parent package build |

`if-selected` is the built-in default and is applied during normalization. Change-detection no longer silently includes unchanged components.

### Default: order-only

```yaml
metadata:
  name: web-console
spec:
  type: cloudflare-pages-turbo
  dependsOn:
    - component: ui-package
      # include: if-selected  (default)
```

```text
changed: web-console            -> plan: web-console
changed: ui-package             -> plan: ui-package
changed: web-console + ui-pkg   -> plan: ui-package -> web-console
full plan                       -> plan: ui-package -> web-console
```

### Opt-in: always pull the dependency in

```yaml
metadata:
  name: api-edge-worker
spec:
  type: cloudflare-worker-turbo
  dependsOn:
    - component: identity-schema
      include: always
      reason: api requires latest generated identity contract before deploy
```

```text
changed: api-edge-worker  -> plan: identity-schema -> api-edge-worker
```

`reason` is free-form text surfaced for auditability; it never affects behavior.

## Input edges (build-input rescope)

Since v2.22.0, `dependsOn` answers a third orthogonal question:

3. **Rescope** — should *A* be treated as changed when only B changed? (new `input`)

Mark a dependency `input: true` when its **sources are build inputs of your
artifact** — a bundled shared package, generated contracts, anything your
build compiles in. Change detection then treats a change to the dependency as
a *direct* change to your component: `--changed` selects it, plans its jobs,
and (on push-to-main lanes) schedules its deploy.

```yaml
metadata:
  name: policy-worker
spec:
  type: cloudflare-worker-turbo
  dependsOn:
    - component: policy-engine
      input: true   # the worker bundles packages/policy-engine
```

```text
changed: packages/policy-engine  -> plan: policy-engine + policy-worker
```

Without `input`, this is the classic shared-package gap: the worker's bundle
embeds the package, but `--changed` only matches files under the worker's own
directory — so a package-only change ships no deploy and production serves a
stale artifact until someone touches the worker's directory by hand.

`input` is transitive over other input edges: with `contracts ←(input)─ sdk
←(input)─ console`, a change to `contracts` rescopes both `sdk` and
`console`. It composes with `include` (forward pull vs reverse rescope) and
is inert to ordering (`dependencyMode` still decides what waits for what).
Plain runtime peers — service bindings, API calls — should **not** be marked
`input`: they appear in the blast radius (`orun catalog affected`) but a peer
redeploy is not required for a peer-only change.

`orun catalog affected --explain` records the provenance of every rescope:

```text
explain:
  - ns/repo/policy-engine: input file changed: packages/policy-engine/src/index.ts
  - ns/repo/policy-worker: build input changed (dependsOn input:true)
```

### Relationship to `dependencyMode`

`include` and `dependencyMode` are orthogonal — combine freely:

| `dependencyMode` | `include`     | Effect                                                                              |
|------------------|---------------|-------------------------------------------------------------------------------------|
| `enforced`       | `if-selected` | Default. Blocking edge, only present if both ends are selected.                     |
| `enforced`       | `always`      | Dependency is pulled in and acts as a hard ordering edge.                           |
| `advisory`       | `if-selected` | PR pattern: parallel feedback, no transitive selection of unchanged components.     |
| `advisory`       | `always`      | Pulls the dependency in but records the edge as advisory rather than blocking.      |
| `disabled`       | *(any)*       | Edge omitted from the plan entirely.                                                |

### Validation

Invalid `include` values fail at plan time:

```text
component api: dependsOn[0].include "sometimes" is invalid
  (expected "if-selected" or "always")
```

Missing-dependency errors are now scoped to `include: always`. Under the default `if-selected`, a dependency target that isn't in the plan is just a silently-dropped order edge — exactly what `--changed` wants. With `include: always` it remains a real misconfiguration:

```text
dependency not found: api.pr depends on db.pr (include: always)
```
