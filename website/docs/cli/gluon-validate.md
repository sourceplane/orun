---
title: gluon validate
---

`gluon validate` checks intent, discovered component manifests, and type-specific schema constraints without generating a plan.

## Usage

```bash
gluon validate \
  --intent intent.yaml
```

## When to use it

- pre-commit validation
- fast CI checks before full plan rendering
- debugging schema failures independently from execution planning

## Examples

Validate the repository example:

```bash
gluon validate -i examples/intent.yaml
```

Enable debug output while validating:

```bash
gluon validate -i examples/intent.yaml --debug
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path |
| `--debug` | Enable debug logging |

Use `validate` first when you want a fast failure signal before compiling or executing a plan. `--config-dir` remains available as a global legacy fallback for folder-shaped compositions.