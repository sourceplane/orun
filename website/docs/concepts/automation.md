---
title: Automation
---

**Automation** in orun connects CI/CD events to execution profiles and plan scoping. When a push, pull request, or tag event fires in your CI provider, orun can automatically select the right profile, set the plan scope, and determine base/head refs for change detection.

## Concepts

Automation has three parts:

1. **Trigger bindings** — declare which events select which profile and scope
2. **CI auto-detection** — orun reads CI environment variables to build an event context
3. **Override policies** — stack-level guardrails that limit what intent can customize

## Trigger bindings

A trigger binding is a document that maps a CI event to plan-generation settings.

```yaml
apiVersion: sourceplane.io/v1
kind: TriggerBinding
metadata:
  name: github-push-main
spec:
  on:
    provider: github
    event: push
    branches:
      - main
  plan:
    profileRef: verify
```

### Trigger fields

| Field | Meaning |
| --- | --- |
| `spec.on.provider` | CI platform: `github`, `gitlab`, or `buildkite` |
| `spec.on.event` | Event type: `push`, `pull_request`, `merge_request`, `release`, `workflow_dispatch` |
| `spec.on.actions` | Filter by event action (e.g., `opened`, `synchronize`) |
| `spec.on.branches` | Branch glob patterns (e.g., `main`, `release/*`) |
| `spec.on.tags` | Tag glob patterns (e.g., `v*`, `release-*`) |
| `spec.plan.profileRef` | Profile to select when this trigger matches |
| `spec.plan.scope` | Override plan scope: `full` or `changed` |
| `spec.plan.base` | Explicit base ref for change detection |
| `spec.plan.head` | Explicit head ref for change detection |

### Where triggers live

Trigger bindings can be defined in two places:

**Stack-level** — standalone YAML files in `triggers/` at the stack root:

```text
my-stack/
├── stack.yaml
├── triggers/
│   ├── github-push-main.yaml
│   ├── github-tag-release.yaml
│   └── github-pull-request.yaml
└── ...
```

These are auto-discovered when `spec.triggers` is omitted from `stack.yaml`.

**Intent-level** — inline in `intent.yaml` under `automation.triggers` and `automation.triggerBindings`:

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

:::tip
`automation.triggers` use `plan.profile` to reference a profile. Stack-level `TriggerBinding` documents use `plan.profileRef`. Both resolve identically.
:::

## Common trigger patterns

### Push to main — verify all changes

```yaml
apiVersion: sourceplane.io/v1
kind: TriggerBinding
metadata:
  name: github-push-main
spec:
  on:
    provider: github
    event: push
    branches:
      - main
  plan:
    profileRef: verify
```

### Tag release — full deploy

```yaml
apiVersion: sourceplane.io/v1
kind: TriggerBinding
metadata:
  name: github-tag-release
spec:
  on:
    provider: github
    event: push
    tags:
      - "v*"
  plan:
    profileRef: release
    scope: full
```

### Pull request — fast validation

```yaml
apiVersion: sourceplane.io/v1
kind: TriggerBinding
metadata:
  name: github-pull-request
spec:
  on:
    provider: github
    event: pull_request
    actions:
      - opened
      - synchronize
      - reopened
      - ready_for_review
  plan:
    scope: changed
    base: main
```

## CI environment auto-detection

The `--from-ci` flag tells orun to detect the CI platform automatically and build an event context from environment variables.

```bash
orun plan --from-ci github
```

### Supported providers

| Provider | Detection | Key environment variables |
| --- | --- | --- |
| GitHub Actions | `GITHUB_ACTIONS=true` | `GITHUB_EVENT_NAME`, `GITHUB_REF`, `GITHUB_BASE_REF`, `GITHUB_HEAD_REF`, `GITHUB_SHA` |
| GitLab CI | `GITLAB_CI` is set | `CI_PIPELINE_SOURCE`, `CI_MERGE_REQUEST_TARGET_BRANCH_NAME`, `CI_COMMIT_SHA` |
| Buildkite | `BUILDKITE` is set | `BUILDKITE_PIPELINE_DEFAULT_BRANCH`, `BUILDKITE_BRANCH`, `BUILDKITE_COMMIT` |

### Event normalization

Regardless of provider, orun normalizes CI variables into a standardized event context:

| Field | Meaning |
| --- | --- |
| `Provider` | Detected CI platform |
| `Event` | Normalized event type (`push`, `pull_request`) |
| `Action` | Optional action qualifier |
| `Ref` | Full git ref (e.g., `refs/heads/main`) |
| `BaseRef` | Base/target branch |
| `HeadSha` | Head commit SHA |
| `Branch` | Simplified branch name |
| `IsFork` | Whether the event is from a fork |

## Trigger matching

When an event context is available (via `--from-ci` or `--event-file`), orun evaluates triggers in declaration order using **first-match-wins** semantics:

1. Provider must match (case-insensitive)
2. Event type must match
3. If `actions` is specified, the event action must be in the list
4. If `branches` is specified, at least one pattern must match the branch
5. If `tags` is specified, at least one pattern must match the tag

Wildcard patterns use glob syntax: `main`, `release/*`, `v*`.

The first matching trigger's `plan` settings are applied to the plan generation.

## Using triggers from the CLI

```bash
# Auto-detect CI and match triggers
orun plan --from-ci github

# Use a specific trigger by name
orun plan --trigger github-push-main

# Load event context from a JSON file (useful for testing)
orun plan --event-file event.json
```

## Override policies

A **StackOverridePolicy** limits what the consuming intent can customize in profiles and controls. This prevents downstream users from enabling dangerous steps that the stack author has locked down.

```yaml
apiVersion: sourceplane.io/v1
kind: StackOverridePolicy
metadata:
  name: tectonic-default
spec:
  default: deny
  allow:
    intent:
      environments:
        defaults:
          - namespacePrefix
          - region
          - cluster.*
        policies:
          - requireApproval
          - requireSignedPlan
      profiles:
        pull-request:
          controls:
            terraform:
              plan:
                type: boolean
        release:
          controls:
            terraform:
              apply:
                type: boolean
  deny:
    - "profiles.*.controls.terraform.init.backend"
```

| Field | Meaning |
| --- | --- |
| `spec.default` | Default stance: `allow` or `deny` |
| `spec.allow.intent.environments` | Whitelisted environment defaults and policies |
| `spec.allow.intent.profiles` | Per-profile control fields the intent may override |
| `spec.deny` | Explicit denial patterns (supports `*` wildcards) |

Override policies are auto-discovered from `policies/*.yaml` in the stack root.

## How it all fits together

```text
CI event fires (push to main)
        ↓
orun plan --from-ci github
        ↓
detect GITHUB_ACTIONS env → build EventContext
        ↓
match triggers → github-push-main wins
        ↓
apply trigger plan: profileRef=verify
        ↓
resolve profile per component×environment
        ↓
generate plan with profile controls applied
```

Read [execution profiles](./profiles.md) for the full profile definition and resolution model.
