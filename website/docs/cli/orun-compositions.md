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

Build and publish a composition package:

```bash
orun compositions package build --root examples/compositions --output /tmp/example-platform-compositions.tgz
orun compositions package push /tmp/example-platform-compositions.tgz oci://ghcr.io/sourceplane/orun-compositions/example-platform
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--expand-jobs`, `-e` | Expand job details in the output |
| `--long`, `-l` | Detailed listing mode on `orun compositions list` |
| `--intent`, `-i` | Intent file path used to resolve declared composition sources |

Use this command to confirm which types are available before validating or planning against them. `--config-dir` remains available as a global legacy fallback for folder-shaped compositions.
