---
title: arx component
---

`arx component` lists discovered components or prints the merged view for a single component.

## Usage

List components:

```bash
arx component \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions
```

Inspect a single component:

```bash
arx component web-app \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --long
```

The alias `components` is also supported.

## Change-aware examples

```bash
arx component \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --changed \
  --base main
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path |
| `--long`, `-l` | Show detailed component data |
| `--changed` | Enable change-aware filtering |
| `--base` | Base git ref for change detection |
| `--head` | Head git ref for change detection |
| `--files` | Explicit changed-file list |
| `--uncommitted` | Scope to uncommitted changes |
| `--untracked` | Scope to untracked files |
| `--config-dir`, `-c` | Global flag used when composition-aware output is needed |

Use `component` before `plan` when you want to understand how inputs, labels, and overrides were merged.