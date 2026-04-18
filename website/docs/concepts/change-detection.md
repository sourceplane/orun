---
title: Change detection
---

`arx` can narrow inspection and planning to changed files or changed components. That is useful in pull requests, preview environments, and incremental review workflows.

## Commands that support change detection

- `arx plan`
- `arx component`

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

## Pull request review flow

```bash
arx component \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --changed \
  --base main \
  --long

arx plan \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --changed \
  --base main \
  --output /tmp/pr-plan.json
```

## Explicit file lists

When your CI system already knows the changed files, pass them directly instead of asking `arx` to compute the diff:

```bash
arx plan \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --files examples/services/web-app/component.yaml,examples/intent.yaml \
  --output /tmp/filtered-plan.json
```

## Local development flow

For uncommitted work in a repository checkout:

```bash
arx component \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --changed \
  --uncommitted
```

Use change detection to reduce noise during review, not to hide full-environment validation when you need a canonical plan for release.