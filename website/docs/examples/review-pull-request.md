---
title: Review a pull request
---

This example shows how to use `arx` in a review flow where you want to focus on changed components before generating a plan.

## Inspect changed components

```bash
arx component \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --changed \
  --base main \
  --long
```

That produces a merged view of the components affected by the current branch.

## Generate a review-scoped plan

```bash
arx plan \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --changed \
  --base main \
  --output /tmp/pr-plan.json \
  --view dependencies
```

## Use explicit file lists in CI

If your CI platform already exposes the changed file list, pass it directly:

```bash
arx plan \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --files examples/services/web-app/component.yaml,examples/intent.yaml \
  --output /tmp/pr-plan.json
```

Use this pattern when you want fast signal for reviewers, then follow up with a full plan in release or merge workflows.