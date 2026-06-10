# Change Detection — the unified engine

> The heart of this spec. Today "what changed?" is computed ad-hoc inside
> `cmd/orun/main.go` for `--changed`, and the cockpit has no notion of it. This
> doc reviews that existing model, defines one engine (`internal/affected`) over
> the catalog substrate (ownership map + virtual Merkle tree + dependency
> graph), and specifies the **migration** that re-bases `plan`/`run`/cockpit onto
> it without changing observable selection. RFC 2119 keywords are binding.

## 1. The existing model (as-built review)

`--changed` (and `run` when `triggerResolution.PlanScope == "changed"`) is five
pieces, none reusable as a unit:

| Piece | Where | What it does |
|-------|-------|--------------|
| **File set** | `internal/git/changes.go` `ChangeDetector` | Nx-style changed files from `{Files, Uncommitted, Untracked, Base, Head}`; merge-base resolution; `origin/<base>` CI fallback. |
| **Path → component** | `cmd/orun/main.go` `isPathChanged` / `collectChangedComponents` | Prefix-matches changed files against each component's `Path` (ad-hoc). |
| **Intent change** | `internal/git/intentdiff.go` `DiffIntent` | Semantic YAML diff of `intent.yaml` → `none` / `global` (change outside `components`) / `components` (added/modified/removed inline). |
| **Intent-impact policy** | `cmd/orun/main.go` + `--intent-impact` | On a `global` intent change: `all` → every component; `watch` → components whose `Change.Watches` intersect the changed sections; `none` → nothing. Inline `components` changes → those component names. |
| **Dependency closure** | `expand.DependencyResolver.CategorizeDependencies` (over the **normalized intent**) | Splits the result into three sets: `changed`, `dependencies` (what changed depends on), `dependents` (what depends on changed). |

**What's good and must be preserved:** the Nx-style file selection, the
intent-diff classification, the intent-impact `all/watch/none` semantics with
per-component `Change.Watches`, and the three-category split. The migration is a
**consolidation + re-basing**, not a redesign — observable selection MUST NOT
change (parity-gated, `test-plan.md`).

**What's wrong with it:** it lives inside `main.go`, is not callable by the
cockpit, re-derives ownership by ad-hoc prefix matching, and walks dependencies
over the normalized intent rather than the resolved catalog — so there is no
single, shared definition of "affected."

## 2. The engine — `internal/affected`

One package, one definition. Surface-agnostic; called by plan, run, cockpit, and
`orun catalog affected`.

```go
// Detector computes the affected component set for a change, over a catalog.
type Detector struct {
    catalog  *objcatalog.CatalogView   // ownership map + dependency graph + fingerprints
    policy   IntentImpact              // all | watch | none (default watch)
}

// Result is the single shape every surface consumes.
type Result struct {
    DirectlyChanged []string            // components whose own inputs changed
    Dependencies    []string            // forward deps of DirectlyChanged
    Dependents      []string            // reverse deps (transitive) of DirectlyChanged
    Affected        []string            // the union the surface acts on (= changed ∪ dependents; see §6)
    IntentMode      IntentMode          // none | global | components
    Confidence      Confidence          // high | low
    NeedsFullResolve bool               // structural/global uncertainty (§5)
    Explain         []ExplainEntry      // provenance for --explain
}

func (d *Detector) Detect(ctx, src ChangeSource) (Result, error)
```

### 2.1 ChangeSource — two implementations, one pipeline

A `ChangeSource` answers "which component directories' inputs changed?" The
engine then classifies, applies intent-impact, and walks the dependency graph —
identically regardless of source.

```go
type ChangeSource interface {
    // ChangedPaths returns the changed workspace-relative file set + whether
    // intent.yaml is among them (with its before/after for DiffIntent).
    ChangedPaths(ctx) (files []string, intent IntentChange, err error)
}
```

- **`GitChangeSource`** — wraps the existing `git.ChangeDetector`
  (`{Files,Uncommitted,Untracked,Base,Head}`). This is the migration of today's
  `--changed`: used by `plan --changed`, `run --changed`, CI, and
  `orun catalog affected --base/--head`. **Committed-only in CI** (clean tree),
  committed + working overlay locally.
- **`FingerprintChangeSource`** — the **virtual Merkle tree** (`data-model.md`
  §3): compares the *current* per-component input fingerprints against the
  fingerprints the catalog recorded at resolve time; a component whose subtree
  hash differs is changed. Content-aware (a comment-only edit that doesn't change
  the resolved inputs → not changed: **early cutoff**, fewer false positives than
  raw path matching). Used by the **cockpit live/dirty view**, where re-shelling
  `git diff` every tick is undesirable and content-identity matters.

Both feed the **same** ownership → intent-impact → dependency pipeline. `git`
gives a file set the engine maps via the ownership map; `fingerprint` gives a
changed-component set directly. Either way the downstream is identical.

### 2.2 The pipeline (identical for both sources)

```
1. changed files (or changed components) from the ChangeSource
2. map files → components via the ownership map (deepest-prefix); classify the rest
3. if intent.yaml changed → DiffIntent → none|global|components
     global   → apply intent-impact: all | (watch ∩ Change.Watches) | none
     components → added/modified/removed component names
     (a component.yaml edit is STRUCTURAL — §5)
4. DirectlyChanged = component-class files' owners  ∪  intent-derived components
5. walk the catalog dependency graph:
     Dependencies = forward closure of DirectlyChanged
     Dependents   = reverse  closure of DirectlyChanged
6. Affected = DirectlyChanged ∪ Dependents      (the set surfaces act on; §6)
7. Confidence/NeedsFullResolve per §5
```

## 3. The virtual Merkle tree (`impact/fingerprints/`)

