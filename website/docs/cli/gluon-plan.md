---
title: gluon plan
---

`gluon plan` compiles intent, component discovery, and compositions into an immutable execution DAG.

## Usage

```bash
gluon plan
```

When run without `--intent`, `gluon` automatically discovers `intent.yaml` by walking up the directory tree to the git root. When run from inside a component directory, the plan is automatically scoped to that component and its transitive dependencies.

The generated plan is saved to `.gluon/plans/{checksum}.json` and also written as `.gluon/plans/latest.json`. `gluon run` resolves `latest` automatically, so you rarely need to specify an output path.

## Common examples

Generate a plan (auto-discovers intent):

```bash
gluon plan
```

Generate a plan from a specific intent file:

```bash
gluon plan -i examples/intent.yaml
```

Generate with an explicit output path (for backwards compatibility):

```bash
gluon plan -i examples/intent.yaml -o /tmp/gluon-plan.json
```

Generate a named plan that can be referenced by name later:

```bash
gluon plan -i examples/intent.yaml --name release-candidate
```

Generate YAML output:

```bash
gluon plan -i examples/intent.yaml -o plan.yaml -f yaml
```

Filter to one environment:

```bash
gluon plan -i examples/intent.yaml --env staging
```

Filter to one component:

```bash
gluon plan -i examples/intent.yaml --component api-edge-worker
```

Preview the dependency graph while compiling:

```bash
gluon plan -i examples/intent.yaml --view dag
```

Focus on changed components:

```bash
gluon plan -i examples/intent.yaml --changed --base main
```

Generate a full plan when inside a component directory (override auto-scoping):

```bash
gluon plan --all
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path (auto-discovered if not set) |
| `--output`, `-o` | Explicit output path (optional; defaults to `.gluon/plans/`) |
| `--format`, `-f` | Output format: `json` or `yaml` |
| `--name` | Give the plan a memorable name for later reference via `--plan <name>` |
| `--debug` | Enable debug logging during planning |
| `--env`, `-e` | Restrict compilation to one environment |
| `--component` | Restrict compilation to one or more components (repeatable) |
| `--all` | Disable CWD-based component scoping; include all components |
| `--view`, `-v` | Render a view such as `dag`, `dependencies`, or `component=<name>` |
| `--changed` | Enable change-aware filtering |
| `--base` | Base git ref for change detection |
| `--head` | Head git ref for change detection |
| `--files` | Explicit changed-file list |
| `--uncommitted` | Scope to uncommitted changes |
| `--untracked` | Scope to untracked files |

## Output contract

The generated plan contains explicit jobs, dependency edges, step phases, labels, and runtime metadata. Read [plan schema](../reference/plan-schema.md) for the full structure.

When a plan is generated with CWD-based scoping, the scope is recorded in `metadata.scope` so downstream commands can detect scope mismatches.

Plans stored in `.gluon/plans/` can be inspected with `gluon get plans` and `gluon describe plan <id>`.

Use `--config-dir` only when you need to load legacy folder-shaped compositions instead of intent-declared packages.

See [context-aware discovery](../concepts/context-discovery.md) for full details on auto-scoping behavior.
