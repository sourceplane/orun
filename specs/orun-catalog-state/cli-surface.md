# CLI Surface

> Universal refresh, cockpit behavior (incl. the Q2 changed/affected view),
> `plan`/`run --changed` migration, the `orun catalog refresh` repoint, and the
> new `orun catalog affected` command (the `internal/affected` engine on the
> CLI). Envelope conventions follow `orun-component-catalog/cli-surface.md` §11
> (apiVersion/kind/data/warnings).

## 0. Universal refresh (every command)

Every `orun` invocation runs the freshness gate via a shared root pre-run hook
(`design.md` §3.5, D-8): on a clean/unchanged tree it is a free `SourceID`
compare; on a miss it full-resolves and write-throughs so `catalogs/current`
stays current as a side effect of using orun. The hook is **best-effort,
time-bounded, non-fatal, and debounced** (≤ one resolve per `refreshTTL` per
source). It is invisible (no output, no flag) unless `--verbose`, which logs the
refresh outcome to stderr. `plan`/`catalog refresh` resolve authoritatively and
the hook is a no-op for them. An escape hatch `ORUN_NO_AUTO_REFRESH=1` disables
the hook for scripted/perf-sensitive callers.

## 1. Cockpit (no new command)

`orun` (the TUI) Browse/components view renders from a **live in-memory
`CatalogView`** kept current by an interval ticker (`design.md` §3.5, D-9). No
flag, no visible command — the data source moves, the UI does not. Behavior:

- **Fresh** (`SourceID(current) == catalog.SourceID`): components render from the
  in-memory snapshot (sourced from `catalogs/current`), instantly.
- **Stale/absent**: the ticker full-resolves off the UI thread, swaps the
  in-memory snapshot, and writes through best-effort. A first-run empty `.orun`
  resolves and populates on open.
- **Interval:** the ticker re-runs the gate every `refreshInterval` (~2–5s), so
  edits and other processes' writes appear without a keystroke. `ctrl+r` forces
  an immediate tick.

**Changed / affected view (Q2 — in scope).** The cockpit calls
`internal/affected` with a `FingerprintChangeSource` (the virtual Merkle tree) to
overlay change state on the component list:

- components in `DirectlyChanged` are badged **"changed"**;
- components in `Dependents` are badged **"affected"**;
- a filter toggles "show only changed/affected" (the cockpit analogue of
  `orun component --changed`'s Changed/Dependencies/Dependents grouping).

This recomputes on the same ticker as the catalog refresh, so the overlay tracks
your uncommitted edits live. It uses the **same** engine as `plan`/`run
--changed` — one definition of affected (`change-detection.md`).

## 1b. `plan --changed` / `run --changed` — migrated onto the engine

Behavior and flags are **unchanged** (`--changed`, `--base`, `--head`, `--files`,
`--uncommitted`, `--untracked`, `--intent-impact`, `--explain`). Internally the
ad-hoc `cmd/orun/main.go` path (`collectChangedComponents` + `isPathChanged` +
`expand.DependencyResolver`) is replaced by a `GitChangeSource` +
`internal/affected.Detect`, and `--explain` renders `Result.Explain`. The
**selection is identical** — guarded by the parity gate (`test-plan.md`; the old
path is removed only after parity holds). `--intent-impact all/watch/none` and
per-component `Change.Watches` semantics are preserved verbatim.

The cockpit MAY show a `preview` badge when the rendered catalog came from a
dirty tree (the source `workingTree == "dirty"`), reusing the
authoritative/preview distinction the resolver already computes. (Gated on Q-2 —
confirm the changed-set UX is wanted before building it.)

## 2. `orun catalog refresh` — repoint (additive)

Today `refresh` writes only `internal/catalogstore`. It is extended to **also**
write the object-model catalog (so an explicit refresh, not just `orun plan`,
populates the cockpit's source and the impact index):

```
orun catalog refresh [--json] [--strict]
```

- Unchanged: resolves the current workspace, writes the `catalogstore` snapshot,
  prints the existing `CatalogRefreshResult` envelope.
- **Added:** after the `catalogstore` write, calls the object-model
  `RefreshCatalog` seam (source + catalog write + `catalogs/current` move +
  `impact/ownership.json`), reusing the **same** resolved `CatalogView` (no
  second resolve). Failures here are warnings appended to the envelope
  `warnings[]`, never a non-zero exit (mirrors `object_model_plan.go`'s
  best-effort posture).
- The envelope `data` gains no required field; an optional
  `data.objectModel: {catalogId, sourceId, components}` is added for parity with
  the text summary. Existing keys are byte-stable (golden tests unaffected).

## 3. `orun catalog affected` — NEW

The `internal/affected` engine on the CLI: given a base and a head (or an explicit
changed-file list), classify changes and emit the affected component set with a
confidence signal. Same engine as `plan`/`run --changed` and the cockpit overlay.
(It also serves as the conformance oracle for the under-review worker — but that
is incidental; this command exists for orun's own use.)

```
orun catalog affected
  [--catalog-source <sel>]      # which catalog to read (default: current; full-resolves if stale)
  [--base <ref>] [--head <ref>] # git range; default base = catalog source HEAD, head = working tree
  [--files <comma-list>]        # bypass git: explicit changed paths
  [--json]
```

Behavior (the CD-1…CD-4 contract, `change-detection.md` §5):

1. Resolve the catalog (the freshness gate; full-resolve if stale) and load its
   `impact/ownership.json` + `graph/dependencies.json`.
2. Build a `GitChangeSource` from `--base/--head/--files` (or the working-tree
   dirty set when head is the working tree).
3. Call `affected.Detect` — ownership classification → intent-impact →
   dependency closure (`change-detection.md` §2).
4. Emit the `Result` projection:

```json
{
  "apiVersion": "orun.dev/v1alpha1",
  "kind": "CatalogAffectedResult",
  "data": {
    "affected": ["sourceplane/orun/web", "sourceplane/orun/api-edge"],
    "directlyChanged": ["sourceplane/orun/web"],
    "dependents": ["sourceplane/orun/api-edge"],
    "confidence": "high",            // "high" | "low"
    "needsFullResolve": false,       // true on any structural/global uncertainty
    "intentMode": "none",            // "none" | "global" | "components"
    "catalogId": "sha256:…"
  },
  "warnings": []
}
```

**Contract surfacing in the CLI:**
- `confidence: "low"` + `needsFullResolve: true` is the CD-2 escalation signal
  (a `component.yaml` edit may have changed structure the loaded graph doesn't
  reflect). Since this command full-resolves the catalog first, the result is
  normally authoritative; the flag matters when reading a pinned/stale catalog
  via `--catalog-source`.
- On any classification ambiguity the command **over-reports** (CD-1): an
  unknown-owner changed path under a component dir resolves to that component; a
  parse failure on `intent.yaml` escalates to `global`.

Exit codes: `0` success; `2` resolver/internal; `3` state read failure; `6`
impact index absent (run refresh).

## 4. No changes

`orun catalog list/describe/tree/history/validate/diff/refs` are untouched (they
read `catalogstore`; convergence is cockpit-scoped — README / D-7). `orun plan`
already writes the object-model catalog; CS3 adds `impact/ownership.json` to that
write with no flag or surface change.