Per-component input fingerprints, derived at resolve time and stored as a catalog
sibling. **In scope** — its local consumer is the cockpit's content-aware change
detection (and it is the substrate a future remote consumer would mirror).

- One leaf-set per component: the hash of its **input file set** — its directory,
  **non-recursive**, matching the resolver's verified read-set (`component.yaml`
  + the inference candidates: `package.json`, lockfiles, `Dockerfile`,
  `Containerfile`, `*.tf`, `Chart.yaml`, `README.md`). Plus a `global` leaf for
  the catalog-relevant `intent.yaml` blocks.
- **Computed cheaply.** For committed files the fingerprint is projected from
  `git ls-tree` (git already content-addresses the tree — no re-hashing). Locally
  a **working overlay** hashes only the dirty/untracked subset (the git-index
  analogue, as `internal/runworktree` already does for runs). CI (clean) → pure
  git projection, zero overlay.
- **Soundness.** Fingerprinting the whole non-recursive component dir is a *sound
  over-approximation* of the resolver's read-set (it can only over-report, never
  miss). The candidate-set listing also captures *additions* (a new
  `package.json` changes the dir listing → the fingerprint changes), which
  provenance alone would miss. See `archive/orun-component-catalog/resolution-pipeline.md`.

`impact/fingerprints/` is **always present** so the catalog tree shape stays
uniform and its Merkle id deterministic.

## 4. Migration mapping (old → new)

| Existing | New home | Disposition |
|----------|----------|-------------|
| `git.ChangeDetector` (`internal/git/changes.go`) | `affected.GitChangeSource` | **Kept**, wrapped by the engine. The Nx file-selection logic is good; it becomes the engine's git source. |
| `git.DiffIntent` (`internal/git/intentdiff.go`) | `affected` (intent classification) | **Kept/moved** behind the engine; same `none/global/components` semantics. |
| `isPathChanged` / `collectChangedComponents` (`main.go`) | the ownership map + engine pipeline | **Deleted** from `main.go`; replaced by `ownership.json` lookup. |
| `expand.DependencyResolver.CategorizeDependencies` (changed path) | engine dependency walk over the catalog graph | **Re-based**: deps/dependents walked over `graph/dependencies.json` instead of the normalized intent. **Parity-asserted** against the old resolver before the old call site is removed. |
| `--intent-impact` + `Change.Watches` + `watchesIntersect` | engine intent-impact stage | **Preserved verbatim** (semantics unchanged). |
| `--explain` printer (`main.go`) | `Result.Explain` + a thin printer | **Preserved**: the engine returns structured `Explain`; the CLI renders it. |

`cmd/orun/main.go`'s `changedOnly` branch becomes: build a `GitChangeSource` from
the existing `ChangeOptions`, call `affected.Detect`, and select jobs from
`Result` exactly as today. The cockpit builds a `FingerprintChangeSource` and
calls the same `Detect`.

## 5. Correctness contract (RFC 2119)

These were implicit in the old model; the engine makes them explicit because
**`plan`/`run --changed` must never under-select** (skipping a changed component
ships a broken thing — the same stakes as the worker, but local).

- **CD-1 (over-report).** On classification ambiguity the engine **MUST**
  over-approximate `Affected`, never under. A malformed `intent.yaml` → `global`
  (not `none`); an unknown-owner changed path under a component dir → that
  component.
- **CD-2 (structural).** A `component.yaml` add/remove/**edit** **MUST** be
  treated as `structural`: it may add/remove a component or change a `dependsOn`
  edge that the *currently loaded* catalog graph doesn't reflect. The engine sets
  `Confidence = low`, `NeedsFullResolve = true`, and (for safety) treats the
  owning component as changed. For `plan`/`run` this is moot — they full-resolve
  anyway — but the flag tells the cockpit to refresh and a CLI consumer to gate.
- **CD-3 (intent-impact preserved).** `global` intent changes apply `all` /
  `watch ∩ Change.Watches` / `none` exactly as today (CD is not an excuse to
  change selection).
- **CD-4 (parity).** Until the parity gate (`test-plan.md`) is green, the engine
  **MUST** produce the same job selection as the existing `--changed` path on the
  fixture corpus. The old code path is removed only after parity holds.

> Note: `plan` and `run` **always full-resolve** the catalog (D-1), so for them
> the engine reads a *fresh* graph and `NeedsFullResolve` is informational. The
> flag matters for the cockpit (which renders from a possibly-stale snapshot
> between ticks) and for `orun catalog affected` (which reads a published
> catalog). This is where CD-2's "structural ⇒ refresh/gate" earns its keep.

## 6. What `Affected` includes (a fidelity decision)

The old `orun component --changed` view shows `changed`, `dependencies`, and
`dependents` as three labeled groups. For **job selection** (`plan`/`run`), the
engine's `Affected` MUST equal whatever the existing `--changed` plan includes —
captured exactly by the parity gate (CD-4), not re-derived from first principles.
The engine exposes all three sets so each surface composes the same selection it
does today:

- **plan/run:** `Affected` = the existing changed-plan selection (parity).
- **cockpit:** shows `DirectlyChanged` (badged "changed") and `Dependents`
  (badged "affected"), and may list `Dependencies` — the Q2 view (`cli-surface.md`
  §1).
- **orun catalog affected:** emits `Affected` + the three sets + `Confidence`.

## 7. Non-goals

- Rewriting `expand.DependencyResolver` outside the changed path (other callers
  keep using it; only the `--changed` selection re-bases onto the catalog graph).
- The remote/edge consumer (`specs/orun-affected-worker/`, under review). The
  engine's inputs are content-addressed and a remote mirror is *possible*, but
  this spec adds nothing for it (`design.md` §7).
- Reverse-closure *materialization* — the engine walks the graph in-process
  (sub-millisecond at component scale).
