---
title: orun validate
description: The verify step - check the intent, its discovered component manifests, the reserved ORUN_ prefix, every profile and dependency rule, and every policy, without compiling a plan.
---

`orun validate` is the verify step: it checks the intent file, the component manifests it discovers, the reserved `ORUN_` environment prefix, the profile and dependency rules, and the intent's [policies](../concepts/intent-model.md#policies), without generating a plan. It resolves the composition sources to merge presets and to check execution-profile policies; if they cannot be resolved it warns and still checks the intent-level policies. Checking each component's parameters against its composition's schema happens in [`orun plan`](./orun-plan.md); run both in CI. See [standards](../concepts/standards.md) for what each command enforces.

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
- catching a policy violation (a component overriding a pinned parameter, a lane on the wrong profile) before review
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