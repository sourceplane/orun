# Spec: orun-catalog-state

> **📦 ARCHIVED — implemented & stable.** All milestones **CS1–CS9 shipped**
> (see `IMPLEMENTATION-STATUS.md`): the component catalog is served from
> object-graph state and the unified `internal/affected` change-detection engine
> powers `plan`/`run --changed`, `orun catalog affected`, and the cockpit's
> changed/affected view. The **code** (`internal/affected`, `internal/objcatalog`,
> the cockpit read seam) is now the living reference; this spec is kept as a
> frozen historical record. `specs/orun-service-catalog/` builds on this substrate.

**The component catalog is served from the object-graph state, and orun gains a
single change-detection engine — built on a virtual Merkle tree of component
inputs — that powers the cockpit's "what changed / what's affected" view *and*
`plan --changed` / `run --changed`.** Change detection becomes one model, used
everywhere.

This is a projection-and-unification spec, not a new store. It builds on
`specs/orun-object-model/` (the content-addressed object graph, merged) and
`specs/archive/orun-component-catalog/` (`catalogresolve`, `catalogmodel`). It (1) makes
the cockpit render the catalog from the graph, (2) replaces the scattered,
ad-hoc `--changed` logic with one reusable change-detection engine over the
catalog, and (3) makes the object-model catalog the canonical catalog home for
these surfaces.

> **A future remote/edge consumer of the same data exists in
> `specs/orun-affected-worker/`, but it is UNDER REVIEW and NOT a requirement
> here.** This spec makes **no** design accommodation for it beyond a single
> high-level note (`design.md` §7): the engine's inputs are content-addressed
> and could later be consumed remotely. When the worker is reviewed, any orun
> changes to support it will be specified then.

## Status

