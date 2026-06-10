# Archived specs

Epics that are **complete and superseded** — their design has shipped and been
absorbed into a later epic, so they are no longer the authoritative reference for
how orun works today. They are kept here as **frozen historical snapshots**: useful
for understanding how the current model was reached, but not maintained.

> Note: documents inside an archived epic may reference sibling specs by their
> original `specs/<epic>/` paths (pre-archive) and describe on-disk layouts or
> packages that have since been replaced. For current behaviour, follow the
> "Superseded by" pointer below.

## Contents

### `orun-state-redesign/` — Phase 1 of the local state model

Trigger-first, revision-first, remote-shaped (local-only) state. Introduced the
`TriggerOccurrence → PlanRevision → ExecutionRun` lineage and the `.orun/revisions/`
layout (refs + indexes) that replaced flat `.orun/plans/<sha>.json`.

- **Status:** Implemented — milestones **M0–M6 merged** to `main`.
- **Superseded by:** [`specs/orun-object-model/`](../orun-object-model/) (Phase 3),
  which collapsed the Phase 1 global `revisions/…` layout into the single
  content-addressed object graph under `.orun/objectmodel/`.
- **Code today:** the Phase 1 stores (`internal/statestore`, `internal/revision`,
  `internal/executionstate`) were deleted in
  [`specs/archive/orun-legacy-retirement/`](orun-legacy-retirement/) Bucket 1. The
  trigger-context model (`internal/triggerctx`) carries forward.

### `orun-component-catalog/` — Phase 2 of the local state model

The `SourceSnapshot`/`CatalogSnapshot` model wrapping the Phase 1 lineage: component
catalog resolution, identity/keys, and the catalog-parent mirror.

- **Status:** Implemented — built on Phase 1 (milestones C0–C9).
- **Superseded by:** [`specs/orun-object-model/`](../orun-object-model/) (Phase 3),
  which collapsed the Phase 2 catalog-parent **mirror** into the same object graph.
  The active change-detection / `orun catalog` work continued in
  [`specs/orun-catalog-state/`](../orun-catalog-state/).
- **Code today:** `internal/catalogstore` was deleted in
  [`specs/archive/orun-legacy-retirement/`](orun-legacy-retirement/) Bucket 1; the
  resolution model lives on in `internal/catalogresolve`, `internal/catalogmodel`,
  and `internal/objcatalog`.

### `orun-legacy-retirement/` — retiring the Phase 1/2 stores

The itemized, file-level plan that deleted the legacy catalog/revision store so
the content-addressed object model became the **single** persistence stack.

- **Status:** Complete — the core program shipped in **v2.15.0** (Buckets 1, 5,
  6; the lint gate enforces single-stack). Bucket 3 (env-scoping) shipped too.
- **Relocated:** the remaining optional perf items — Bucket 2 (`objindex`) +
  Bucket 5 packfile delta compression — moved to
  [`specs/orun-objectmodel-perf/`](../orun-objectmodel-perf/); Bucket 4 moved to
  its own epic [`specs/orun-affected-worker/`](../orun-affected-worker/).
- **Code today:** `internal/catalogstore`/`statestore`/`revision`/
  `executionstate`/`catalogsync` deleted; the object model
  ([`specs/orun-object-model/`](../orun-object-model/)) is the live reference.

### `orun-env-scoping/` — the "Z" selection model

`orun plan`/`run` environment selection: `--all-envs`, self-contained scoped plans
(dangling edges pruned with a warning), in-plan promotion ordering, and a
fail-closed mutating `run`. Converged from a breaking single-env epic to a small
additive feature.

- **Status:** Complete — ES1–ES5 shipped in **v2.15.0**.
- **Code today:** the selection model in `internal/model` (`PlanSelection`) +
  `internal/planner` (promotion), surfaced by `orun plan`/`run` and the
  env-promotion concept docs.

## The state-model arc (for context)

```
Phase 1  orun-state-redesign      (archived)  ─┐
Phase 2  orun-component-catalog    (archived)  ─┼─►  Phase 3  orun-object-model   (active)
                                                │         the single, content-addressed
                                                │         persistence stack
         orun-catalog-state        (active)  ───┘    change-detection engine + orun catalog
         orun-legacy-retirement    (archived)        deleted the Phase 1/2 stores (shipped v2.15.0)
```

The current authoritative references are `specs/orun-object-model/` and
`specs/orun-catalog-state/`.
