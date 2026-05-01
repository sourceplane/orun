---
title: Change detection
---

`orun` can narrow inspection, planning, and execution to changed files or changed components. That is useful in pull requests, preview environments, and incremental review workflows.

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
