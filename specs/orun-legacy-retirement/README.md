# Spec: orun-legacy-retirement

**The closeout plan.** `internal/state` (the legacy ExecState file store) was
deleted at the object-model M12 cutover, but a second legacy stack survives: the
Phase‑1/2 **revision + catalog store** (`internal/catalogstore` →
`internal/statestore`, plus `internal/revision` + `internal/executionstate`). It
is still **dual-written on every `orun run`** and is still the **sole backing for
`orun catalog *`**. This spec is the itemized, file-level plan to retire it — the
"separate spec once the object-model catalog read surface is complete" that
`orun-catalog-state` **D‑7 / L‑3** promised — plus the register of remaining
epics and optional follow-ups needed to fully close the program.

> This spec **promotes** the deferred D‑7 / L‑3 entry to a ready work plan. It
> does **not** restate the object model; it consumes it. Read
> `specs/orun-object-model/` and `specs/orun-catalog-state/` first.

## Status

| Field | Value |
|-------|-------|
| Status | **Buckets 1, 5, 6 effectively COMPLETE.** Bucket 1: the legacy catalog/revision store is retired; the object model is the single persistence stack and the lint gate enforces it. Bucket 6: cockpit catalog auto-refresh shipped (engine + TUI wiring + stale badge). Bucket 5: resume step-log carry-forward and the `objectstore` fault-injection seam are done; packfile delta compression stays deferred (profiling-gated). Bucket 3 (`orun-env-scoping`) shipped in v2.15.0 (the converged "Z" model, ES1–ES5). Only Bucket 2 (optional `objindex` accelerator, perf-gated) and Bucket 4 (`orun-affected-worker`, a separate unapproved epic) remain gated. |
| Promotes | `orun-catalog-state` deferred register **D‑7 / L‑3** ("full `internal/catalogstore` retirement") |
| Builds on | `specs/orun-object-model/` (the canonical graph), `specs/orun-catalog-state/` (`internal/objcatalog`, `internal/affected`) |
| Target branch | `main` (PRs merged incrementally) |
| Constraint | **Do not regress** `orun catalog list/describe/tree/...` or `orun catalog history` during the move (S‑7). |

## The one-paragraph thesis

The unification is ~80% done: one store for **executions** (the object graph),
two stores for **catalog/revision history**. `orun run` writes the canonical
execution via `internal/objrun` **and** mirrors a second copy into the legacy
revision layout via `internal/executionstate`, purely to back `orun catalog
history` and the old read paths. Closing the legacy systems means moving the
last readers onto the object graph, deleting the mirror, and then deleting the
packages — in that dependency order. After that, the lint gate bans the imports
repo-wide and the object model is the *single* persistence stack.

## What is legacy (retire) vs shared (keep)

| Retire (delete at the end) | Keep (shared with the object model) |
|----------------------------|-------------------------------------|
| `internal/catalogstore` | `internal/catalogresolve` (the resolver `objplan` calls) |
| `internal/statestore` (the Phase‑1 KV primitive `objectstore` generalized) | `internal/catalogmodel` (shared manifest types used by `objplan`/`sourcectx`; prune store-only types) |
| `internal/revision` (incl. `legacy*.go`, `manifest.go`) | `internal/sourcectx` |
| `internal/executionstate` (the legacy-layout writer) | `internal/statebackend` (Backend interface + remote HTTP driver; `execmodel`-based, **not** legacy) |
| `internal/catalogdiff`, `internal/catalogsync` (catalog-CLI-only) | `internal/execmodel`, `internal/objectstore` + the `obj*` family |

Already done (reference points, no work): `internal/state` deleted;
`internal/statebackend/file.go` deleted; `orun catalog affected` and the runner
(`internal/objrun`) already on the object model.

---

# Bucket 1 — Retire the legacy catalog/revision store (D‑7 / L‑3)

