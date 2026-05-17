---
title: Stacks
---

A **Stack** is the standard packaging format for distributing composition types in orun. It replaces the legacy `orun.yaml` / `CompositionPackage` format and is the recommended way to author, version, and share platform compositions.

## What is a Stack?

A Stack is a directory rooted at a `stack.yaml` manifest (apiVersion: `orun.io/v1`, kind: `Stack`). It bundles one or more composition types — each using split-kind authoring — together with metadata and an optional OCI registry target.

```text
my-platform/
├── stack.yaml                        ← Stack manifest
└── compositions/
    ├── terraform/
    │   ├── composition.yaml          ← Composition facade
    │   ├── schema.yaml               ← ComponentSchema
    │   ├── jobs/
    │   │   └── terraform-validate.yaml   ← JobTemplate
    │   └── profiles/
    │       ├── terraform-pull-request.yaml  ← ExecutionProfile
    │       ├── terraform-verify.yaml
    │       └── terraform-release.yaml
    └── helm-chart/
        ├── composition.yaml
        ├── schema.yaml
        ├── jobs/
        │   └── helm-chart-render.yaml
        └── profiles/
            ├── helm-chart-lint-only.yaml
            └── helm-chart-verify.yaml
```

Each composition type uses split-kind authoring: a `Composition` facade references a `ComponentSchema`, one or more `JobTemplate` documents, and `ExecutionProfile` documents by name.

## Auto-discovery

When `spec.compositions` is omitted from `stack.yaml`, the packager walks the directory tree and automatically discovers composition documents (`composition.yaml`, `schema.yaml`, job templates, and profiles). The composition name is taken from the parent directory:

```text
compositions/terraform/composition.yaml  →  type "terraform"
compositions/helm-chart/composition.yaml →  type "helm-chart"
```

No path listing is required. Drop new composition subdirectories in and they are included automatically on the next pack or publish.

## Stack manifest

```yaml
apiVersion: orun.io/v1
kind: Stack
metadata:
  name: my-platform-stack           # unique package name
  title: My Platform Stack          # human-readable title
  version: 1.0.0                    # semver
  description: Platform compositions for my-platform
  owner: my-org
  tags:
    - terraform
    - helm
registry:
  host: ghcr.io
  namespace: my-org
  repository: my-platform-stack
  visibility: public                # public | private
```

The `registry` block is used to infer the OCI publish target when running `orun publish` without an explicit `--ref`.

To pin specific files instead of relying on auto-discovery, add `spec.compositions`:

```yaml
spec:
  compositions:
    - path: compositions/terraform/composition.yaml
    - path: compositions/helm-chart/composition.yaml
```

## Packaging a Stack

Build a local `.tgz` archive (no registry required):

```bash
orun pack --root ./my-platform
orun pack --root ./my-platform --output dist/my-platform-stack-1.0.0.tgz
```

## Publishing a Stack to an OCI registry

Log in first, then stream-publish directly from the directory. No temp file is written:

```bash
orun login ghcr.io
orun publish --root ./my-platform
```

`orun publish` reads the `registry` block from `stack.yaml` to determine the target. Override with `--ref`:

```bash
orun publish --root ./my-platform --ref ghcr.io/my-org/my-platform-stack:v1.0.0
```

Dry-run to validate the target without uploading:

```bash
orun publish --root ./my-platform --dry-run
```

### OCI artifact layout

Stacks are published as multi-layer OCI artifacts:

| Layer | Media type | Content |
|---|---|---|
| compositions | `application/vnd.orun.stack.compositions.layer.v1+tar+gzip` | `stack.yaml` + `compositions/` tree |
| examples | `application/vnd.orun.stack.examples.layer.v1+tar+gzip` | `examples/` tree (optional) |

Consumers pull only the compositions layer, avoiding the (potentially large) examples layer.

## Using a remote Stack

Reference a published Stack by OCI ref in your `intent.yaml`:

```yaml
compositions:
  sources:
    - name: platform
      kind: oci
      ref: oci://ghcr.io/my-org/my-platform-stack:v1.0.0
```

Pin the resolved digest for reproducible, air-gapped builds:

```yaml
compositions:
  sources:
    - name: platform
      kind: oci
      ref: oci://ghcr.io/my-org/my-platform-stack:v1.0.0
      digest: sha256:abc123...
```

Or generate a lock file with `orun compositions lock` — it records resolved digests automatically.

## Using a local Stack

Point at a local directory or archive:

```yaml
# Local directory (hashed and cached)
compositions:
  sources:
    - name: platform
      kind: dir
      path: ./my-platform

# Local archive
compositions:
  sources:
    - name: platform
      kind: archive
      path: ./dist/my-platform-stack-1.0.0.tgz
```

## Multiple stacks with precedence

Declare multiple sources and control resolution with `precedence` or `bindings`:

```yaml
compositions:
  sources:
    - name: core
      kind: oci
      ref: oci://ghcr.io/my-org/core-stack:v2.0.0
    - name: team-overrides
      kind: dir
      path: ./overrides
  resolution:
    precedence:
      - team-overrides  # wins when both export the same type
      - core
    bindings:
      terraform: core   # always use terraform from core
```

## Example: orun platform stack

The repository ships a complete example stack at `examples/compositions/`. It exports eleven composition types covering Terraform, Helm, Cloudflare Workers, Cloudflare Pages, and Turbo monorepo patterns.

The `stack.yaml` uses auto-discovery (no `spec.compositions` list) and includes the OCI registry target:

```yaml
apiVersion: orun.io/v1
kind: Stack
metadata:
  name: sumo-ops-orun-platform-stack
  version: 0.9.2
  description: Packaged compositions for the Sumo Ops Platform Orun repository.
  owner: sourceplane
registry:
  host: ghcr.io
  namespace: sourceplane
  repository: sumo-ops-platform-orun-stack
  visibility: public
```

Running `orun pack --root examples/compositions` discovers and archives all eleven composition types automatically.

## Intent Presets

Stacks can publish reusable intent scaffolding alongside compositions. This lets platform teams ship "golden repo baselines" — standard environments, triggers, defaults, and policies — that consuming repos opt into via `extends:`.

### Declaring Presets in stack.yaml

```yaml
apiVersion: orun.io/v1
kind: Stack
metadata:
  name: aws-platform-stack
  version: 1.0.0
spec:
  compositions:
    - path: compositions/terraform/compositions.yaml
  intentPresets:
    - name: standard
      path: presets/standard.yaml
    - name: github-actions
      path: presets/github-actions.yaml
```

Preset files use `kind: IntentPreset` and live anywhere within the Stack directory. They are included in the OCI artifact when the Stack is published.

### Directory Structure

```text
my-platform/
├── stack.yaml
├── compositions/
│   └── ...
└── presets/
    ├── standard.yaml       ← IntentPreset
    └── github-actions.yaml ← IntentPreset
```

See [Intent Presets](./intent-presets.md) for the full preset specification and merge rules.

