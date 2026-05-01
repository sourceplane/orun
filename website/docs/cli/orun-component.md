---
title: orun component
---

`orun component` lists discovered components or prints the merged view for a single component.

## Usage

List components:

```bash
orun component \
  --intent examples/intent.yaml
```

Inspect a single component:

```bash
orun component network-foundation \
  --intent examples/intent.yaml \
  --long
```

The alias `components` is also supported.

## Change-aware examples

```bash
orun component \
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
| `--explain` | Print how `--changed` resolved its base and head refs |

Use `component` before `plan` when you want to understand how inputs, labels, and overrides were merged. `--config-dir` remains available as a global legacy fallback when the intent does not declare packaged sources.
