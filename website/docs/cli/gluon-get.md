---
title: gluon get
---

`gluon get` lists resources in a kubectl-style interface.

## Usage

```bash
gluon get <resource>
```

Supported resources: `plans`, `runs`, `jobs`, `components`, `environments`.

## Subcommands

### `gluon get plans`

List stored plans:

```bash
gluon get plans
```

Output shows revision IDs, job counts, age, and status:

```
REVISION        JOBS    AGE       STATUS
latest          38      now       ● ready
a1b2c3d4ef12    38      now       ● ready
```

A summary line below the table shows the revision count and latest checksum.

### `gluon get jobs`

List jobs from the latest (or named) plan, grouped by component and environment:

```bash
gluon get jobs
```

When run from inside a component directory, the job list is automatically scoped to that component and its dependencies. Use `--all` to see all jobs.

Default view (tree):

```
PLAN: my-plan (a1b2c3d) · 38 jobs

api-edge-worker
  production
    ○ verify-deploy            pending
  staging
    ○ verify-deploy            pending

platform-shared
  production
    ✓ build                    completed
```

#### View modes

| Flag | View |
| --- | --- |
| `--view=tree` | Grouped component → env → job tree (default) |
| `--view=compact` | One line per job: icon, component, env, job name |
| `--view=table` or `--output wide` | Full job ID table |

#### Plan reference

```bash
gluon get jobs --plan release-candidate
gluon get jobs --plan a1b2c3
```

### `gluon get runs`

List execution records (alias for `gluon status --all`):

```bash
gluon get runs
```

### `gluon get components`

List components from the intent:

```bash
gluon get components
```

Default compact view:

```
15 components

  ✓ api-edge-worker     cloudflare-worker-turbo    prod,staging,dev
  ✓ platform-shared     turbo-package              prod,staging,dev
  – legacy-service      terraform                  prod
```

Use `--long` for the full detail view with inputs and instance paths.

### `gluon get environments`

List environments from the intent:

```bash
gluon get environments
```

Output is sorted alphabetically with associated metadata:

```
ENVIRONMENTS  4

dev            prefix=dev-
prod           prefix=prod-
staging        prefix=stg-
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--output`, `-o` | Output format: `json`, `yaml`, or `wide` |
| `--plan` | Plan reference for `get jobs` (name, checksum prefix, or `latest`) |
| `--view` | View mode for `get jobs`: `tree`, `compact`, or `table` |
| `--intent`, `-i` | Intent file for `get components` and `get environments` (auto-discovered if not set) |
| `--all` | Disable CWD-based scoping for `get jobs` |

All subcommands support `-o json` for machine-readable output.

See [context-aware discovery](../concepts/context-discovery.md) for details on automatic scoping.
