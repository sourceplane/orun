---
title: orun fetch
---

`orun fetch` downloads a composition package from an OCI registry to a local directory. Use it to inspect remote stacks locally or to vendor composition sources into your repository.

## Usage

```bash
orun fetch <oci-ref> [--output <dir>] [--overwrite]
```

## What it does

1. Pulls the composition layer from the OCI artifact at `<oci-ref>`
2. Extracts the `compositions/` tree and `stack.yaml` into the output directory
3. Skips the examples layer (if present in the artifact)

The output directory defaults to the repository name inferred from the OCI reference.

## Examples

Fetch a stack to a directory named after the repository:

```bash
orun fetch ghcr.io/sourceplane/stack-tectonic:1.0.0
# Creates ./stack-tectonic/ with stack.yaml and compositions/
```

Fetch to a custom directory:

```bash
orun fetch ghcr.io/sourceplane/stack-tectonic:1.0.0 --output ./my-compositions
```

Overwrite an existing directory:

```bash
orun fetch ghcr.io/sourceplane/stack-tectonic:1.0.0 --output ./my-compositions --overwrite
```

## Flags

| Flag | Short | Meaning |
| --- | --- | --- |
| `--output` | `-o` | Destination directory (defaults to inferred package name) |
| `--overwrite` | | Replace existing directory if it exists |

## Behavior notes

- The destination directory must not exist unless `--overwrite` is passed.
- Only the `compositions/` tree and `stack.yaml` are extracted — not the full OCI package.
- Authentication uses the same credentials as `orun login` / Docker config.

## When to use fetch

- **Inspect a remote stack** before referencing it in your intent
- **Vendor compositions locally** for air-gapped or offline development
- **Compare versions** by fetching different tags side by side

For normal usage, reference OCI stacks directly in `intent.yaml` via composition sources — `orun plan` resolves them automatically:

```yaml
compositions:
  sources:
    - name: platform
      kind: oci
      ref: oci://ghcr.io/sourceplane/stack-tectonic:1.0.0
```
