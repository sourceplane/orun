# Design

> The cockpit reads the catalog from the object graph; one change-detection
> engine over the catalog powers the cockpit's "what changed/affected" view and
> `plan`/`run --changed`; orun full-resolves for truth. This doc fixes the read
> path, the refresh triggers, the read/write coupling, the unified-engine
> overview, the convergence decision, the single future-remote note, and the
> sharpness register. The engine's detail is in `change-detection.md`.

## 1. Problem

1. **The cockpit renders from `intent.yaml`, not the graph.**
   `workspace_service.go` → `loader.LoadResolvedIntent` → `normalize` →
   `componentSummaries`. It shows *authored* intent, not the *resolved* catalog,
   and never touches `.orun`.
2. **The resolved catalog is in the graph, write-only.** `orun plan` →
   `nodes.AssembleCatalog` writes `catalogs/current`; `internal/objread` has no
   catalog reader.
3. **Change detection is ad-hoc and unshared.** `--changed` is five disjoint
   pieces inside `cmd/orun/main.go` (`change-detection.md` §1); the cockpit has
   no concept of "what changed." There is no single definition of "affected."
4. **The freshness oracle already exists.** `CatalogSnapshot.SourceID` +
   `nodes.SourceID(algo, src)` (no write) make "is the catalog current?" a single
   id compare.

## 2. Goals / non-goals

**Goals**
- The cockpit renders the **resolved** catalog from the graph, always fresh.
- **One** change-detection engine (`internal/affected`) over the catalog —
  ownership map + virtual Merkle tree + dependency graph — used by the cockpit,
  `plan --changed`, `run --changed`, and `orun catalog affected`.
- orun **always full-resolves** when a refresh is needed (D-1).
- The object-model catalog is lossless and canonical for these surfaces.
- The cockpit is **one consumer** of (state store + engine) over a **read seam**
  (view-models) + an **action seam** (run); a future web UI is the same read seam,
  read-only. The catalog→component→job→logs drill-down mirrors the object lineage
  (`consumers.md`).
- The cockpit can **select an environment and run a component in it, on the
  existing env model** (no schema change, no change to `orun plan`/`run`). The
  single-env *direction* (`defaultEnvironment`, removing the all-env path) is a
  separate breaking epic — `specs/orun-env-scoping/` (deferred L-1);
  `environments.md` covers only the cockpit's current behavior.

**Non-goals**
- Incremental/partial resolution (rejected, D-1).
- The edge worker (`specs/orun-affected-worker/`, **under review** — §7).
- Rewriting `expand.DependencyResolver` outside the changed path.
- Retiring `internal/catalogstore` wholesale (D-7).

## 3. Pillar A — the catalog read path

### 3.1 The freshness gate
`LoadWorkspace`: `cur := nodes.SourceID(algo, BuildSourceNode(ws))`; if a catalog
is present and `cur == catalog.SourceID` → read from the store (`objcatalog`); else
full `BuildCatalog`, render the in-memory view, write-through (§3.3). `SourceID`
includes `DirtyHash` → locally the gate sees uncommitted edits; in CI the tree is
clean → stable id → fast path.

### 3.2 `internal/objcatalog` — the read view (new)
Mirrors `internal/objread` (`Reader{store, refs, root}`, default `catalogs/current`).
`Load(ctx, ref) CatalogView` decodes `catalog.json` + `components/` + `graph/` +
(tolerant) `impact/` (ownership + fingerprints). Returns a presentation-neutral
`CatalogView`; the cockpit maps `CatalogComponentView → services.ComponentSummary`
in `services` (not `objview`). Redeclares tree-filename consts locally, as
`objread` does.

### 3.3 Read/write coupling — DECISION
Rendering is a pure function of the resolved `CatalogView`; the store write is
**best-effort, asynchronous, CAS-safe** (`nodewriter.MoveRefs` retries), and a
write failure is a logged warning, never a render failure. Write-through on every
stale refresh including dirty previews (content-addressing + GC make churn cheap).
Rejected alternative (scratch ref / never-write) recorded in D-3. "Reading is
read-only" holds *in effect*: the render is pure, the write is a non-blocking
cache update.

