---
title: gluon compositions
---

`gluon compositions` lists or inspects the composition types resolved for an intent, or from a legacy `--config-dir` fallback.

## Usage

```bash
gluon compositions --intent examples/intent.yaml
```

The command also accepts a composition name directly:

```bash
gluon compositions helm --intent examples/intent.yaml
```

The alias `composition` is also supported.

## Subcommand form

For detailed output, use the explicit `list` subcommand:

```bash
gluon compositions list helm \
  --intent examples/intent.yaml \
  --long \
  --expand-jobs
```

Resolve declared sources into the local cache:

```bash
gluon compositions pull --intent examples/intent.yaml
gluon compositions lock --intent examples/intent.yaml
```

Build and publish a composition package:

```bash
gluon compositions package build --root examples/packages/platform-core --output /tmp/platform-core.tgz
gluon compositions package push /tmp/platform-core.tgz oci://ghcr.io/sourceplane/gluon-compositions/platform-core
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--expand-jobs`, `-e` | Expand job details in the output |
| `--long`, `-l` | Detailed listing mode on `gluon compositions list` |
| `--intent`, `-i` | Intent file path used to resolve declared composition sources |

Use this command to confirm which types are available before validating or planning against them. `--config-dir` remains available as a global legacy fallback for folder-shaped compositions.