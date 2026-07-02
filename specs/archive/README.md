# Archived specs

Epics that are **complete** — either *superseded* (their design shipped and was
absorbed into a later epic) or *implemented and stable* (their design shipped and
the **code** is now the reference). Either way they are no longer the
authoritative spec for how orun works today, and are kept here as **frozen
historical snapshots**: useful for understanding how the current model was
reached, but not maintained.

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
  The change-detection / `orun catalog` work landed in
  [`specs/archive/orun-catalog-state/`](orun-catalog-state/) (implemented).
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

### `orun-catalog-state/` — catalog-from-state + unified change detection

Serves the component catalog from the object graph and consolidates `--changed`
into one engine: ownership map + virtual-Merkle-tree fingerprints, the
`internal/affected` changed/affected engine, `orun catalog affected`, and the
cockpit's changed/affected view + drill-down.

- **Status:** ✅ Implemented — milestones **CS1–CS9 complete**.
- **Code today (the live reference):** `internal/affected` (the unified engine),
  `internal/objcatalog` (catalog read view), the cockpit read seam
  (`internal/cockpit/catalogread`), and the `plan`/`run --changed` +
  `orun catalog affected` paths. `specs/orun-service-catalog/` builds on this.

### `orun-work-v1/` — the first work-plane design

The original Linear-style work plane: Initiative/Epic/Task entities with an
event-sourced hot store (spec'd against Cloudflare D1 + per-project Durable
Objects), status automation writing to a stored status column, a `work_links`
side table pending SC2, and milestones W0–W6. Its cores (Go `internal/work`,
orun-cloud `@saas/db/work`) landed test-green but were never wired to any
product surface.

- **Status:** Superseded — scrapped before any UI shipped. The platform moved
  under the spec (Postgres backend, SC1–SC8 landed, workspace tenancy + Teams
  RBAC), and the stored-status ontology was replaced wholesale.
- **Superseded by:** [`specs/orun-work/`](../orun-work/) (v2) — the
  intent/fact/coordination split: lifecycle becomes a *derived query* over two
  append-only logs (coordination + observation), never a stored column; work
  kinds join the shipped service-catalog graph directly.
- **Code today:** the v1 cores (`internal/work`, `internal/workbridge`,
  orun-cloud `packages/db/src/work/`) were deleted when this spec was archived;
  the unused `work` schema is dropped by orun-cloud migration
  `470_work_teardown`. The v1 invariants that survive (append-only events,
  mandatory actor provenance, one write path, seal determinism) carry forward
  into v2.

## The state-model arc (for context)

```
Phase 1  orun-state-redesign      (archived)  ─┐
Phase 2  orun-component-catalog    (archived)  ─┼─►  Phase 3  orun-object-model   (active)
                                                │         the single, content-addressed
                                                │         persistence stack
         orun-catalog-state        (archived) ─┘    change-detection engine + orun catalog (implemented)
         orun-legacy-retirement    (archived)        deleted the Phase 1/2 stores (shipped v2.15.0)
```

The current authoritative spec reference is `specs/orun-object-model/` (the live
persistence model). For change detection / `orun catalog`, the **code** is the
reference (`internal/affected`, `internal/objcatalog`); the archived
`specs/archive/orun-catalog-state/` spec records the design.
