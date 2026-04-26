---
title: Context-aware discovery
---

`gluon` can automatically discover the intent file and detect which component you are working in based on your current directory. This means you can run `gluon plan` or `gluon run` from inside any component subdirectory without passing `--intent` or `--component`.

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

Running `gluon plan` from `services/api/src/` finds `intent.yaml` at the repo root automatically.

If no intent file is found between the current directory and the git root, the command falls back to looking for `intent.yaml` in the current directory (the original behavior).

## Component context detection

Once the intent is discovered, `gluon` determines which component you are working in by comparing your current directory against the resolved component paths. It picks the component whose path is the longest prefix of your relative working directory.

In the example above, running from `services/api/src/` matches the `api` component (path: `services/api`).

Components with path `"./"` (the repo root) are skipped during context detection because they are too broad to be meaningful.

## Automatic scoping

When a component is detected, `gluon plan`, `gluon run`, and `gluon get jobs` automatically scope to:

1. The detected component
2. All of its **transitive dependencies**

Dependents (components that depend on yours) are **not** included by default. When you are working on `api`, you want to make `api` work — downstream components like `web` will pick it up in their own context.

The dependency resolution uses the same `DependencyResolver.ResolveComponentSet` already used by `--changed`.

### Context banner

When auto-scoped, `gluon` prints a context banner to stderr before other output:

```
context: auto-scoped to component api (+ 2 dependencies: common-services, shared-config)
hint: pass --all to include all components
```

The banner is suppressed when output is `--json` or `-o json`.

## Overrides

| Override | Behavior |
| --- | --- |
| `--all` | Disables all CWD-based scoping; processes every component |
| `--component <name>` | Explicit component filter always wins over auto-detection |
| `--intent <path>` | Skips intent discovery; uses the specified path |

These flags work on all commands that support them.

## Which commands are scoped

| Command | Scoped? | Notes |
| --- | --- | --- |
| `gluon plan` | Yes | Generates a scoped plan with scope metadata |
| `gluon run` | Yes | Filters jobs to scoped components; warns on scope mismatch |
| `gluon get jobs` | Yes | Filters displayed jobs to scoped components |
| `gluon validate` | **No** | Always validates the full intent — scoping validation would create false confidence |
| `gluon get components` | No | Lists all components from intent |
| `gluon get environments` | No | Lists all environments from intent |

## Scope metadata in plans

When a plan is generated with auto-scoping, the scope is recorded in the plan metadata:

```json
{
  "metadata": {
    "name": "my-project",
    "checksum": "sha256-...",
    "scope": {
      "detectedComponent": "api",
      "components": ["api", "common-services"]
    }
  }
}
```

When `gluon run` detects a different scope than what the plan was generated for, it prints a warning:

```
warning: plan was generated for [api, common-services] but current scope is [web, common-services]
```

## Multiple components at the same path

If two components share the exact same path, `gluon` picks the one with the longest matching prefix. If they are identical, the first alphabetically is chosen. In practice, well-structured repos rarely have overlapping component paths.

## End-to-end example

```bash
# From repo root — full plan, all components
gluon plan
# ✓ Plan generated with 38 jobs

# cd into a component
cd services/api/

# Auto-scoped plan
gluon plan
# context: auto-scoped to component api (+ 1 dependency: common-services)
# hint: pass --all to include all components
# ✓ Plan generated with 4 jobs

# See only scoped jobs
gluon get jobs
# context: auto-scoped to component api (+ 1 dependency: common-services)
# PLAN: my-project (a1b2c3d) · 4 jobs
# ...

# Override to see everything
gluon get jobs --all
# PLAN: my-project (a1b2c3d) · 38 jobs

# Run the scoped plan
gluon run
# context: auto-scoped to component api (+ 1 dependency: common-services)
# ✓ Run complete
```
