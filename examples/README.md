# example-platform

This embedded sample mirrors the standalone `example-platform-repo` layout inside `orun/examples/`. It demonstrates intent-driven composition source resolution, repository discovery across multiple roots, and GitHub Actions-compatible `use:` steps inside packaged compositions.

The example includes:

- `apps/*` for Cloudflare Worker and Pages-style apps
- `infra/*` for Terraform stacks
- `deploy/*` for Helm values components
- `charts/*/chart` for Helm chart components
- `packages/*` for shared monorepo packages
- `compositions/` for the packaged `CompositionPackage`
- `website/` for the docs-site example plus provider smoke assets

`intent.yaml` resolves its compositions from `./compositions`, then discovers component manifests from `apps/`, `infra/`, `deploy/`, `charts/`, `packages/`, and `website/`.

Run the sample from the repository root:

```bash
./orun validate --intent examples/intent.yaml
./orun plan --intent examples/intent.yaml --output /tmp/orun-example-plan.json --view dag
./orun plan --intent examples/intent.yaml --component network-foundation --env development --output /tmp/orun-example-gha-plan.json
./orun run --plan /tmp/orun-example-gha-plan.json --workdir examples --gha
```

The last command exercises a real packaged composition that installs Terraform through `hashicorp/setup-terraform` and then validates the local stack from the embedded example repo.

## saas-baseline: build, plan, and run a product

[`saas-baseline/`](saas-baseline/) is the baseline from the main README's
recording. It is self-contained and separate from the sample above. Build a
product from it, then plan and run it locally:

```bash
cd examples
orun new --blueprint saas-baseline/blueprint.yaml --out acme-shop --set name=acme-shop
cd acme-shop && orun plan
```

See [`saas-baseline/README.md`](saas-baseline/README.md).
