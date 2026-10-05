---
title: Intent presets
description: Intent presets let a platform team ship its platform rules - environments, trigger bindings, defaults, discovery roots - as a versioned part of a Stack that every repository inherits with extends, so the standard lives in one place and evolves by version.
---

An intent preset is how a platform team **ships its platform rules as versioned code**.
Environment names and their activation triggers, the trigger bindings themselves,
parameter defaults per environment and per domain, discovery-root conventions,
organisation-wide `env`: instead of every repository copying them into its own
`intent.yaml` and slowly drifting, a preset publishes them once, inside a
[Stack](./stacks.md), and each repository inherits them with one line of `extends:`.

Updating the standard is a new Stack version. A repository adopts it by changing its source
pin, and `orun intent explain` and the plan diff show exactly what changed.

## The three steps

### 1. A Stack declares its presets

A preset is a file inside a Stack, listed in `stack.yaml` under `spec.intentPresets`:

```yaml
# stack.yaml
apiVersion: orun.io/v1
kind: Stack
metadata:
  name: aws-platform-stack
  version: 1.0.0
registry:
  host: ghcr.io
  namespace: sourceplane
  repository: aws-platform-stack
spec:
  intentPresets:
    - name: standard
      path: presets/standard.yaml
    - name: github-actions
      path: presets/github-actions.yaml
```

Preset files travel with the Stack when it is packed and published.

### 2. The preset declares the rules

A preset is a `kind: IntentPreset` document whose `spec` can carry `env`, `discovery`,
`automation`, `environments`, and `groups`, with the same shapes as in `intent.yaml`:

```yaml
# presets/github-actions.yaml
apiVersion: sourceplane.io/v1alpha1
kind: IntentPreset
metadata:
  name: github-actions
spec:
  env:
    ORG: sourceplane

  discovery:
    roots: [apps/, infra/, deploy/]

  automation:
    triggerBindings:
      github-pull-request:
        on:
          provider: github
          event: pull_request
          baseBranches: [main]
        plan:
          scope: changed
          base: pull_request.base.sha
          head: pull_request.head.sha
      github-push-main:
        on:
          provider: github
          event: push
          branches: [main]
        plan:
          scope: changed
          base: before
          head: after

  environments:
    dev:
      activation:
        triggerRefs: [github-pull-request]
      parameterDefaults:
        "*":
          lane: pull-request
    staging:
      activation:
        triggerRefs: [github-push-main]
      parameterDefaults:
        "*":
          lane: verify
```

A preset cannot declare `compositions.sources` or `components`; those belong to the
repository.

### 3. The repository opts in

The repository declares the Stack as a composition source and names the presets it
inherits:

```yaml
# intent.yaml
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: aws-admin

compositions:
  sources:
    - name: aws-platform
      kind: oci
      ref: oci://ghcr.io/sourceplane/aws-platform-stack:v1.0.0

extends:
  - source: aws-platform        # must name a declared composition source
    preset: github-actions

env:
  REPO: aws-admin

environments:
  production:
    parameterDefaults:
      "*":
        awsAccountId: "123456789012"
```

Nothing is inherited implicitly: a Stack never injects rules into a repository that did
not list the preset under `extends:`.

## How presets merge

Presets **fill in what the repository leaves out**. orun starts from the repository's own
intent and applies each preset in `extends:` order, adding only what is not already set.
So the repository always wins, and among presets the **first** one to set a value wins.

| Field | How a preset contributes |
|-------|---------------|
| `env` | Adds keys the repository (or an earlier preset) has not set |
| `discovery.roots` | Union, deduplicated |
| `automation.triggerBindings` | Adds bindings by name that are not already declared |
| `groups` | A group the repository does not declare is added whole. For an existing group: `parameterDefaults` keys not already set are added; `path` is added if unset; `policies` are overwritten (see below) |
| `environments` | An environment the repository does not declare is added whole. For an existing environment: `parameterDefaults` and `env` keys not already set are added; `activation.triggerRefs` is a union; `selectors` and `path` are added only if the repository has none; `policies` are overwritten. `promotion`, `dependencyMode`, and `secretEnv` come only from the repository |
| `compositions.sources`, `components` | Never merged; repository-owned |

**`policies` are the exception.** A preset's `policies` keys are written over the
repository's and over earlier presets', so on a conflicting key the last preset wins.
That is what lets a platform team ship a rule in a Stack that an adopting repository
cannot loosen. Policies are enforced by `orun validate`, `orun plan`, and `orun run`; see
[policies](./intent-model.md#policies).

```text
repo intent.yaml  →  + preset 1 (fills gaps)  →  + preset 2 (fills remaining gaps)  →  effective intent
(always wins)          wins over later presets
```

## Seeing the effective intent

`orun intent explain` lists every field a preset contributed and which preset it came
from:

```bash
orun intent explain
```

`orun intent render` prints the fully merged intent, which is what the planner sees:

```bash
orun intent render
orun intent render --output /tmp/effective-intent.yaml
```

Presets are applied by `orun plan` and the `orun intent` commands. `orun validate` checks
the repository's own `intent.yaml` without applying presets, so run `orun plan` in CI to
check the effective intent.

## What belongs in a preset

**Good candidates** are the rules every repository on the platform should share:

- Environment names, their activation triggers, and their trigger bindings
- Discovery-root conventions
- Default lanes and parameters per environment and per domain
- Organisation-wide `env`

**Keep in the repository** what is genuinely its own:

- Components and composition sources (presets cannot declare them)
- Promotion order and dependency modes (presets do not merge them into an existing
  environment)
- Repository-specific `env` and secret references

## Presets, Stacks, and baselines

- A **Stack** packages golden paths (compositions) and platform rules (presets) together,
  versioned as one OCI artifact. See [Stacks](./stacks.md).
- A **preset** is the platform-rules half of a Stack, inherited with `extends:`.
- A **baseline** packages a whole product built on those standards, rebuilt for a new
  owner and upgraded rather than forked. See [baselines](./baselines.md).

How a repository pins a Stack version, and how to see what a version change does, is
covered in [versioning and locking](./versioning-and-locking.md).
