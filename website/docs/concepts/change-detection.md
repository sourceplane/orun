---
title: Change detection
---

`orun` can narrow inspection, planning, and execution to changed files or changed components. That is useful in pull requests, preview environments, and incremental review workflows.

Change detection is computed by a single engine over the **object-model catalog** — the same engine behind [`orun catalog affected`](../cli/orun-catalog.md) and the cockpit's changed/affected overlay. It classifies a change using the catalog's ownership map and dependency graph, and **over-reports rather than under-reports** on ambiguity, so a missed change never silently drops out of a plan. To inspect the engine's classification directly (the directly-changed, dependents, affected, and selection sets, with a confidence signal), use `orun catalog affected --json`.

## Commands that support change detection

- `orun plan`
- `orun component`
- `orun run`

Use those commands when you want to focus on the parts of the platform graph that were touched by a branch, commit range, or explicit file list.

## Available flags

| Flag | Purpose |
| --- | --- |
| `--changed` | Enable change-aware filtering based on git state |
| `--base <ref>` | Set the base ref used for comparison |
| `--head <ref>` | Set the head ref used for comparison |
| `--files <path,...>` | Override git diff resolution with an explicit file list |
| `--uncommitted` | Scope detection to uncommitted changes |
| `--untracked` | Scope detection to untracked files |
| `--intent-impact <mode>` | How global intent changes affect components (`watch`/`all`/`none`, default: `watch`) |
| `--explain` | Print how `--changed` resolved its base and head refs (useful for debugging) |

## CI auto-detection

When `--changed` is used inside a CI environment without explicit `--base` or `--head` flags, `orun` automatically infers the correct refs from environment variables:

| CI system | Base ref source | Head ref source |
| --- | --- | --- |
| GitHub Actions (`pull_request`) | `GITHUB_BASE_REF` | `GITHUB_SHA` |
| GitHub Actions (`push`) | Previous commit | `GITHUB_SHA` |
| Other CI | Falls back to `main` | `HEAD` |

This means a simple `--changed` flag is sufficient in most CI pipelines — no manual ref wiring needed:

```bash
orun plan --changed
orun run --changed
```

Use `--explain` to see exactly which refs were resolved:

```bash
orun plan --changed --explain
```

## Pull request review flow

```bash
orun component \
  --intent examples/intent.yaml \
  --changed \
  --base main \
  --long

orun plan \
  --intent examples/intent.yaml \
  --changed \
  --base main \
  --output /tmp/pr-plan.json
```

To generate and run a change-scoped plan in a single step:

```bash
orun run --changed --base main
```

## Explicit file lists

When your CI system already knows the changed files, pass them directly instead of asking `orun` to compute the diff:

```bash
orun plan \
  --intent examples/intent.yaml \
  --files examples/infra/infra-1/component.yaml,examples/intent.yaml \
  --output /tmp/filtered-plan.json
```

## Local development flow

For uncommitted work in a repository checkout:

```bash
orun component \
  --intent examples/intent.yaml \
  --changed \
  --uncommitted
```

## Debugging ref resolution

When `--changed` produces unexpected results, `--explain` shows the full resolution trace — which CI variables were detected, which git commands were run, and which refs were ultimately used:

```bash
orun plan --changed --explain
orun run --changed --explain
```

Use change detection to reduce noise during review, not to hide full-environment validation when you need a canonical plan for release.

## Intent-aware change scoping

When `intent.yaml` itself is in the changed file set, `orun` performs a **semantic diff** to determine whether the change affects all components or only specific inline components:

| What changed in intent.yaml | Effect |
| --- | --- |
| Top-level `env`, `environments`, `groups`, `discovery`, `compositions`, `automation`, or `execution` | Only components with matching `change.watches` are marked changed |
| Only entries under top-level `components: []` | Only the added/modified/removed inline component names are marked changed |
| Formatting, comments, or component reordering without content change | No components marked changed from intent |
| Both `components` and another section | Global sections use watch matching; inline component changes are always direct |

By default, a global intent section change does **not** mark any component as changed unless that component explicitly opts in with [`spec.change.watches`](./change-watches.md). Use `--intent-impact=all` for pre-v2.3 behavior where all components are marked.

If the base or head intent cannot be parsed (e.g. the file is new), the engine escalates to a global intent change (over-reporting) rather than risk missing an affected component.

To inspect *which* components a change selected and *why*, query the engine directly:

```bash
orun catalog affected --json
# {
#   "directlyChanged": ["api-platform"],
#   "dependents": ["web-gateway"],
#   "affected": ["api-platform", "web-gateway"],
#   "selection": ["api-platform"],
#   "intentMode": "components",
#   "confidence": "high",
#   "needsFullResolve": false
# }
```

`--explain` on `plan`/`run` covers **ref resolution** (which base/head were used); the per-component classification and its provenance live in `orun catalog affected`.

## Trigger-driven change detection

When using [trigger bindings](./trigger-bindings.md) with `plan.scope: changed`, the trigger system automatically enables change detection with the base/head refs resolved from the event payload:

```yaml
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
```

When this trigger fires via `--from-ci github --event-file "$GITHUB_EVENT_PATH"`, orun resolves `base` and `head` from the event JSON and enables `--changed` mode automatically. No explicit `--changed` flag is needed:

```bash
orun plan --from-ci github --event-file "$GITHUB_EVENT_PATH"
```

CLI `--base` and `--head` flags override trigger-derived values when you need manual control.
