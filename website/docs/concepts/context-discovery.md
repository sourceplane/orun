---
title: Context-aware discovery
---

`gluon` automatically discovers the intent file and detects which component you are working in based on your current directory. This means you can run `gluon` commands from anywhere in the repository without passing `--intent` or `--component`.

## Intent discovery

When `--intent` is not explicitly passed, `gluon` walks up the directory tree from your current working directory looking for `intent.yaml` (or `intent.yml`). The search stops at the git repository root.

```
my-repo/
├── intent.yaml            ← found by walking up
├── services/
│   └── api/
│       ├── component.yaml
│       └── src/            ← you are here
└── infra/
    └── network/
        └── component.yaml
```

Running any `gluon` command from `services/api/src/` finds `intent.yaml` at the repo root automatically.

The `.gluon/` state directory (plans, executions, logs) is always created at the intent root — never at your current working directory.

## Component context detection

`gluon` detects which component you are working in by walking **upward** from your current directory looking for a `component.yaml` file on disk. The first `component.yaml` found is used; its `metadata.name` field gives the component name.

In the example above, running from `services/api/src/` finds `services/api/component.yaml` and detects component `api`.

If no `component.yaml` is found between CWD and the repo root, no component context is active and all commands behave as if run from the repo root.

## Automatic scoping

Component context detection affects **runtime** behavior only — it never changes what is compiled into the plan.

| Command | Effect when inside a component directory |
| --- | --- |
| `gluon plan` | No change — always generates a global plan for all components |
| `gluon run` | Equivalent to passing `--component=<detected>` |
| `gluon get jobs` | Filters displayed jobs to the detected component + its dependencies |

When `gluon run` or `gluon get jobs` detects a component context, it resolves the **transitive dependency set** (the detected component plus all components it depends on) and filters to that set.

### Context banner

When auto-scoping is active, `gluon` prints a context banner to stderr before other output:

```
context: auto-scoped to component api (+ 2 dependencies: common-services, shared-config)
hint: pass --all to include all components
```

The banner is suppressed when output is `--json` or `-o json`.

## Overrides

| Override | Behavior |
| --- | --- |
| `--all` | Disables CWD-based scoping for `run` and `get jobs`; shows/runs all components |
| `--component <name>` | Explicit component filter always wins over auto-detection |
| `--intent <path>` | Skips intent discovery; uses the specified path |

## End-to-end example

```bash
# From repo root — full plan, all components
gluon plan
# ✓ Plan generated with 38 jobs

# cd into a component
cd services/api/

# Plan is still global — always includes all components
gluon plan
# ✓ Plan generated with 38 jobs

# run auto-filters to api and its dependencies
gluon run
# context: auto-scoped to component api (+ 1 dependency: common-services)
# hint: pass --all to include all components
# ✓ Run complete

# get jobs auto-filters to scoped components
gluon get jobs
# context: auto-scoped to component api (+ 1 dependency: common-services)
# PLAN: my-project (a1b2c3d) · 4 jobs
# ...

# Override to see everything
gluon get jobs --all
# PLAN: my-project (a1b2c3d) · 38 jobs

# Run the full plan regardless of CWD
gluon run --all
# ✓ Run complete
```