### 3.4 Refresh trigger policy — when the gate runs
Two triggers, one `RefreshCatalog` seam (Q-1 → a shared helper):

1. **Every orun command (universal, best-effort).** A root pre-run hook runs the
   gate around any command; a hit is a free `SourceID` compare, a miss
   full-resolves + write-throughs. **Best-effort, time-bounded, non-fatal, and
   debounced** (≤ one resolve per `refreshTTL` per source, D-8). `plan`/`catalog
   refresh` resolve authoritatively and are no-ops for the hook.
   `ORUN_NO_AUTO_REFRESH=1` disables it.
2. **Cockpit interval (live view).** Renders from a **live in-memory
   `CatalogView`**; a background ticker re-runs the gate every `refreshInterval`
   (D-9, ~2–5s), resolves off the UI thread (`tea.Cmd`), atomically swaps the
   snapshot, write-throughs best-effort. `ctrl+r` forces a tick. Picks up edits
   and other processes' writes without a keystroke.

The cost/staleness trade (S-9): the gate keeps the clean case free; the debounce
bounds the dirty case to one resolve per TTL.

## 4. The `Path` fix (prerequisite)

The graph catalog is lossy today: `mapManifest` drops `catalogmodel.Identity.Path`
because `nodes.ComponentIdentity` has no `Path` field. CS1 adds it (+ `mapManifest`
map). Consequence: the manifest/catalog ids change on the next resolve
(content-addressing absorbs it; the memo misses once). Guarded by the parity test
(CS8).

## 5. Pillar B — the unified change-detection engine

`internal/affected` is **one** definition of "what changed / what's affected,"
over the catalog substrate (ownership map + virtual Merkle tree + dependency
graph). It **consolidates and re-bases** the five ad-hoc `--changed` pieces
(`git.ChangeDetector`, `git.DiffIntent`, `main.go` path matching,
`expand.DependencyResolver`, intent-impact) into a surface-agnostic engine with
two change sources (git diff; virtual-tree fingerprints), preserving observable
selection (parity-gated). Full detail and the migration mapping: **`change-
detection.md`**. Consumers: cockpit "what changed/affected" (Q2), `plan
--changed`, `run --changed`, `orun catalog affected`.

The engine's correctness contract (CD-1…CD-4, `change-detection.md` §5) — over-
report, structural ⇒ refresh/gate, intent-impact preserved, parity — exists
because **`plan`/`run --changed` must never under-select** (skipping a changed
component is a local correctness failure, not a cloud concern).

## 6. Why the artifacts ship (no provision-for-cloud framing)

`impact/ownership.json` and `impact/fingerprints/` ship because **orun's own
change-detection engine consumes them** — the ownership map is how the engine maps
paths to components; the fingerprints are the cockpit's content source. They are
not published speculatively for a future worker. The component **dependency
graph** is already published (`graph/dependencies.json`); the engine walks it
in-process (no materialized closure needed at component scale). This keeps the
provision honest: every published artifact has a local consumer in this spec.

## 7. Future remote consumers (high-level only — no accommodation now)

A remote/edge consumer of the same artifacts is described in
`specs/orun-affected-worker/` and is **under review / not a requirement**. The
only design fact worth recording: the engine's inputs (ownership map,
fingerprints, dependency graph) are **content-addressed and part of the catalog
closure**, so they already push to remote storage via the existing `objremote`
substitution with no change here. If/when the worker is approved, any orun-side
support is specified then. This spec makes **no** structural accommodation for it.

## 8. Package boundaries

