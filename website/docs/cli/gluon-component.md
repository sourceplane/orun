---
title: gluon component
---

`gluon component` lists discovered components or prints the merged view for a single component.

## Usage

List components:

```bash
gluon component \
  --intent examples/intent.yaml
```

Inspect a single component:

```bash
gluon component web-app \
  --intent examples/intent.yaml \
  --long
```

The alias `components` is also supported.

## Change-aware examples

```bash
gluon component \
  --intent examples/intent.yaml \
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

Use `component` before `plan` when you want to understand how inputs, labels, and overrides were merged. `--config-dir` remains available as a global legacy fallback when the intent does not declare packaged sources.