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
| Status | **Draft → Ready for implementation** (Bucket 1) |
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

Must land in dependency order: **read surface → CLI re-point → kill dual-write →
delete packages → gate.** Each `[ ]` is a reviewable PR-sized unit.

## 1A. Prerequisite — finish the object-model catalog read surface

`internal/objcatalog.Reader` today only `Load`s a single `CatalogView`. The
catalog CLI needs more before it can move off `catalogstore`.

- [ ] **Two-snapshot catalog diff** in `internal/objcatalog` (replaces
  `internal/catalogdiff` + `catalogstore` reads behind `orun catalog diff`).
- [ ] **Catalog event / validation views** for `catalog_run_events.go` and
  `catalog_validate.go` (currently `catalogstore.events` / strict re-resolve into
  the legacy store).
- [ ] **Component → execution history join** for `orun catalog history` (today
  `catalogstore.ReadComponentExecutionIndex`). **Decision:** start with
  `objread` scan + filter (`objread.PlanSummary` over `RunSummary.Components`,
  the CS6 v1 choice); only build the `objindex` component index (Bucket 2) if
  measured too slow.

**Done when:** every datum the catalog CLI renders is available from
`objcatalog`/`objread`, covered by tests.

## 1B. Re-point every `orun catalog *` command off `catalogstore`/`statestore`

One PR per command (all currently import `catalogstore` + `catalogmodel`):

- [ ] `cmd/orun/catalog_list.go` → `objcatalog.Load(...).Components`
- [ ] `cmd/orun/catalog_describe.go` → `objcatalog` component view
- [ ] `cmd/orun/catalog_tree.go` → `objcatalog` graph view
- [ ] `cmd/orun/catalog_refs.go` → `refstore` ref listing (drop `catalogstore.listrefs`)
- [ ] `cmd/orun/catalog_validate.go` → keep `catalogresolve` (strict); read/write via the object model only
- [ ] `cmd/orun/catalog_diff.go` → `objcatalog` diff (1A); retire `internal/catalogdiff`
- [ ] `cmd/orun/catalog_history.go` → `objread`/`objindex` join (1A); drop `ComponentExecutionIndex`
- [ ] `cmd/orun/catalog_run_events.go` → object-graph event view
- [ ] `cmd/orun/catalog_plan_resolve.go` → drop `revision` + `catalogstore`
- [ ] `cmd/orun/catalog_refresh.go` → **remove the legacy write**: today (CS9) it
  writes *both* the object model (`objplan.RefreshCatalog`) and
  `catalogstore`/`catalogsync`. Keep only the object-model write; retire
  `internal/catalogsync` (NoopSyncer).
- [ ] `cmd/orun/catalog.go` → drop the shared `catalogstore.New(stateStore)`
  helper + `statestore` wiring.
- [ ] `cmd/orun/catalog_affected.go` → **already** on the object model; no change.

**Done when:** no `cmd/orun/catalog_*.go` imports `catalogstore` or `statestore`.

## 1C. Re-point the remaining read commands off `revision`/`statestore`

- [ ] `cmd/orun/command_describe.go` — `describe revision|trigger|plan` still call
  `revision.ResolveRevision` + `statestore.ManifestPath/TriggerPath`. Move to
  `objread` (revision/trigger/plan are graph nodes).
- [ ] `cmd/orun/command_get.go` — `get plans|triggers` reads
  `statestore.ManifestPath` + `revision.RevisionManifest`. Move to
  `objread`/`objplan`.
- [ ] `cmd/orun/read_resolve.go` — drop the `statestore` read-store helper.
- [ ] `cmd/orun/main.go` — remove the `statestore` read-store wiring once unused.

**Done when:** `status`/`logs`/`describe`/`get` read only the object graph.

## 1D. Kill the run-path dual-write (the heart of it)

- [ ] `cmd/orun/command_run_revision.go` — **delete the
  `executionstate.CreateExecution` / `MarkTerminal` mirror.** The canonical
  execution is already written by `internal/objrun` (`command_run.go`); this
  file's only job is the legacy revision layout + `orun catalog history`. Dead
  once 1B/1C land.
- [ ] `cmd/orun/command_run.go` — drop the `revision` resolve import if it only
  fed the mirror.
- [ ] `cmd/orun/bridge_mirror_warn.go` — **delete** (its only purpose is
  surfacing `bridge-mirror-failed` events from the legacy layout; no mirror ⇒ no
  warnings).

**Done when:** `orun run` writes the object graph **only**; no second copy.

## 1E. Delete the legacy packages

After 1B–1D these have **zero** production importers:

- [ ] `internal/catalogstore/` — delete
- [ ] `internal/revision/` — delete (incl. `legacy.go`, `legacy_scan.go`, `manifest.go`)
- [ ] `internal/executionstate/` — delete
- [ ] `internal/statestore/` — delete (nothing rides it once `catalogstore`/`revision` are gone)
- [ ] `internal/catalogdiff/`, `internal/catalogsync/` — delete (catalog-CLI-only)
- [ ] Prune now-orphaned store-only types from `internal/catalogmodel`
  (refs/indexes/events/`ComponentExecutionRow`) — keep the shared manifest types.

**Done when:** the packages are gone; `go build ./...` + `go test ./...` green.

## 1F. Tighten the lint gate + docs

- [ ] `scripts/check-object-model.sh` — extend the §4 import ban (today only
  `internal/state`) to also forbid `internal/catalogstore`,
  `internal/statestore`, `internal/revision`, `internal/executionstate`
  repo-wide.
- [ ] Repoint/delete the deleted packages' test suites; confirm `make test` +
  `make test-object-model` + `make verify-generated` stay green, `-race` clean.
- [ ] Mark **D‑7 / L‑3 done** in `specs/orun-catalog-state/risks-and-open-questions.md`;
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

# Bucket 3 — Epic: `orun-env-scoping` (breaking single-env change)

A **holding epic** today ("do not implement"). Promote to a ready spec by
resolving the open points, then implement across the run path
(`specs/orun-env-scoping/design.md`).

- [ ] **3.0 Finalize the design** — resolve **G‑5** (no-default resolution;
  propose fallback-then-error), **G‑6** (`--env each-of` fan-out vs N
  invocations), **G‑7** (trigger → one env), **G‑9** (`defaultEnvironment`
  placement), **G‑10** (promotion = gated run in target env). Promote
  epic → ready spec.

Enforcement surfaces (design §3 — all breaking):

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

- [ ] **Resume — carry step logs forward** (small): re-attach prior step-log
  blobs for skip-completed jobs at seal (`internal/objrun` + `internal/runworktree` seam).
- [ ] **Packfile delta compression** (larger, profiling-gated): only if loose-object
  size becomes the bottleneck.
- [ ] **`objectstore` atomic-write fault-injection seam** (test-only): lift coverage
  past the 90% gate on the temp/fsync/rename error branches.

---

## Definition of done (program)

- **Legacy closed (Bucket 1):** `internal/catalogstore`, `internal/statestore`,
  `internal/revision`, `internal/executionstate`, `internal/catalogdiff`,
  `internal/catalogsync` deleted; `orun run` single-writes the object graph; the
  lint gate bans the imports repo-wide; `orun catalog *` is unregressed.
- **Epics resolved:** `orun-env-scoping` promoted + shipped (or explicitly
  parked); `orun-affected-worker` reviewed (approved + built elsewhere, or
  closed).
- **Follow-ups:** Bucket 5 items closed or consciously left as documented
  limitations.
</content>
</invoke>
