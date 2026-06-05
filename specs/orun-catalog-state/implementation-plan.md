# Implementation Plan

> Milestone-based. Each states **goal**, **deps**, **suggested PR scope**, **done
> when**, **design refs**. Agents may split/merge while keeping each PR reviewable.
> The lossless-catalog prerequisite (CS1) and the read view (CS2) freeze early;
> the change-detection substrate (CS3) and engine (CS4) are the core; the
> migration (CS5) is **parity-gated**; the cockpit (CS6) delivers Pillar A + Q2;
> CS8 is the gate.

```
CS1 Path fix ─► CS2 objcatalog ─► CS3 ownership + fingerprints ─► CS4 affected engine
                                              │                        │
                                              │            CS5 migrate plan/run --changed (parity)
                                              │            CS6 cockpit: gate + hook + live view + changed view (Q2)
                                              │            CS7 orun catalog affected
                                              └────────────────────────────► CS8 parity + determinism gate
                                                                              CS9 catalog refresh repoint (foldable)
```

---

## CS1 — Lossless object-model catalog (`Path`)
**Goal:** the graph catalog carries every field `ComponentSummary` + the engine need.
- Add `path` to `nodes.ComponentIdentity`; map `cm.Identity.Path` in
  `objplan/catalog.go:mapManifest`; regenerate golden ids; document the one-time
  catalog-id change.

**Deps:** none. **PR scope:** 1 PR. **Done when:** a graph `ComponentManifest`
round-trips `path`; object-model tests green. **Design:** `design.md` §4,
`data-model.md` §4.

## CS2 — `internal/objcatalog` read view
**Goal:** read the catalog (incl. `impact/`) from the graph.
- `Reader` mirroring `objread`; `Load → CatalogView` (catalog + components + graph
  + tolerant `impact/`); local tree-filename consts.

**Deps:** CS1. **PR scope:** 1–2 PRs. **Done when:** loads a seeded catalog;
missing `impact/` → nil (no error); ≥90% coverage. **Design:** `design.md` §3.2,
`data-model.md` §3.

## CS3 — Change-detection substrate: ownership map + fingerprints
**Goal:** every full resolve emits `impact/ownership.json` + `impact/fingerprints/`.
- Derive the ownership map (path→component + classification rules) from discovery
  + intent blocks; derive per-component fingerprints (git-projected leaf set;
  candidate file set). Add `nodes` types + validation; write both into the catalog
  tree (always present). Wire into the `objplan` catalog write.

**Deps:** CS1. **PR scope:** 2 PRs (ownership; fingerprints). **Done when:** `orun
plan` writes deterministic `ownership.json` + `fingerprints/`; two resolves of one
source → byte-identical; ≥90% coverage. **Design:** `change-detection.md` §3,
`data-model.md` §2/§2b.

## CS4 — `internal/affected` — the unified engine
**Goal:** one definition of "changed/affected," surface-agnostic.
- `Detector` + `Result`; `ChangeSource` with `GitChangeSource` (wraps
  `git.ChangeDetector`) and `FingerprintChangeSource` (virtual-tree diff);
  pipeline: ownership → intent classification (`git.DiffIntent`) → intent-impact
  (`all/watch/none` + `Change.Watches`) → dependency closure over
  `graph/dependencies.json`. Move/wrap `git.DiffIntent`; consolidate the intent
  classification.

**Deps:** CS2, CS3. **PR scope:** 2–3 PRs (engine + git source; fingerprint
source; intent-impact). **Done when:** the engine reproduces the change-detection
fixtures (`test-plan.md` §3) from both sources; intent-impact semantics covered;
≥90% coverage. **Design:** `change-detection.md` §2/§5/§6.

## CS5 — Migrate `plan --changed` / `run --changed` onto the engine
**Goal:** replace the ad-hoc `main.go` path; identical selection.
- Build `GitChangeSource` from the existing `ChangeOptions`; call `affected.Detect`;
  select jobs from `Result`; render `--explain` from `Result.Explain`. Delete
  `collectChangedComponents`/`isPathChanged`; re-base the changed-path use of
  `expand.DependencyResolver` onto the catalog graph.
- **Gated by the parity test (CS8): the old path is removed only after parity.**

