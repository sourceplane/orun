---
title: Composition examples
---

The repository ships one packaged example-platform Stack that covers the quick start, GitHub Actions-compatible execution, and multi-root repository discovery.

## Packaged examples

| Package | Exports | Purpose |
| --- | --- | --- |
| `examples/compositions` | `terraform`, `helm-values`, `helm-chart`, `cloudflare-worker-turbo`, `cloudflare-worker`, `cloudflare-pages`, `cloudflare-pages-turbo`, `cloudflare-pages-terraform`, `cloudflare-pages-turbo-terraform`, `turbo-package`, `workspace` | Embedded example-platform Stack used by `examples/intent.yaml` |

Each composition type lives at `examples/compositions/compositions/<type>/compositions.yaml`. The `stack.yaml` at the package root uses auto-discovery — no explicit path list is needed.

## GitHub Actions-compatible composition

The embedded example package includes several compositions that install tools with GitHub Actions before running shell commands. The smallest smoke path is the Terraform example used by `network-foundation`.

That example shows how a job can include a GitHub Actions `use:` step followed by ordinary shell commands:

```yaml
steps:
  - id: setup-terraform
    name: setup-terraform
    use: hashicorp/setup-terraform@v4
    with:
      terraform_version: "{{.terraformVersion}}"
      terraform_wrapper: "false"
  - name: terraform-context
    run: |
      terraform version
      terraform -chdir={{.terraformDir}} validate -no-color
```

Use that example as a reference when you need Actions-style setup behavior in a compiled plan.

The legacy folder-shaped compositions under `assets/config/compositions` remain in the repository as compatibility fixtures for `--config-dir`, but the packaged Stack examples above are the recommended authoring pattern.
