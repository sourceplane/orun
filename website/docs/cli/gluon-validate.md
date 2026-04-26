---
title: gluon validate
---

`gluon validate` checks intent, discovered component manifests, and type-specific schema constraints without generating a plan.

:::note Always global
`validate` always operates on the full intent regardless of your current directory. CWD-based component scoping does not apply — you need to know the whole graph is valid, not just your component. The `--all` flag has no effect on this command.
:::

## Usage

```bash
gluon validate
```

When `--intent` is not specified, `gluon` auto-discovers `intent.yaml` by walking up the directory tree.

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
| `--intent`, `-i` | Intent file path (auto-discovered if not set) |
| `--debug` | Enable debug logging |

Use `validate` first when you want a fast failure signal before compiling or executing a plan. `--config-dir` remains available as a global legacy fallback for folder-shaped compositions.