**Deps:** CS4. **PR scope:** 2 PRs (wire engine alongside old path + parity test;
remove old path). **Done when:** `plan`/`run --changed` job selection is identical
to pre-migration on the fixture corpus (parity green); `--explain` output
preserved. **Design:** `change-detection.md` §4/§6, `cli-surface.md` §1b.

## CS6 — Cockpit: read seam + drill-down + changed view (Q2) + run action
**Goal:** Pillar A + the Q2 overlay + the catalog→component→job→logs drill-down,
all as a **consumer** of the read/action seams (`consumers.md`).
- `RefreshCatalog` seam; the freshness gate in `LoadWorkspace`; the universal
  command hook (best-effort, debounced, D-8); the cockpit live in-memory view +
  interval ticker (D-9).
- **View-models (read seam):** `CatalogView`/`ComponentView` in
  `internal/cockpit/viewmodel`; the cockpit renders these (not the store directly);
  reuse existing `RunView`/`LogsView`.
- **Drill-down (v1, reviewable):** catalog list → component page (details + Jobs
  section) → job view → logs. The Jobs section uses the component→executions join
  (Q-6 — scan+filter for v1).
- **Q2 overlay:** `affected.Detect` with a `FingerprintChangeSource` on the ticker;
  badge `DirectlyChanged`/`Dependents`; show-only-changed filter.
- **Env selector + env-scoped run (TUI-only, existing model):** a selected-env
  context (key `e`; chosen from existing `intent.environments`; default =
  first/last-used via prefs — **no schema change**); run from the component/job
  level via the existing `objrun` path, **component-scoped for the one selected
  env** (`environments.md`), only for components active in it. The single-env
  *redesign* (`defaultEnvironment`, removing the all-env path) is a separate epic
  (`specs/orun-env-scoping/`, L-1) — **not here**. The action seam stays separate
  from the read seam.

**Deps:** CS2, CS3, CS4. **PR scope:** 4–5 PRs (seam; gate+hook; view-models +
drill-down; changed overlay; run action). **Done when:** the cockpit renders the
catalog from the graph via view-models; drill-down catalog→component→job→logs
works from the state store; the fast path takes no resolve; an external write
appears within one `refreshInterval`; the overlay tracks edits; a run from the
component page seals via `objrun` **scoped to the selected env** (single env);
render never fails on a write error. **Design:** `consumers.md`, `environments.md`,
`design.md` §3, `cli-surface.md` §0/§1.

## CS7 — `orun catalog affected`
**Goal:** the engine on the CLI (and the explicit affected query).
- Wire `affected.Detect` to a command emitting `CatalogAffectedResult`
  (`affected` + the three sets + `confidence`/`needsFullResolve`).

**Deps:** CS4. **PR scope:** 1 PR. **Done when:** the change-detection fixtures pass
end-to-end via the CLI; over-report / structural / global / watch cases correct.
**Design:** `cli-surface.md` §3, `change-detection.md` §5.

## CS8 — Parity + determinism gate
**Goal:** lock selection-parity, losslessness, and determinism.
- **Parity:** new engine == existing `--changed` selection (the CS5 gate);
  `[]ComponentSummary` graph-path == intent-path (the `Path` guard).
- Ownership + fingerprint determinism; the change-detection fixture corpus as the
  shared reference.

**Deps:** CS5, CS6, CS7. **PR scope:** 1 PR. **Done when:** parity green over a
multi-component fixture (incl. intent-impact watch, nested dirs, dependency edges);
determinism asserted. **Design:** `test-plan.md`.

## CS9 — `orun catalog refresh` repoint (foldable into CS6)
**Goal:** explicit refresh also populates the object-model catalog + `impact/`.
- After the `catalogstore` write, call `RefreshCatalog` with the same
  `CatalogView`; append `data.objectModel`; failures → `warnings[]`.

**Deps:** CS3. **PR scope:** small. **Done when:** one resolve writes both
materializations; existing `CatalogRefreshResult` golden keys unchanged.
**Design:** `cli-surface.md` §2.

---

## Cross-cutting (every milestone)
- One definition of affected — no surface re-implements it (invariant 5).
- The migration (CS5) **never** changes observable `--changed` selection before
  parity (CD-4); the old path is deleted only after.
- Canonical JSON; best-effort writes never fail a read; `errors.Is/As`.
- **No** structural accommodation for the under-review worker (`design.md` §7).