> **✅ COMPLETE.** Landed incrementally in dependency order (read surface → CLI
> re-point → kill dual-write → delete packages → gate). Outcome:
> - **Reads (1A/1B):** every `orun catalog *` command (`tree`/`list`/`history`/
>   `refs`/`validate`/`describe`/`diff`) plus `describe revision|trigger`,
>   `get plans`, and `orun run <ref>` resolution read the object graph.
> - **Writes (1D):** `orun run` single-writes the object graph (the
>   executionstate/revision mirror is gone); `orun plan` and `orun catalog
>   refresh` write only the object-model catalog. `bridge_mirror_warn` removed.
> - **Producer fixes:** graph-edge `Optional` carried; the runner records an
>   `executionKey`; revision-key derivation relocated to `internal/revkey`.
> - **Deleted (1E):** `internal/executionstate`, `internal/revision`,
>   `internal/catalogsync`, `internal/catalogstore`, `internal/statestore`.
> - **Gate (1F):** `scripts/check-object-model.sh` bans all five legacy imports
>   repo-wide.
>
> Accepted v1 gaps (documented in code): `catalog history`/`describe` do not
> surface per-execution profile/environment/trigger-name or the component
> runtime-inference block; `orun plan --catalog-source`/`--catalog-snapshot` and
> `orun catalog refresh --sync` were removed.

Must land in dependency order: **read surface → CLI re-point → kill dual-write →
delete packages → gate.** Each `[ ]` is a reviewable PR-sized unit.

## 1A. Prerequisite — finish the object-model catalog read surface

`internal/objcatalog.Reader` today only `Load`s a single `CatalogView`. The
catalog CLI needs more before it can move off `catalogstore`.

- [x] **Two-snapshot catalog diff** in `internal/objcatalog` (replaces
  `internal/catalogdiff` + `catalogstore` reads behind `orun catalog diff`).
- [x] **Catalog event / validation views** for `catalog_run_events.go` and
  `catalog_validate.go` (currently `catalogstore.events` / strict re-resolve into
  the legacy store).
- [x] **Component → execution history join** for `orun catalog history` (today
  `catalogstore.ReadComponentExecutionIndex`). **Decision:** start with
  `objread` scan + filter (`objread.PlanSummary` over `RunSummary.Components`,
  the CS6 v1 choice); only build the `objindex` component index (Bucket 2) if
  measured too slow.

**Done when:** every datum the catalog CLI renders is available from
`objcatalog`/`objread`, covered by tests.

## 1B. Re-point every `orun catalog *` command off `catalogstore`/`statestore`

One PR per command (all currently import `catalogstore` + `catalogmodel`):

- [x] `cmd/orun/catalog_list.go` → `objcatalog.Load(...).Components`
- [x] `cmd/orun/catalog_describe.go` → `objcatalog` component view
- [x] `cmd/orun/catalog_tree.go` → `objcatalog` graph view
- [x] `cmd/orun/catalog_refs.go` → `refstore` ref listing (drop `catalogstore.listrefs`)
- [x] `cmd/orun/catalog_validate.go` → keep `catalogresolve` (strict); read/write via the object model only
- [x] `cmd/orun/catalog_diff.go` → `objcatalog` diff (1A); retire `internal/catalogdiff`
- [x] `cmd/orun/catalog_history.go` → `objread`/`objindex` join (1A); drop `ComponentExecutionIndex`
- [x] `cmd/orun/catalog_run_events.go` → object-graph event view
- [x] `cmd/orun/catalog_plan_resolve.go` → drop `revision` + `catalogstore`
- [x] `cmd/orun/catalog_refresh.go` → **remove the legacy write**: today (CS9) it
  writes *both* the object model (`objplan.RefreshCatalog`) and
  `catalogstore`/`catalogsync`. Keep only the object-model write; retire
  `internal/catalogsync` (NoopSyncer).
- [x] `cmd/orun/catalog.go` → drop the shared `catalogstore.New(stateStore)`
  helper + `statestore` wiring.
- [x] `cmd/orun/catalog_affected.go` → **already** on the object model; no change.

