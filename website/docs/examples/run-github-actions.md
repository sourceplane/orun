---
title: Run with GitHub Actions compatibility
---

The repository includes a minimal example that installs Helm through a GitHub Action and then uses the resulting binary from a later shell step.

## Compile the example plan

```bash
gluon plan \
  --intent examples/gha-actions/intent.yaml \
  --output /tmp/gluon-gha-actions-plan.json
```

The example intent declares its packaged `gha-demo` composition source, so no extra composition path flag is required.

## Run the plan

```bash
gluon run \
  --plan /tmp/gluon-gha-actions-plan.json
```

Because the plan contains a `use:` step, `gluon run` auto-selects the `github-actions` backend unless you explicitly override it.

## Force the backend explicitly

```bash
gluon run \
  --plan /tmp/gluon-gha-actions-plan.json \
  --gha
```

Use the explicit flag when you want the command line itself to document that the plan requires GitHub Actions semantics.
