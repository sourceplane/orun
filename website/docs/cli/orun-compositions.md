---
title: orun compositions
---

`orun compositions` lists or inspects the composition types resolved for an intent, or from a legacy `--config-dir` fallback.

## Usage

```bash
orun compositions --intent examples/intent.yaml
```

The command also accepts a composition name directly:

```bash
orun compositions terraform --intent examples/intent.yaml
```

The alias `composition` is also supported.

## Subcommand form

For detailed output, use the explicit `list` subcommand:

```bash
orun compositions list terraform \
  --intent examples/intent.yaml \
  --long \
  --expand-jobs
```

Resolve declared sources into the local cache:

```bash
orun compositions pull --intent examples/intent.yaml
orun compositions lock --intent examples/intent.yaml
```

## Packaging and publishing stacks

Build a local archive from a Stack directory:

```bash
orun pack --root examples/compositions
orun pack --root examples/compositions --output dist/platform-stack.tgz
```

Stream publish a Stack directly to an OCI registry (no temp file):

```bash
orun login ghcr.io
orun publish --root examples/compositions
orun publish --root examples/compositions --ref ghcr.io/my-org/my-platform-stack:v1.0.0
orun publish --root examples/compositions --dry-run
```

The `stack.yaml` `registry` block is used to infer the target when `--ref` is omitted:

```yaml
registry:
  host: ghcr.io
  namespace: my-org
  repository: my-platform-stack
```

## Using a remote stack from a registry

Reference a published Stack directly in `intent.yaml`:

```yaml
compositions:
  sources:
    - name: platform
      kind: oci
      ref: oci://ghcr.io/my-org/my-platform-stack:v1.0.0
```

Lock the resolved digest for reproducible plans:

```bash
orun compositions lock --intent intent.yaml
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--expand-jobs`, `-e` | Expand job details in the output |
| `--long`, `-l` | Detailed listing mode on `orun compositions list` |
| `--intent`, `-i` | Intent file path used to resolve declared composition sources |

Use this command to confirm which types are available before validating or planning against them. `--config-dir` remains available as a global legacy fallback for folder-shaped compositions.
