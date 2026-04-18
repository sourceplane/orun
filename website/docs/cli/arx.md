---
title: arx CLI
---

The root `arx` command is the entry point for planning, inspection, and execution.

## Command map

| Command | Purpose |
| --- | --- |
| `arx plan` | Compile intent and compositions into a deterministic execution plan |
| `arx run` | Dry-run or execute a compiled plan |
| `arx validate` | Validate intent and discovered components against schemas |
| `arx debug` | Inspect intent processing and planning internals |
| `arx compositions` | List or inspect available compositions |
| `arx component` | List components or inspect a merged component view |
| `arx completion` | Generate shell completion scripts |

## Global flags

| Flag | Meaning |
| --- | --- |
| `--config-dir`, `-c` | Path or glob used to load composition assets |
| `--version` | Print the CLI version |
| `--help` | Show command help |

`--config-dir` can also be set through `ARX_CONFIG_DIR`. The deprecated `CIZ_CONFIG_DIR` and `LITECI_CONFIG_DIR` aliases are still accepted.

## Typical flow

```bash
arx validate --intent intent.yaml --config-dir assets/config/compositions
arx plan --intent intent.yaml --config-dir assets/config/compositions --output plan.json
arx run --plan plan.json
```

Read the command-specific pages next if you need examples and flag details.