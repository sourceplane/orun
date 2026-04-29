---
title: Deployment Paths
---

## Wrangler direct upload {#wrangler-direct-upload}

The `cloudflare-pages` composition builds the site in CI and then uses `wrangler pages deploy` to
push the generated `docs-build/` directory to a Direct Upload Pages project.

Key inputs:

- `siteDir`
- `installCommand`
- `buildCommand`
- `outputDir`
- `projectName`
- `productionBranch`

That path is useful when you want deployment to follow the build artifact produced by Gluon.

## Turbo worker deploy {#turbo-worker-deploy}

The `cloudflare-worker-turbo` composition installs pnpm, runs workspace-level install, build, and
typecheck commands, then uses the app-local Wrangler config to publish a Worker from `apps/*`.

Key inputs:

- `workspaceDir`
- `appDir`
- `installCommand`
- `buildCommand`
- `typecheckCommand`
- `deployCommand`
- `pnpmVersion`

That path is useful when each Worker app owns its own `component.yaml` and `wrangler.jsonc`, but the
real install and build still need to happen from the monorepo root.

## Turbo Pages direct upload {#turbo-pages-direct-upload}

The `cloudflare-pages-turbo` composition follows the same workspace-root install and build pattern,
then direct-uploads an app-specific output directory such as `apps/web-console/dist`.

Key inputs:

- `workspaceDir`
- `appDir`
- `installCommand`
- `buildCommand`
- `outputDir`
- `projectName`
- `pnpmVersion`

That path is useful when the Pages app lives under `apps/`, the component manifest should stay next
to the app, and Gluon should still own the final build artifact.

## Terraform Git source {#terraform-git-source}

The `cloudflare-pages-terraform` composition validates the same local site build, then reconciles a
`cloudflare_pages_project` resource that points Cloudflare Pages at this GitHub repository.

Key inputs:

- `terraformDir`
- `rootDir`
- `cloudflareBuildCommand`
- `repoOwner`
- `repoName`
- `projectName`

That path is useful when you want the Pages project itself versioned as infrastructure and let
Cloudflare build from Git after the project is connected.

## Turbo Pages Terraform {#turbo-pages-terraform}

The `cloudflare-pages-turbo-terraform` composition keeps the component manifest inside an `apps/*`
directory, performs the local pnpm and Turbo build from the workspace root, and then points Terraform
at an `infra/*` directory that owns the Cloudflare Pages project contract.

Key inputs:

- `workspaceDir`
- `appDir`
- `outputDir`
- `destinationDir`
- `rootDir`
- `terraformDir`
- `cloudflareBuildCommand`
- `pnpmVersion`

That path is useful when platform teams want app-local manifests for discovery and ownership, but
still want the Cloudflare Pages project itself versioned under infrastructure code.

## Choosing between them

- Use Wrangler when Gluon should own the build artifact and publish it directly.
- Use Terraform when Gluon should manage the Pages project contract and let Cloudflare own the Git-triggered deploys.
- Use the Turbo variants when the reusable manifest should live inside an app or package directory while the install and build still need the monorepo root.