| Package | Change |
|---------|--------|
| `internal/objcatalog` | **new** — catalog read view over the graph (mirrors `objread`) |
| `internal/affected` | **new** — the unified change-detection engine (`change-detection.md`) |
| `internal/nodes` | add `Path` to `ComponentIdentity`; `ImpactOwnership` + fingerprint node types |
| `internal/objplan` | derive + publish `impact/ownership.json` + `impact/fingerprints/`; a catalog-only `RefreshCatalog` seam |
| `internal/git` | `changes.go`/`intentdiff.go` wrapped/moved behind the engine (kept logic) |
| `internal/expand` | `DependencyResolver` changed-path use re-based onto the catalog graph (parity-gated); other callers untouched |
| `internal/cockpit/viewmodel` | **extend** — `CatalogView`/`ComponentView` view-models (the read seam shared by the TUI and a future web UI); reuse existing `RunView`/`LogsView` (`consumers.md`) |
| `internal/tui/services` | freshness gate + live view; render view-models; the changed/affected view (Q2); the run **action seam** (TUI-only, via `objrun`) |
| `cmd/orun` | universal refresh hook; `plan`/`run --changed` onto the engine; `orun catalog affected` |

## 9. Invariants

1. **Read purity.** Rendering is pure; the store write is best-effort (§3.3).
2. **Freshness soundness.** Fast path **iff** `SourceID(cur) == catalog.SourceID`.
3. **Lossless catalog.** Every `ComponentSummary` field is recoverable from the
   graph catalog (parity test, CS8).
4. **Determinism.** `ownership.json` and fingerprints are deterministic functions
   of the resolved catalog / source (dedup + edge-cacheable).
5. **One definition of affected.** Cockpit, plan, run, and the CLI compute
   affected through `internal/affected` — no second implementation.
6. **Selection parity.** `plan`/`run --changed` selection is unchanged by the
   migration (CD-4 parity gate) before the old path is removed.
7. **Every artifact has a local consumer** (§6).

## 10. Where this model needs sharpness (register)

| # | Sharpness point | Resolution |
|---|-----------------|------------|
| S-1 | "Affected" must not be re-defined four ways. | One engine (`internal/affected`); invariant 5. |
| S-2 | Migration could silently change `--changed` selection. | CD-4 parity gate; old path removed only after parity (`test-plan.md`). |
| S-3 | A `component.yaml` dep-edge edit changes structure with no new path. | CD-2: any `component.yaml` edit is structural. |
| S-4 | Under-selecting in `plan/run --changed` ships a broken thing. | CD-1 over-report (MUST); parity superset assertion. |
| S-5 | Cockpit-read mutating the store is a coupling smell. | D-3: best-effort, non-blocking, CAS-safe. |
| S-6 | The graph catalog is lossy (`Path`). | CS1 fix + parity test. |
| S-7 | Two materializations coexist. | Convergence is surface-scoped; D-7 tracks full `catalogstore` retirement. |
| S-8 | Q2 cockpit changed view — confirmed in scope. | `cli-surface.md` §1; engine exposes `DirectlyChanged`/`Dependents`. |
| S-9 | Refresh-on-every-command cost on a churning tree. | §3.4 debounce (D-8); best-effort/time-bounded. |
| S-10 | Intent-impact `watch` semantics could be lost in the migration. | CD-3: preserved verbatim; fixture coverage (`test-plan.md`). |
| S-11 | Fingerprint determinism across machines (macOS NFD/NFC, separators). | Project git's normalization for committed files; canonical-encode the overlay (`change-detection.md` §3). |
| S-12 | The cockpit must be a *consumer*, not reach into the store; a web UI must be read-only by construction. | `consumers.md`: read seam (view-models) vs action seam kept separate; web UI gets the read seam only. G-1/G-2 resolved (scan+filter; single-env component-scoped run). |
| S-13 | Single-env enforcement is a breaking run-path change masquerading as a small cockpit feature. | Split out: the cockpit runs on the **existing** env model (`environments.md`); the breaking redesign (`defaultEnvironment`, removing all-env) is its own epic `specs/orun-env-scoping/` (deferred L-1). |
