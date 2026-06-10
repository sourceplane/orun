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
  [`specs/orun-legacy-retirement/`](../orun-legacy-retirement/) Bucket 1. The
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
  [`specs/orun-legacy-retirement/`](../orun-legacy-retirement/) Bucket 1; the
  resolution model lives on in `internal/catalogresolve`, `internal/catalogmodel`,
  and `internal/objcatalog`.

## The state-model arc (for context)

```
Phase 1  orun-state-redesign      (archived)  ─┐
Phase 2  orun-component-catalog    (archived)  ─┼─►  Phase 3  orun-object-model   (active)
                                                │         the single, content-addressed
                                                │         persistence stack
         orun-catalog-state        (active)  ───┘    change-detection engine + orun catalog
         orun-legacy-retirement    (active)          deletes the Phase 1/2 stores
```

The current authoritative references are `specs/orun-object-model/` and
`specs/orun-catalog-state/`.
