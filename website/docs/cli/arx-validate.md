---
title: arx validate
---

`arx validate` checks intent, discovered component manifests, and type-specific schema constraints without generating a plan.

## Usage

```bash
arx validate \
  --intent intent.yaml \
  --config-dir assets/config/compositions
```

## When to use it

- pre-commit validation
- fast CI checks before full plan rendering
- debugging schema failures independently from execution planning

## Examples

Validate the repository example:

```bash
arx validate -i examples/intent.yaml -c assets/config/compositions
```

Enable debug output while validating:

```bash
arx validate -i examples/intent.yaml -c assets/config/compositions --debug
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path |
| `--debug` | Enable debug logging |
| `--config-dir`, `-c` | Global flag used to load compositions |

Use `validate` first when you want a fast failure signal before compiling or executing a plan.