**Done when:** no `cmd/orun/catalog_*.go` imports `catalogstore` or `statestore`.

## 1C. Re-point the remaining read commands off `revision`/`statestore`

- [x] `cmd/orun/command_describe.go` — `describe revision|trigger|plan` still call
  `revision.ResolveRevision` + `statestore.ManifestPath/TriggerPath`. Move to
  `objread` (revision/trigger/plan are graph nodes).
- [x] `cmd/orun/command_get.go` — `get plans|triggers` reads
  `statestore.ManifestPath` + `revision.RevisionManifest`. Move to
  `objread`/`objplan`.
- [x] `cmd/orun/read_resolve.go` — drop the `statestore` read-store helper.
- [x] `cmd/orun/main.go` — remove the `statestore` read-store wiring once unused.

**Done when:** `status`/`logs`/`describe`/`get` read only the object graph.

## 1D. Kill the run-path dual-write (the heart of it)

- [x] `cmd/orun/command_run_revision.go` — **delete the
  `executionstate.CreateExecution` / `MarkTerminal` mirror.** The canonical
  execution is already written by `internal/objrun` (`command_run.go`); this
  file's only job is the legacy revision layout + `orun catalog history`. Dead
  once 1B/1C land.
- [x] `cmd/orun/command_run.go` — drop the `revision` resolve import if it only
  fed the mirror.
- [x] `cmd/orun/bridge_mirror_warn.go` — **delete** (its only purpose is
  surfacing `bridge-mirror-failed` events from the legacy layout; no mirror ⇒ no
  warnings).

**Done when:** `orun run` writes the object graph **only**; no second copy.

## 1E. Delete the legacy packages

After 1B–1D these have **zero** production importers:

- [x] `internal/catalogstore/` — delete
- [x] `internal/revision/` — delete (incl. `legacy.go`, `legacy_scan.go`, `manifest.go`)
- [x] `internal/executionstate/` — delete
- [x] `internal/statestore/` — delete (nothing rides it once `catalogstore`/`revision` are gone)
- [x] `internal/catalogdiff/`, `internal/catalogsync/` — delete (catalog-CLI-only)
- [x] Prune now-orphaned store-only types from `internal/catalogmodel`: deleted
  `SourceRef`/`CatalogRef` (refs), `ComponentGlobalIndex`/`ComponentIndexLocation`/
  `ComponentIndexPreview`/`ComponentExecutionIndex` (indexes), `ComponentHistoryEvent`
  + the `Event*` enum (events), and their `Kind*` constants + golden fixtures.
  **Kept:** the shared manifest types, the `RefName*` vocabulary (`catalog diff`
  selectors), and `ComponentExecutionRow` — still the row shape
  `catalog describe`/`history` render from the object-graph join.

**Done when:** the packages are gone; `go build ./...` + `go test ./...` green.

## 1F. Tighten the lint gate + docs

- [x] `scripts/check-object-model.sh` — extend the §4 import ban (today only
  `internal/state`) to also forbid `internal/catalogstore`,
  `internal/statestore`, `internal/revision`, `internal/executionstate`
  repo-wide.
- [x] Repoint/delete the deleted packages' test suites; confirm `make test` +
  `make test-object-model` + `make verify-generated` stay green, `-race` clean.
- [x] Mark **D‑7 / L‑3 done** in `specs/orun-catalog-state/risks-and-open-questions.md`;
  update `specs/orun-object-model/IMPLEMENTATION-STATUS.md` + `FOLLOW-UPS.md`.

**Done when:** the object model is the single persistence stack and the gate
enforces it.

### Critical path

```
1A ─→ 1B ─┐
          ├─→ 1D ─→ 1E ─→ 1F
1A ─→ 1C ─┘
```

Highest-leverage item: **1D** (delete the dual-write) — but only safe **after**
1A–1C move the readers off the legacy layout.

---

# Bucket 2 — `objindex` component→execution index (L‑2)

