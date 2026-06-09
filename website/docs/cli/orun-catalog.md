---
title: orun catalog
---

`orun catalog` resolves, persists, and inspects the **component catalog** — the
resolved set of components (plus their dependency graphs and the impact index)
for a workspace at a given source snapshot. It is the content-addressed
foundation that powers change detection (`--changed`), the `orun catalog
affected` query, and the cockpit's live component view.

```bash
orun catalog            # summary of the catalog command group
orun catalog <sub> --help
```

## Subcommands

| Subcommand | Purpose |
| --- | --- |
| `refresh` | Resolve the current workspace and persist a catalog snapshot |
| `affected` | Compute the components affected by a change (the change-detection engine) |
| `list` | List the components in the selected catalog |
| `describe` | Show the full resolved manifest for one component |
| `tree` | Render the catalog relationship graphs |
| `history` | Enumerate a component's execution history |
| `diff` | Compare two catalog snapshots |
| `validate` | Re-resolve in strict mode and report validation issues |
| `refs` | List every catalog ref with its resolved source/catalog keys |

A catalog is also written transparently as a side effect of using orun: `orun
plan` and a universal pre-run refresh hook keep `catalogs/current` fresh, so the
read subcommands and the cockpit usually have an up-to-date catalog without an
explicit `refresh`.

## `orun catalog refresh`

Resolves the current workspace into a `(SourceSnapshot, CatalogSnapshot)` pair
and persists it, including the `impact/` index (the ownership map and per-component
fingerprints) used by change detection. A byte-identical re-refresh is idempotent
— it reuses the existing snapshot and creates no new directory; a dirty worktree
is marked local-only.

```bash
orun catalog refresh
orun catalog refresh --json
```

| Flag | Purpose |
| --- | --- |
| `--json` | Stable machine-readable output |
| `--catalog-strict` / `--strict` | Promote validation warnings to errors |
| `--no-infer` | Disable the inference layer |
| `--catalog-source` / `--source` | Resolve by ref selector (`current\|main\|latest\|branches/<name>\|prs/<n>\|cat-<key>`) |
| `--catalog-snapshot` | Bypass refs; pin to an explicit `catalogSnapshotKey` |

Exit codes: `0` created or reused · `1` validation error (or any warning under
`--strict`) · `2` resolver internal error · `3` object-store ref conflict.

## `orun catalog affected`

Reads the object-model catalog (its ownership map and dependency graph),
classifies the change between `--base` and `--head` (or an explicit `--files`
list), and reports the affected component set with a confidence signal. **This is
the same change-detection engine that `orun plan/run --changed` and the cockpit
use** — `affected` is the way to inspect its output directly.

```bash
orun catalog affected
orun catalog affected --base main --head HEAD
orun catalog affected --files apps/api/main.go --json
```

The result has four sets:

| Field | Meaning |
| --- | --- |
| `directlyChanged` | components whose own inputs changed |
| `dependents` | components that transitively depend on a changed one |
| `affected` | the cockpit "blast radius" (`directlyChanged` + `dependents`) |
| `selection` | the plan/run job set (`directlyChanged` + `include:always` deps) |

On classification ambiguity the engine **over-reports, never under** — a false
positive is safe, a missed change is not. A `component.yaml` edit is treated as
structural: it lowers `confidence` and sets `needsFullResolve`.

| Flag | Purpose |
| --- | --- |
| `--base <ref>` | Base ref for change detection (default: `main`) |
| `--head <ref>` | Head ref for change detection (default: working tree) |
| `--files <path,...>` | Comma-separated changed files (bypasses git diff) |
| `--intent-impact <mode>` | How global intent changes affect components (`watch`/`all`/`none`, default `watch`) |
| `--json` | Emit the `CatalogAffectedResult` JSON envelope |

The `--json` envelope (`directlyChanged`, `dependents`, `affected`, `selection`,
`confidence`, `needsFullResolve`, `intentMode`, `catalogId`) is the
provenance-rich projection of the engine's result — use it in CI to explain why a
component was (or wasn't) selected.

`orun catalog affected` exits `6` when no catalog or impact index is present
(run `orun catalog refresh` or `orun plan` first).

## Read subcommands

```bash
orun catalog list                 # components in the current catalog
orun catalog describe <name>      # the full resolved manifest for one component
orun catalog tree                 # the catalog relationship graphs
orun catalog history <name>       # a component's execution history
orun catalog diff <a> <b>         # compare two catalog snapshots
orun catalog refs                 # every catalog ref + resolved source/catalog keys
```

These read whatever has been persisted (most recently `catalogs/current`); they
never resolve unless you ask for `refresh`.

## See also

- [Change detection](../concepts/change-detection.md) — `--changed` on
  `plan`/`run`/`component`, powered by this engine.
- [Change watches](../concepts/change-watches.md) — `spec.change.watches`, read
  by the engine for intent-impact scoping.
- [Cockpit overview](../cockpit/overview.md) — the live component view and
  changed/affected overlay over the same catalog.