| Field | Value |
|-------|-------|
| Status | **✅ Implemented — CS1–CS9 complete (shipped); archived as a frozen record.** The change-detection engine + catalog-from-state is live; the code is the reference. |
| Builds on | `specs/orun-object-model/` (object graph), `specs/archive/orun-component-catalog/` (`catalogresolve`, `catalogmodel`) |
| Replaces | the ad-hoc `--changed` path: `internal/git/changes.go` + `internal/git/intentdiff.go` usage in `cmd/orun/main.go` (`isPathChanged`, `collectChangedComponents`) + `expand.DependencyResolver` in the changed path — consolidated into one engine (`change-detection.md`) |
| Downstream | `specs/orun-affected-worker/` — **under review, out of scope** |
| Target branch | `main` (PRs merged incrementally) |
| Decisions locked | full-resolve-always; one change-detection engine for cockpit + plan + run; virtual-Merkle-tree component fingerprints are **in scope** (the engine's content source); migration must **reproduce existing `--changed` selection** (parity-gated); object-model catalog canonical for these surfaces; refresh on every command (best-effort, debounced) + cockpit live in-memory view on an interval; the worker is deferred |

## The one-paragraph thesis

Two loops are open today. (1) The resolved catalog already lives in the object
graph but nothing reads it back, so the cockpit renders authored `intent.yaml`
instead of the resolved catalog. (2) Change detection — "which components did
this change touch?" — is implemented ad-hoc inside `cmd/orun/main.go` for
`--changed`, with its own git calls, path matching, intent-diff, and dependency
walk, and the cockpit has no notion of it at all. We close both with one move:
the catalog is read from the graph, and a single **change-detection engine**
(`internal/affected`) computes the changed/affected component set over the
catalog's **ownership map** and **dependency graph**, with the component
**virtual Merkle tree** (per-component input fingerprints) as its content
source. `plan`, `run`, the cockpit, and `orun catalog affected` all call the
same engine — one definition of "affected," not four.

## The pillars

```
PILLAR A — CATALOG FROM STATE              PILLAR B — UNIFIED CHANGE DETECTION
────────────────────────────              ──────────────────────────────────
sourcectx → SourceID(current)             ChangeSource (git diff  OR  virtual-tree fingerprints)
  == catalog.SourceID ? read : resolve      → changed components (ownership map)
  → live in-memory CatalogView              → intent classification (none/global/components, intent-impact)
  → cockpit renders the resolved catalog    → dependency closure (catalog graph: deps + dependents)
                                            → Affected result
  CONSUMERS of Pillar B: cockpit "what changed/affected" · plan --changed · run --changed · orun catalog affected
```

## Read order

1. **`design.md`** — the two pillars, the freshness gate + refresh triggers, the
   read/write decision, the unified-engine overview, the convergence decision,
   package boundaries, invariants, the single future-remote note, and the
   sharpness register.
2. **`change-detection.md`** — the heart of this spec: a review of the existing
   `--changed` model, the new `internal/affected` engine, the virtual Merkle
   tree, the two change sources, the **migration mapping** (old → new), and the
   preserved semantics (intent-impact, watches, the three categories).
3. **`consumers.md`** — the cockpit/web-UI consumer model: the read seam
   (view-models) vs the action seam, the catalog→component→job→logs drill-down
   mapped to the object lineage, the event model, and the gaps (G-1…G-4).
4. **`environments.md`** — the cockpit env selector + component-scoped run on the
   **existing** env model (no schema change). The single-env redesign is a
   separate epic, `specs/archive/orun-env-scoping/`.
5. **`data-model.md`** — the ownership map, the virtual-tree fingerprints, the
   `Affected` result type, the catalog read-view types, the `Path` fix.
6. **`cli-surface.md`** — universal refresh, cockpit behavior + the changed
   view, `plan/run --changed` migration, `orun catalog affected`.
7. **`implementation-plan.md`** — milestones **CS1 → CS9**.
8. **`test-plan.md`** — the **parity gate** (new engine == existing `--changed`),
   freshness property tests, determinism, the changed-detection fixtures.
9. **`risks-and-open-questions.md`** — decisions, open questions, risks, and the
   **deferred / needs-later-attention register**.

## Phase boundaries

| In scope (this spec) | Out of scope |
|----------------------|--------------|
| `internal/objcatalog` (catalog read view); `internal/affected` (the unified engine) consolidating `git.ChangeDetector` + `git.DiffIntent` + the main.go changed logic + `expand.DependencyResolver`(changed path); the virtual-Merkle-tree component fingerprints (`impact/fingerprints/`) + ownership map (`impact/ownership.json`); the `Path` fix; cockpit freshness gate + live view + **changed/affected view (Q2)**; `plan --changed` / `run --changed` migration onto the engine; `orun catalog affected`; object-model catalog canonical for these surfaces | The edge worker (`specs/orun-affected-worker/`, **under review**); reverse-closure *materialization* (the engine walks the graph in-process); retiring `internal/catalogstore` wholesale (follow-on, D-7); remote/SaaS push of the artifacts (rides existing `objremote`); a non-`--changed` rewrite of `expand.DependencyResolver` (only the changed path migrates) |

## Convergence decision (locked)

The object-model catalog is canonical for the cockpit **and the change-detection
engine** (the ownership map + fingerprints + dependency graph all come from the
catalog snapshot). The cockpit stops reading `intent.yaml` for the component
list; `--changed` stops re-deriving ownership/deps ad-hoc. Fully retiring
`internal/catalogstore` (it still backs `orun catalog list/describe/tree`) is a
follow-on (D-7), not regressed here. Prerequisite: the object-model catalog must
be lossless (the `Path` fix, CS1).

## Document conventions

- Go for interfaces, JSON for on-disk schemas. Forward-slash logical paths,
  root-relative to `.orun/objectmodel/`.
- Object IDs `"<algo>:<hex>"`. `lowerCamelCase`. RFC 3339 / Z.
- "MUST / SHOULD / MAY" carry RFC 2119 weight in `change-detection.md` (the
  correctness contract) and the schema sections.

## Out-of-band references

- Predecessor specs: `specs/orun-object-model/`, `specs/archive/orun-component-catalog/`.
- Downstream (under review): `specs/orun-affected-worker/`.
- Packages consumed/changed: `internal/objread` (pattern), `internal/objplan`,
  `internal/nodes`, `internal/catalogresolve`, `internal/sourcectx`,
  `internal/git` (`changes.go`, `intentdiff.go` — consolidated), `internal/expand`
  (`DependencyResolver` — changed path re-based), `internal/tui/services`,
  `cmd/orun`.
