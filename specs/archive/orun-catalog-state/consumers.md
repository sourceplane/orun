# Consumers — cockpit, web UI, and the read/action seam

> The cockpit TUI is **not special**: it is one *consumer* of (state store +
> engine). Its drill-down mirrors the object graph, and a future SaaS web UI is
> the **same read seam with no action seam** (read-only). This doc fixes the
> consumer model so every surface renders from the state store the same way. The
> drill-down *UX* is a reviewable v1 placeholder; what is fixed is "everything
> from the state store."

## 1. The model: consumers of (state store + engine)

A consumer binds to two seams:

- a **read seam** — presentation-neutral **view-models** derived from the state
  store (+ the `internal/affected` engine), over a **store-location-agnostic**
  `Source` (local `.orun` *or* a remote SaaS store); and
- an **action seam** — operations that produce events/state (run a job).

| Consumer | Read seam | Action seam |
|----------|-----------|-------------|
| **Cockpit TUI** (this spec) | ✅ | ✅ (run) |
| **SaaS web UI** (later; no work now) | ✅ (same view-models, possibly a *remote* `Source`) | ❌ read-only |

This pattern **already exists** for executions: `internal/cockpit/viewmodel`
(`RunView`/`RunListView`/`LogsView`) + `internal/cockpit/bridge.Source`
(`LoadRun`/`ListRuns`, local-or-remote) + `internal/cockpit/watch`. This spec
**extends the same pattern** to the catalog/component dimension — it does not
invent a new one.

## 2. The read seam — view-models

Presentation-neutral, in `internal/cockpit/viewmodel`. The cockpit and any web UI
render these; neither touches the object store directly.

| View-model | Source (state store) | Drill-down level |
|------------|----------------------|------------------|
| `CatalogView` (component list + affected overlay) | `objcatalog.Load(catalogs/current)` via the freshness gate; affected from `internal/affected` | **Catalog** |
| `ComponentView` (component detail + a Jobs section) | `objcatalog` manifest + the component's executions (the §5 join) | **Component page** |
| `RunView` / job view *(exists)* | `objread` `ExecutionView`/`JobView` | **Job** |
| `LogsView` *(exists)* | `objread.StepLog` / live working-tree tail | **Logs** |

Pillar A builds `CatalogView`/`ComponentView`; the existing run read path builds
`RunView`/`LogsView`. The web UI, pointed at a remote state store, reuses the
identical view-models over a remote `Source` (exactly as `bridge.FromBackend`
already does for runs).

## 3. Navigation = object-lineage drill-down

The cockpit's drill-down mirrors the object graph ("similar to the state-store
drill down"). v1 — **reviewable, not final**:

```
Catalog              (CatalogSnapshot)          → component list, affected-badged
  └─ Component page   (ComponentManifest)        → details + a Jobs section
       └─ Job view    (ExecutionRun / JobRun)    → job/attempt/step status   [▶ run from here]
            └─ Logs    (StepAttempt log / live)   → step logs (sealed blob or live tail)
```

Each level is sourced from the state store; navigating **down** = following an
**edge** in the lineage. Only the drill-down *presentation* is a placeholder (the
UX will be reviewed separately); the invariant this spec fixes is that every
level reads from the state store and that the hierarchy mirrors the object graph.

## 4. The action seam (TUI-only)

The cockpit can **run** from the component/job level, reusing the **same path as
`orun run`** (the `internal/objrun` session glue from the M12 repoint — no second
runner). The run is **component-scoped for the cockpit's selected environment**
(G-2 = component-scoped; env scoping = `environments.md`): exactly one selected
env, on the **existing** env model. The single-env *redesign* is a separate epic
(`specs/archive/orun-env-scoping/`). The action seam:

- is **not** part of the read seam, so a read-only consumer (web UI) is simply
  **not given** it — capability-by-construction, not a runtime "is this UI
  allowed?" check;
- produces events/state the read seam then reflects: a run writes a live working
  tree → the views update on the next refresh tick (`watch` later, §6).

## 5. The component→executions join (a gap — G-1)

The component page's Jobs section needs "executions/jobs for **this** component."
The object model has **no such index today** — `internal/objindex` indexes
executions only. Two options:

- **(a) scan + filter** executions by component, reusing `objread.PlanSummary`'s
  plan→component extraction. Fine at cockpit scale; **no new index**.
- **(b) derive a component→execution index** in `objindex`, mirroring
  `catalogmodel.ComponentExecutionIndex` (which the `catalogstore` lineage already
  maintains via `WriteComponentExecutionIndex`).

**Recommendation:** (a) for v1; (b) as a follow-on if scale demands. **Note the
convergence tie:** `catalogstore` already keeps `ComponentExecutionIndex` /
`ComponentHistory`; *where component-execution history lives long-term* is part of
D-7 and should be decided before building (b) in the object model (else a third
index appears).

## 6. Events

Two classes, both already modeled:

- **Input events** (keystrokes: drill, run) — TUI-only; drive navigation + the
  action seam.
- **State-change events** (the store changed) — drive **refresh for all
  consumers**. v1 uses the D-9 interval ticker (poll); a future refinement is a
  store **watch/notify** — `internal/cockpit/watch.Run` already does this for run
  views, so the cockpit/web UI can react without polling once a catalog watch is
  added.

## 7. Read-only web UI (high-level only — no work now)

Out of scope to build; in scope to **not preclude**. The seam guarantees it: a
web UI receives the read `Source` + `objcatalog` + the view-models, but **not**
the runner/action seam. Same views, no actions, possibly a remote `Source`. No
web-specific code in this spec — only the discipline that the cockpit depends on
the read seam and the action seam *separately*, never reaching past the
view-models into the store.

## 8. Gaps / needs attention

| # | Gap | Options | Decision needed |
|---|-----|---------|-----------------|
| **G-1** | component→executions join (§5) | **RESOLVED → (a) scan + filter** for v1 (no new index); (b)/index deferred, tie to D-7 | — |
| **G-2** | "run a job" scoping (§4) | **RESOLVED → component-scoped, one selected env on the existing model** (`environments.md`); the single-env redesign is the `orun-env-scoping` epic | — |
| **G-3** | drill-down UX | v1 placeholder (catalog → component → jobs → logs); will be reviewed | none now — flagged as non-final |
| **G-4** | catalog state-change **watch** vs poll (§6) | poll (D-9) now; add a catalog `watch` later | none now — poll is the v1 |
