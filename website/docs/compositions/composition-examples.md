---
title: Composition examples
---

The repository ships packaged composition examples for the main quick start and the GitHub Actions-compatible runner.

## Packaged examples

| Package | Exports | Purpose |
| --- | --- | --- |
| `examples/packages/platform-core` | `charts`, `helm`, `helmCommon`, `terraform` | Repository quick-start package used by `examples/intent.yaml` |
| `examples/gha-actions/packages/gha-demo` | `gha-demo` | Minimal package that exercises `use:` steps and the GitHub Actions backend |

## Example-only GitHub Actions composition

The GitHub Actions example package exports a `gha-demo` composition that installs Helm with a GitHub Action and then uses the binary in a later shell step.

That example shows how a job can include a GitHub Actions `use:` step followed by ordinary shell commands:

```yaml
steps:
  - id: setup-demo
    name: setup-demo
    use: azure/setup-helm@v4.3.0
  - name: verify-gha-state
    run: |
      helm version --short
      which helm
```

Use that example as a reference when you need Actions-style setup behavior in a compiled plan.

The legacy folder-shaped compositions under `assets/config/compositions` remain in the repository as compatibility fixtures for `--config-dir`, but the packaged examples above are the recommended authoring pattern.