---
title: arx plan
---

`arx plan` compiles intent, component discovery, and compositions into an immutable execution DAG.

## Usage

```bash
arx plan \
  --intent intent.yaml \
  --config-dir assets/config/compositions \
  --output plan.json
```

## Common examples

Generate a JSON plan:

```bash
arx plan -i examples/intent.yaml -c assets/config/compositions -o plan.json
```

Generate YAML instead:

```bash
arx plan -i examples/intent.yaml -c assets/config/compositions -o plan.yaml -f yaml
```

Filter to one environment:

```bash
arx plan -i examples/intent.yaml -c assets/config/compositions --env staging
```

Preview the dependency graph while compiling:

```bash
arx plan -i examples/intent.yaml -c assets/config/compositions --view dag
```

Focus on changed components:

```bash
arx plan -i examples/intent.yaml -c assets/config/compositions --changed --base main
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path |
| `--output`, `-o` | Output plan path |
| `--format`, `-f` | Output format: `json` or `yaml` |
| `--debug` | Enable debug logging during planning |
| `--env`, `-e` | Restrict compilation to one environment |
| `--view`, `-v` | Render a view such as `dag`, `dependencies`, or `component=<name>` |
| `--changed` | Enable change-aware filtering |
| `--base` | Base git ref for change detection |
| `--head` | Head git ref for change detection |
| `--files` | Explicit changed-file list |
| `--uncommitted` | Scope to uncommitted changes |
| `--untracked` | Scope to untracked files |

## Output contract

The generated plan contains explicit jobs, dependency edges, step phases, labels, and runtime metadata. Read [plan schema](../reference/plan-schema.md) for the full structure.