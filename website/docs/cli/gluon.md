---
title: gluon CLI
---

The root `gluon` command is the entry point for planning, inspection, and execution.

## Command map

| Command | Purpose |
| --- | --- |
| `gluon plan` | Compile intent and compositions into a deterministic execution plan |
| `gluon run` | Dry-run or execute a compiled plan |
| `gluon validate` | Validate intent and discovered components against schemas |
| `gluon debug` | Inspect intent processing and planning internals |
| `gluon compositions` | List or inspect available compositions |
| `gluon component` | List components or inspect a merged component view |
| `gluon completion` | Generate shell completion scripts |

## Global flags

| Flag | Meaning |
| --- | --- |
| `--config-dir`, `-c` | Legacy fallback path or glob for folder-shaped compositions |
| `--version` | Print the CLI version |
| `--help` | Show command help |

`--config-dir` can also be set through `GLUON_CONFIG_DIR`, but packaged composition sources declared in the intent are the recommended path.

## Typical flow

```bash
gluon compositions lock --intent intent.yaml
gluon validate --intent intent.yaml
gluon plan --intent intent.yaml --output plan.json
gluon run --plan plan.json
```

Read the command-specific pages next if you need examples and flag details.