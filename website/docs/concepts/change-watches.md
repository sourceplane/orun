---
title: Change watches
---

When `intent.yaml` changes, `orun` no longer marks all components as changed by default. Instead, only components that explicitly opt in via `spec.change.watches` are affected by global intent changes.

This prevents a small environment default tweak from triggering every component in a large repository.

## The problem

Before v2.3.0, any change to a global intent section (environments, groups, env, etc.) would mark **all** components as changed. In repositories with many components, editing a single environment default would produce a full-repo plan — even if most components don't depend on that default.

## How it works

Add a `change.watches` field to your component manifest to declare which intent sections should trigger the component:

```yaml
apiVersion: sourceplane.io/v1
kind: Component

metadata:
  name: api-platform

spec:
  type: terraform
  domain: platform

  subscribe:
    environments: [dev, staging, production]

  change:
    watches:
      - environments
      - groups
```

When `intent.yaml` changes and the semantic diff identifies that the `environments` section was modified, only components with `environments` in their `change.watches` list are marked changed.

## Valid watch values

| Value | Triggers when |
| --- | --- |
| `environments` | Any change to `environments:` in intent.yaml |
| `groups` | Any change to `groups:` in intent.yaml |
| `env` | Any change to root-level `env:` in intent.yaml |
| `automation` | Any change to `automation:` in intent.yaml |
| `compositions` | Any change to `compositions:` in intent.yaml |
| `discovery` | Any change to `discovery:` in intent.yaml |
| `execution` | Any change to `execution:` in intent.yaml |

## Default behavior

```yaml
spec:
  change:
    watches: []
```

If a component has no `change.watches` (or an empty list), global intent changes do **not** mark it as changed. Only direct file changes (component source files or `component.yaml` itself) trigger change detection for that component.

## What is NOT affected by watches

These change detection rules remain unchanged regardless of watches:

- **Component source files changed** — always marks the component (no watch needed)
- **`component.yaml` changed** — always marks the component (no watch needed)
- **Inline component changed in `intent.components`** — always marks that specific inline component (no watch needed)
- **Formatting-only intent changes** — never marks any component

Watches only control the behavior when a **global intent section** changes.

## The `--intent-impact` flag

Use `--intent-impact` to override the default watch behavior:

```bash
# Default: only components with matching watches are marked
orun plan --changed --intent-impact=watch

# Legacy behavior: all components marked on any global intent change
orun plan --changed --intent-impact=all

# Suppress all intent-based changes (only file-based detection)
orun plan --changed --intent-impact=none
```

| Value | Behavior |
| --- | --- |
| `watch` (default) | Only components whose watches match the changed sections |
| `all` | All components marked changed (pre-v2.3 behavior) |
| `none` | No components marked from intent changes |

## Debugging with `--explain`

Use `orun catalog affected` to see exactly which components a change selected — the watch-matched components show up in `directlyChanged`:

```bash
orun catalog affected --json
```

```json
{
  "directlyChanged": ["api-platform", "network-foundation"],
  "dependents": [],
  "affected": ["api-platform", "network-foundation"],
  "selection": ["api-platform", "network-foundation"],
  "intentMode": "global",
  "confidence": "high"
}
```

Here a global `intent.yaml` change matched the `environments`/`groups` watches on
`api-platform` and `network-foundation`, while `docs-site` and `worker` (no
matching watches) were left out. `--intent-impact=all` would select every
component; `none` would select none from the intent change.

## Migration from pre-v2.3

If you relied on the old behavior where any intent change triggered all components:

1. **Quick fix**: Add `--intent-impact=all` to your CI commands
2. **Recommended**: Add `change.watches` to components that actually depend on global intent sections

To identify which components need watches, review your intent file and ask: "If this section changes, which components actually need to be re-planned?"

### Example migration

Before (all components implicitly react to everything):

```yaml
spec:
  type: terraform
  subscribe:
    environments: [dev, staging]
```

After (explicit opt-in):

```yaml
spec:
  type: terraform
  subscribe:
    environments: [dev, staging]
  change:
    watches:
      - environments
      - groups
```

## Relationship to dependency resolution

Watch-based change detection happens **before** dependency resolution. If component A is marked changed via a watch match, and component B depends on A, then B is also included in the plan — even if B has no watches.

The full pipeline:

```
changed files
  → file-to-component detection
  → intent semantic diff (identifies changed sections)
  → component watches matched against changed sections
  → final changed component set
  → dependency resolution (adds transitive dependencies)
  → filtered plan
```
