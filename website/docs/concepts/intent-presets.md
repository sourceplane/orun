---
title: Intent Presets
---

# Intent Presets

Intent Presets allow Stack packages to publish reusable intent scaffolding — environments, trigger bindings, defaults, policies, discovery roots, and env vars — that consuming repos can explicitly opt into via `extends:` in their `intent.yaml`.

## Problem

Without presets, every Orun repo must repeat the same environment names, trigger bindings, default profile mappings, group defaults, and policy scaffolding. This creates drift across an organization. A Stack preset lets a platform team publish a "golden repo baseline" so each repo only declares what is truly repo-specific.

## How It Works

### 1. Stack Declares Presets

A Stack package can include intent presets alongside its compositions:

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
  compositions:
    - path: compositions/terraform/compositions.yaml
    - path: compositions/helm/compositions.yaml
  intentPresets:
    - name: standard
      path: presets/standard.yaml
    - name: github-actions
      path: presets/github-actions.yaml
```

### 2. Write the Preset

Presets use the `IntentPreset` kind and can declare any combination of env vars, discovery roots, automation trigger bindings, environments, and groups:

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
    roots:
      - apps/
      - infra/
      - deploy/

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

### 3. Repo Opts In

The consuming repo's `intent.yaml` uses `extends:` to inherit from presets. The repo must declare the composition source first, then reference it:

```yaml
# intent.yaml
apiVersion: sourceplane.io/v1
kind: Intent

metadata:
  name: aws-admin
  namespace: sourceplane

compositions:
  sources:
    - name: aws-platform
      kind: oci
      ref: oci://ghcr.io/sourceplane/aws-platform-stack:v1.0.0

extends:
  - source: aws-platform
    preset: github-actions

env:
  OWNER: sourceplane
  REPO: aws-admin

environments:
  production:
    parameterDefaults:
      "*":
        awsAccountId: "123456789012"
```

## Merge Rules

Presets are applied in declaration order. Later presets take precedence over earlier ones for non-conflicting fields. The repo intent always wins over any preset.

| Field | Merge Behavior |
|-------|---------------|
| `env` | Deep merge, repo wins on conflict |
| `discovery.roots` | Union (deduplicated) |
| `groups.defaults` | Deep merge, repo wins on conflict |
| `environments.defaults` | Deep merge, repo wins on conflict |
| `automation.triggerBindings` | Merge by name, repo wins on conflict |
| `policies` | Additive — preset policies always included |
| `compositions.sources` | Not merged (repo-owned) |
| `components` | Not merged (repo-owned) |

### Merge Order

```
Stack preset 1 → Stack preset 2 → ... → Repo intent.yaml
(lowest priority)                        (highest priority)
```

## Inspecting Presets

### Explain

See what each preset contributes:

```bash
orun intent explain
```

Output shows each preset and the fields it contributes with provenance tracking.

### Render

Emit the fully-merged effective intent:

```bash
orun intent render
orun intent render --output /tmp/effective-intent.yaml
```

The rendered output shows the final merged state — what the planner actually sees.

## What Should Go in a Preset

**Good candidates:**
- Standard environment names and activation triggers
- Discovery root conventions
- Default lanes and profiles per environment
- Organization-wide env vars
- Policy baselines (approval requirements, clean git tree)

**Avoid in presets:**
- Component declarations (repo-specific)
- Composition sources (repo-owned)
- Repo-specific env vars (secrets, repo names)

## Constraints

- The repo intent must visibly opt in via `extends:`. Stacks do not automatically inject behavior.
- Presets cannot declare `compositions.sources` or `components`.
- Preset policies are additive — repos cannot override them without explicit authorization.
- The `extends[].source` must reference a declared composition source name.
- The effective intent is always deterministic for the same inputs.
