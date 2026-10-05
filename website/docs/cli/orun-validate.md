---
title: orun validate
---

`orun validate` is the verify step: it checks the intent file, the component manifests it discovers, the reserved `ORUN_` environment prefix, and the profile and dependency rules, without loading compositions or generating a plan. Checking each component's parameters against its composition's schema happens in [`orun plan`](./orun-plan.md); run both in CI. See [standards](../concepts/standards.md) for what each command enforces.

:::note Always global
`validate` always operates on the full intent regardless of your current directory. CWD-based component scoping does not apply — you need to know the whole graph is valid, not just your component. The `--all` flag has no effect on this command.
:::

## Usage

```bash
orun validate
```

When `--intent` is not specified, `orun` auto-discovers `intent.yaml` by walking up the directory tree.

## When to use it

- pre-commit validation
- fast CI checks before full plan rendering
- debugging schema failures independently from execution planning

## Examples

Validate the repository example:

```bash
orun validate -i examples/intent.yaml
```

Enable debug output while validating:

```bash
orun validate -i examples/intent.yaml --debug
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path (auto-discovered if not set) |
| `--debug` | Enable debug logging |

Use `validate` first when you want a fast failure signal before compiling or executing a plan. `--config-dir` remains available as a global legacy fallback for folder-shaped compositions.