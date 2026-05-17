---
title: Trigger bindings
---

Trigger bindings map CI provider events to environment activation. They let you declare which environments should be planned for each type of event — pull requests activate development, pushes to main activate staging, release tags activate production.

## How it works

```text
Provider event → TriggerBinding match → Environment activation → Plan
```

When `orun plan` runs with a trigger context (`--trigger` or `--from-ci`), it:

1. Normalizes the raw provider event into a standard format
2. Matches the event against declared trigger bindings
3. Activates only environments that reference the matched triggers
4. Plans only components subscribed to those active environments

Without a trigger context, all environments are planned as before.

## Declaring trigger bindings

Trigger bindings live under `automation.triggerBindings` in your intent file:

```yaml
automation:
  triggerBindings:
    github-pull-request:
      description: Pull request validation
      on:
        provider: github
        event: pull_request
        actions:
          - opened
          - synchronize
          - reopened
        baseBranches:
          - main
      plan:
        scope: changed
        base: pull_request.base.sha
        head: pull_request.head.sha

    github-push-main:
      description: Main branch verification
      on:
        provider: github
        event: push
        branches:
          - main
      plan:
        scope: changed
        base: before
        head: after

    github-tag-release:
      description: Release planning
      on:
        provider: github
        event: push
        tags:
          - "v*"
      plan:
        scope: full
```

## Activating environments

Environments reference trigger bindings through `activation.triggerRefs`:

```yaml
environments:
  development:
    activation:
      triggerRefs:
        - github-pull-request
    parameterDefaults:
      "*":
        namespacePrefix: dev-

  staging:
    activation:
      triggerRefs:
        - github-push-main
    parameterDefaults:
      "*":
        namespacePrefix: stg-

  production:
    activation:
      triggerRefs:
        - github-tag-release
    parameterDefaults:
      "*":
        namespacePrefix: prod-
```

When `github-pull-request` fires, only `development` is activated. Components subscribed to `development` are planned; components only subscribed to `staging` or `production` are skipped.

## Event matching

Each trigger binding declares match criteria under `on`:

| Field | Purpose |
| --- | --- |
| `provider` | Event provider (required, e.g. `github`) |
| `event` | Event name (required, e.g. `push`, `pull_request`) |
| `actions` | Optional action filter (e.g. `opened`, `synchronize`) |
| `branches` | Optional source branch filter with glob support |
| `baseBranches` | Optional base branch filter (for PRs) with glob support |
| `tags` | Optional tag filter with glob support |

All specified fields must match for the trigger to fire. Unspecified fields are not checked.

### Glob patterns

Branch and tag filters support simple glob patterns:

```yaml
branches:
  - main          # exact match
  - release/*     # prefix match (release/1.0, release/hotfix)
  - feature/**    # same as feature/*

tags:
  - "v*"          # matches v1.0.0, v2.3.1-rc1
  - "*"           # matches any non-empty value
```

## Plan scope

Each trigger binding can set `plan.scope` to control how the plan is scoped:

| Scope | Behavior |
| --- | --- |
| `full` | Plan all components in activated environments |
| `changed` | Plan only changed components (enables `--changed` mode) |

When `scope: changed` is set, the trigger also needs `base` and `head` to determine the diff range. These are dot-paths into the raw event payload:

```yaml
plan:
  scope: changed
  base: pull_request.base.sha   # resolves from event JSON
  head: pull_request.head.sha
```

For push events, the standard paths are `before` and `after`.

## CLI usage

### Named trigger simulation (local development)

```bash
orun plan --trigger github-pull-request
```

This activates the named trigger directly without requiring a real event. Useful for local reproduction of CI behavior.

### Provider event file (CI)

```bash
orun plan \
  --from-ci github \
  --event-file "$GITHUB_EVENT_PATH"
```

This reads the raw event JSON, normalizes it, matches against all declared trigger bindings, and activates the corresponding environments.

### Combining with --env

When both a trigger and `--env` are provided, the result is the intersection:

```bash
orun plan --trigger github-pull-request --env development
```

If the specified environment is not activated by the trigger, planning fails with an error.

## Multiple matched triggers

When an event matches multiple trigger bindings, orun:

1. Collects all matched trigger names (sorted alphabetically)
2. Activates the union of all referenced environments
3. Checks for conflicting `plan.scope` values — if triggers disagree, planning fails with a clear error

## Plan metadata

When a trigger is used, the resulting plan includes trigger metadata:

```json
{
  "metadata": {
    "trigger": {
      "mode": "event-file",
      "provider": "github",
      "event": "pull_request",
      "action": "synchronize",
      "matchedBindings": ["github-pull-request"],
      "activeEnvironments": ["development"],
      "scope": "changed",
      "base": "abc123",
      "head": "def456"
    }
  }
}
```

## Validation

Static validation (`orun validate`) checks:

- Trigger bindings have required `on.provider` and `on.event` fields
- `plan.scope` is either `full` or `changed` (if set)
- Environment `triggerRefs` point to existing trigger bindings
- No duplicate refs within an environment

A warning (not error) is produced for trigger bindings that no environment references.

## Backwards compatibility

Existing intent files without `automation` or `activation` continue to work unchanged. The trigger system is opt-in:

- `orun plan` without `--trigger` or `--from-ci` ignores all activation rules
- Environments without `activation.triggerRefs` are never activated by triggers (but are always included in non-trigger plans)