*Optional accelerator for 1A's history join; only if scan + filter is too slow.*

- [ ] Build a derived component→execution index in `internal/objindex` (mirrors
  the existing executions index) with reindex + walk-fallback.
- [ ] Switch `orun catalog history` to it.
- **Pull-in trigger:** scan + filter measured too slow **and** D‑7 underway.

---

# Bucket 3 — Epic: `orun-env-scoping` — ✅ SHIPPED (v2.15.0)

> **✅ DONE — shipped in v2.15.0 (ES1–ES5).** This was a **holding epic** ("do
> not implement"); the design was finalized and the feature shipped. It
> **converged away from the original breaking single-env design below** to the
> non-breaking **"Z" model**: `plan` still defaults to all environments, `--env`
> keeps its comma-separated list, and a new **`--all-envs`** flag plus a
> **fail-closed mutating `run`** (deprecation Phase A) carry the safety intent
> without a hard break. Scoped plans prune dangling edges with a warning; env
> promotion compiles to in-plan `dependsOn` ordering. See `specs/orun-env-scoping/`
> (`README.md` + `IMPLEMENTATION-STATUS.md`).
>
> The checklist below is the **original breaking design, retained for history and
> now superseded** — it was *not* implemented as written.

- [x] **3.0 Finalize the design** — resolved via the converged Z model
  (`specs/orun-env-scoping/design.md` §5 decision ledger Z-1…Z-7); epic promoted
  and shipped in v2.15.0.

Enforcement surfaces *(original breaking design — superseded by the Z model)*:

- [ ] **Schema/validation** — add `intent.defaultEnvironment`; validate it names a real env.
- [ ] **`--env` flag** (`plan`/`run`) — single value only; `a,b` → error; absence → default, not all-env.
- [ ] **`internal/expand/expander.go`** — scope expansion to one env, not env × components.
- [ ] **`trigger.ResolveActiveEnvironments`** — resolve to exactly one env (or fan out to N single-env runs).
- [ ] **`Plan.ActiveEnvironments`** — always length 1.
- [ ] **Promotion (`EnvironmentPromotion`)** — a gated run in the target env.
- [ ] **CI workflows + docs** — multi-env pipelines become N single-env invocations.
- [ ] **Migration / deprecation window** (design §4) — warn → error on the removed
  all-env / `a,b` behavior; upgrade note (set `defaultEnvironment`; split pipelines).

---

# Bucket 4 — Epic: `orun-affected-worker` (under review)

- [ ] **4.0 Review/approval gate** — currently "UNDER REVIEW — not a current
  requirement; do not implement." Get an explicit decision before any work.
- [ ] If approved: implement milestones **AW0 → AW5** (Cloudflare Worker, TS;
  consumes `impact/ownership.json` + `graph/dependencies.json` by content id from
  R2/KV) in a **separate service/repo**, conformance-gated against `orun catalog
  affected` (prereqs CS3/CS7 already done).

---

# Bucket 5 — Object-model optional follow-ups (`orun-object-model/FOLLOW-UPS.md`)

Not required for correctness; close for completeness.

- [x] **Resume — carry step logs forward** (small): re-attach prior step-log
  blobs for skip-completed jobs at seal (`internal/objrun` + `internal/runworktree` seam).
- [ ] **Packfile delta compression** (larger, profiling-gated): only if loose-object
  size becomes the bottleneck.
- [x] **`objectstore` atomic-write fault-injection seam** (test-only): the
  temp/fsync/rename error branches are now exercised via package seam vars
  (`osCreateTemp`/`fsyncFile`/`osRename`); package coverage is 92.5%.

---

# Bucket 6 — Cockpit-side catalog auto-refresh (TUI)

The cockpit reads `catalogs/current` and already gates on freshness
(`services.catalogFresh` = current source id vs the catalog's `SourceID`), but
on a miss it falls back to the live intent loader rather than resolving — the
"cockpit-side resolve is a tracked follow-up" noted in `catalog_source.go`. This
bucket closes that: the TUI keeps the object-model catalog fresh for what it
displays, instead of relying on `orun plan`/`run`/`catalog refresh` to have run.

**Design (agreed):**

- **Shared engine** `internal/catalogrefresh`: `EnsureFresh(objModelRoot,
  workspaceRoot, {Force})` — a cheap **source-hash staleness gate** (resolve the
  source snapshot, compare its content id to `catalogs/current`'s `SourceID`),
  then, only when stale (or `Force`), run `catalogresolve.BuildCatalog` +
  `objplan.RefreshCatalog`. Single source of truth with the CLI resolve path so
  CLI and TUI produce the **same** content-addressed catalog id (no churn).
- **Concurrency:** a **non-blocking advisory try-lock** around resolve+write so a
  concurrent CLI `orun catalog refresh` and the TUI don't both run the expensive
  resolve (the second skips). Data integrity is already guaranteed by the
  refstore lockfile + atomic ref rename — this only avoids wasted work.
- **On TUI open → refresh** (`Force`, even when dirty) so the cockpit opens on a
  current catalog; a brief `⟳ refreshing…` state covers the one-time cost.
- **In-session → manual + toggle:** `r` = manual refresh (always available);
  `a` = auto-refresh toggle (persisted in `prefs`). Auto runs the staleness gate
  on the existing live-view ticker and refreshes only on change — default chosen
  so a dirty tree doesn't resolve-on-every-edit unless the user opts in.
- **Stale badge:** when the source differs from the loaded catalog (auto off, or
  between ticks), show `⟳ catalog stale` prompting `r`.

- [x] **6A** — `internal/catalogrefresh` engine (staleness gate + resolve+write +
  try-lock); cmd/orun resolve path deduped onto it. *(#298)*
- [x] **6B** — TUI wiring: `services.RefreshCatalog`; refresh-on-open (force);
  manual refresh on `⌃r` + the `catalog.refresh` palette command; auto-refresh on
  the live-view ticker gated by the persisted `prefs.AutoRefresh`, toggled via
  the `catalog.autorefresh` palette command.
- [x] **6C** — visual "⟳ catalog stale" badge driven by
  `catalogrefresh.IsStale` (rendering only; the engine + gate already exist).

---

## Definition of done (program)

- **Legacy closed (Bucket 1): ✅ DONE.** `internal/catalogstore`,
  `internal/statestore`, `internal/revision`, `internal/executionstate`,
  `internal/catalogsync` deleted; the orphaned store-only types in
  `internal/catalogmodel` are pruned; `orun run` single-writes the object graph;
  the lint gate bans the imports repo-wide; `orun catalog *` is unregressed.
  (`internal/catalogdiff` is kept as the pure, store-free diff engine
  `catalog_diff.go` feeds object-model-reconstructed snapshots into.)
- **Cockpit auto-refresh (Bucket 6): ✅ DONE.** The shared
  `internal/catalogrefresh` engine (staleness gate + resolve+write + advisory
  try-lock) backs both the CLI resolve path and the TUI, so they produce the
  same content-addressed catalog id; the cockpit refreshes on open, exposes
  manual + auto-refresh, and shows a `⟳ stale` badge.
- **Follow-ups (Bucket 5): ✅ mostly DONE.** Resume step-log carry-forward and
  the `objectstore` atomic-write fault-injection seam are landed; packfile delta
  compression stays a documented, profiling-gated deferral.
- **Epics (Buckets 3–4):** **Bucket 3 (`orun-env-scoping`): ✅ DONE** — shipped in
  v2.15.0 as the converged "Z" model (ES1–ES5; see `specs/orun-env-scoping/`).
  **Bucket 4 (`orun-affected-worker`): still gated** — awaits the review/approval
  gate (4.0); it is a separate service/repo and the only remaining program epic.
  `objindex` (Bucket 2) is a pull-in only if the history scan is measured too slow.
</content>
</invoke>
