---
title: Intent model
description: Where a platform's structure and standards live as code. intent.yaml holds the platform's rules, component.yaml declares each unit next to its code, and everything downstream (plans, agents, the catalog) reads from them.
---

Intent is where your platform's **structure and standards live as code**. A platform team
declares the rules once, in `intent.yaml`: which environments exist, which components ship
to them, which events activate what, and in what order environments promote. Each team
declares what it owns in a `component.yaml` next to its code: what the component is, which
golden path it follows, and what it depends on. Everything downstream reads this one
source: the planner compiles it, the catalog is derived from it, and coding agents query it
instead of guessing.

Intent says **what** should happen. It never says **how** a step executes; that belongs to
[compositions](./compositions.md).

## Who declares what

| Document | Declares | Owned by |
|---|---|---|
| `intent.yaml` | Composition sources, presets, discovery roots, groups, environments, trigger bindings, platform-wide `env` | The platform team, one per repository |
| `component.yaml` | One unit's name, type, domain, environment subscriptions, typed parameters, dependencies, `env` | The team that owns the code |
| Compositions | How a type is built: its schema, job templates, and execution profiles | The platform team, published as a [Stack](./stacks.md) |

The split is the point. An application team can change its parameters without reading
runner code, and a platform team can change a golden path without touching any
`component.yaml`.

## `intent.yaml`

All sections sit at the top level of the document:

```yaml
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: shop-platform

compositions:              # where the golden paths come from
  sources:
    - name: platform
      kind: oci
      ref: ghcr.io/acme/platform-stack:v1.4.0   # the pin

extends:                   # platform rules inherited from that Stack's presets
  - source: platform
    preset: github-actions

discovery:                 # where component.yaml files are found
  roots: [apps/, infra/]

env:                       # platform-wide environment variables
  OWNER: sourceplane

groups:                    # defaults per domain
  platform-foundation:
    parameterDefaults:
      "*":
        namespacePrefix: platform-

environments:              # where components ship, and under which rules
  staging:
    activation:
      triggerRefs: [github-push-main]
    parameterDefaults:
      "*":
        lane: verify
  production:
    selectors:
      domains: [platform-foundation, commerce]
    activation:
      triggerRefs: [github-tag-release]
    promotion:
      dependsOn:
        - environment: staging

automation:                # which CI events activate which environments
  triggerBindings:
    github-push-main:
      on: { provider: github, event: push, branches: [main] }
      plan: { scope: changed, base: before, head: after }
    github-tag-release:
      on: { provider: github, event: push, tags: ["v*"] }
      plan: { scope: full }
```

| Section | What it declares | Read more |
|---|---|---|
| `compositions.sources` | The golden paths this repository uses: a directory, an archive, or an OCI Stack, pinned by its reference | [Stacks](./stacks.md) |
| `extends` | Presets inherited from a Stack. The repository's own values always win; `orun intent explain` shows where each field came from | [Intent presets](./intent-presets.md) |
| `discovery.roots` | Directories scanned recursively for `component.yaml`, relative to the intent file. The intent file also marks the workspace root | [Context discovery](./context-discovery.md) |
| `env` | Platform-wide environment variables, the lowest-precedence user-declared layer | [Runtime environment](./runtime-environment.md) |
| `groups` | Defaults shared by every component in a domain. A group is keyed by domain name: a component with `domain: platform-foundation` takes the `platform-foundation` group's defaults | [Merge model](#merge-model) |
| `environments` | Each target environment: which components it selects (by name or domain), its parameter defaults, `env`, activation, promotion order, and default dependency mode | [Environment promotion](./environment-promotion.md), [dependency rules](./dependency-rules.md) |
| `automation.triggerBindings` | Which CI events activate which environments, and how much of the repository each plans | [Trigger bindings](./trigger-bindings.md) |
| `components` | Optional inline components, for teams that want some declarations central | |

Groups and environments also accept a `policies` map. It is carried onto every component
instance, but the planner and runner do not act on it yet; see
[standards](./standards.md#declared-not-yet-enforced).

## `component.yaml`

A component manifest sits next to the code it describes. Its fields live under `spec`:

```yaml
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: network-foundation
spec:
  type: terraform                  # the golden path: a composition type
  domain: platform-foundation      # which group's defaults apply
  subscribe:
    environments:
      - name: development
        profile: pull-request      # how it may run in this environment
      - name: staging
        profile: verify
      - name: production
        profile: release
  parameters:                      # checked against the type's schema at plan time
    stackName: network-foundation
    terraformDir: .
    terraformVersion: 1.9.8
  dependsOn:
    - component: dns-zones
  env:
    SERVICE: platform-infra
```

- **`type`** binds the component to a composition. `orun plan` checks `parameters` against
  that composition's schema and fails if they do not match.
- **`subscribe.environments`** lists where the component ships, as names or as objects
  with a `profile`, `profileRules`, or `env`. See [profile rules](./profile-rules.md).
- **`dependsOn`** declares ordering between components. See
  [dependency rules](./dependency-rules.md).
- **`change`** declares which intent sections and which outside files count as a change to
  this component under `--changed`. See [change detection](./change-detection.md).
- **`secretEnv`** maps variables to `secret://` references, never to values. See
  [secrets](./secrets.md).

## Merge model

At plan time, each component's parameters are merged from lowest to highest precedence:

1. The environment's `parameterDefaults["*"]`, then `parameterDefaults[<type>]`.
2. The component's group's `parameterDefaults["*"]`, then `parameterDefaults[<type>]`.
3. The component's own `parameters`.

The merged result is then checked against the composition's schema. `path` has its own
order: the component's `path`, then the group default, then the environment default, then
`./`. Environment variables follow a separate ladder, described in
[runtime environment](./runtime-environment.md).

## What reads intent

- **The planner.** `orun validate` checks the intent and its rules; `orun plan` compiles
  it, with the components and compositions, into a deterministic `plan.json`. See the
  [plan DAG](./plan-dag.md).
- **The catalog.** Components, systems, domains, environments, and compositions are
  derived from intent, never curated by hand. See the [service catalog](./service-catalog.md).
- **Agents.** `orun mcp serve` answers an agent's questions about the repository from the
  same intent. See [agents in orun repositories](../ai-context/orun-repositories.md).
- **People.** `orun intent explain` and `orun intent render` show the effective intent
  after presets; `orun component <name>` shows one component's merged view.

Next: [standards](./standards.md), the rules you can declare and which ones orun enforces,
then [compositions](./compositions.md).
