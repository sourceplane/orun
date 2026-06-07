# Implementation Status — orun-catalog-state

> Live tracker for the CS1 → CS9 milestones (`implementation-plan.md`). Updated as
> each milestone lands.

| Milestone | Status | PR | Notes |
|-----------|--------|----|-------|
| CS1 — Lossless object-model catalog (`Path`) | **Done** | — | `nodes.ComponentIdentity.Path` added; mapped in `objplan/catalog.go:mapManifest`. One-time catalog-id change (below). |
| CS2 — `internal/objcatalog` read view | **Done** | — | `Reader.Load → CatalogView` (catalog + components + graph + tolerant `impact/`). Missing `impact/` → `Ownership == nil`. 92.8% coverage. |
| CS3 — ownership map + fingerprints | **Done** | — | Ownership map (`impact/ownership.json`) + the per-component virtual Merkle tree (`impact/fingerprints/<name>.json`). Fingerprints derived in `catalogresolve` over the candidate read-set, content-hashed, deterministic (clean→edit→clean returns the same subtree); folded into the catalog Merkle root. |
| CS4 — `internal/affected` engine | **Done** | — | Engine core + `GitChangeSource` (#251); `FingerprintChangeSource` (#252); catalog **watch-enrichment** — `spec.change.watches` is now a component.yaml field carried into the resolved manifest → node spec, so the engine's `watch` intent-impact reads real per-component data (optional/pointer ⇒ no hash churn for watch-less components). |
| CS5 — migrate `plan/run --changed` | **In progress** | — | PR1 (substrate): dependency-edge `include` mode carried through the catalog; engine `Result.Selection` = DirectlyChanged ∪ **include:always** forward closure. **PR2 (parity gate, done):** `cmd/orun/changed_parity_test.go` proves the engine's selection == the legacy `collectChangedComponents → ResolveComponentSet` over the same workspace + changed-files input, across include:always/if-selected, multi-change, **and intent.yaml changes under every intent-impact mode (watch/all/none)** — confirming `applyIntent` mirrors the legacy `git.DiffIntent`/`watchesIntersect` logic. **PR3 (live cutover, done):** `plan --changed` now routes through `engineChangedSelection` — refresh-if-needed (cheap memo hit otherwise) → load the full object-model catalog → `affected.Detect` → Selection mapped to names. Resilient: any failure (no object store / non-git / resolve error) falls back to the legacy selector, so `--changed` never hard-fails. `collectChangedComponents` retained as the fallback (its direct unit tests stay green); deleting it can follow once the engine path is proven in production. e2e test exercises the live engine path. |
| CS6 — cockpit read seam + drill-down + changed view | **In progress** | — | PR1 (#256): `viewmodel` catalog view-models (`CatalogView`/`ComponentRow` + Q2 overlay/filter, `ComponentView`). PR2: `internal/cockpit/catalogread` — the catalog data provider composing objcatalog + the engine's live `FingerprintChangeSource` overlay + the view-models (`CatalogView(withOverlay)`, `ComponentView(key)`). 94.7% coverage. PR3 (live-view ticker, done): the cockpit arms a `refreshInterval` ticker (D-9) in `Init` that silently re-runs `LoadWorkspace` off the UI thread (`workspaceRefreshedMsg`, best-effort — a failed background refresh keeps the current snapshot and never toggles the spinner), so local edits and other processes' writes (an external `orun plan`/`run`, the §0 hook) appear without a keystroke; `ctrl+r` still forces an immediate reload. **Next:** repoint `LoadWorkspace` onto the objcatalog freshness gate (read view-models from `catalogs/current`); drill-down; run action — the remaining interactive pieces. |
| CS7 — `orun catalog affected` | **Done** | — | New CLI: reads the object-model catalog, runs `affected.Detect` over `--base/--head/--files`, emits `CatalogAffectedResult` (the three sets + selection + confidence/needsFullResolve/intentMode + catalogId), text or `--json`. Exit 6 when no catalog/impact index. A no-parity-risk engine consumer (done before CS5 per the inline-vs-discovery decision). |
| CS8 — parity + determinism gate | Not started | — | |
| CS9 — `orun catalog refresh` repoint | **Done** | — | PR1 (done): `orun catalog refresh` now also writes the **object-model catalog** — new `objplan.RefreshCatalog` seam (source + catalog + `catalogs/current` move + `impact/ownership.json`), reusing the same resolved `CatalogView` (no second resolve, same memo as `Plan`). Best-effort: failures append to `warnings[]`, never a non-zero exit. Optional `data.objectModel: {catalogId, sourceId, components}` added (existing keys byte-stable). So an explicit refresh — not just `orun plan` — populates the full catalog + impact index. **PR2 (universal refresh hook §0, done):** a root `PersistentPreRunE` hook (`maybeAutoRefresh`) runs the freshness gate around any catalog-using command via a **shared `refreshObjectCatalog` seam** (Q-1 — same helper the `--changed` engine path now uses). It is invisible, **best-effort/non-fatal** (a refresh failure never blocks or fails the command), **time-bounded** (`autoRefreshTimeout`), and **debounced** to ≤ one resolve per `autoRefreshTTL` per workspace via a `cache/auto-refresh.json` marker (D-8). No-op for authoritative resolvers (`plan`, `catalog refresh`), for non-catalog commands, and for the bare cockpit (its own ticker, D-9). Escape hatch `ORUN_NO_AUTO_REFRESH=1`; `--verbose`/`ORUN_VERBOSE` logs the outcome to stderr. So `catalogs/current` stays fresh as a side effect of using orun. |

## Catalog unblock (inline components + dir fix)

The object catalog now ingests **inline `intent.yaml` components** alongside
discovered `component.yaml` files (catalogresolve), so the catalog's component set
matches the legacy `inline ∪ discovered` set the cockpit and `--changed` operate
on — removing the divergence that blocked the CS5 `--changed` rewire and the CS6
cockpit repoint. Discovered components win on a name collision; inline
`subscribe.environments` accepts both the string and `{name,profile}` map forms
(a decode failure there would otherwise break the whole intent load). A prior fix
also corrected the component-dir derivation to use `SourceFile` (real
`component.yaml` files omit `spec.path`), so the ownership map and fingerprints
actually populate.

## CS1 — the one-time catalog-id change

Adding `identity.path` to `nodes.ComponentManifest` (previously dropped on the way
into the object model) changes the manifest blob hash → the catalog Merkle id →
the `catalogs/current` target on the next resolve. This is expected and absorbed
by content-addressing:

- **No migration.** Old catalogs remain readable; `path` is optional on read and
  is empty for catalogs written before this change.
- The resolve memo (`cache/resolve/<srcId>-rv<n>.json`) misses **once** after the
  upgrade, then re-stabilizes on the new id.
- The parity test (CS8) only runs against freshly-written catalogs, so the id
  change does not regress it.

See `data-model.md` §4 for the full identity-impact note.

## CS3 — fingerprint hashing note (sound deviation)

`change-detection.md` §3 describes projecting committed-file fingerprints from
`git ls-tree` (the git blob sha, "no re-hashing") with a dirty overlay. The
initial implementation instead **content-hashes** each candidate file with
sha256 at resolve time. This is:

- **Internally consistent** — the cockpit's change source (CS6) recomputes the
  subtree the same way, so comparison still works; the choice of hash function is
  not externally constrained while there is no remote consumer.
- **Sound & deterministic** — `clean → edit → clean` returns the identical
  subtree (asserted in `fingerprint_test.go`); the subtree folds in the global
  intent digest so an intent change flips every component.
- **Bounded** — the read-set is the resolver's candidate set (`component.yaml` +
  inference candidates + `*.tf`), not the whole dir, keeping the `plan` hot path
  cheap.

The git-`ls-tree` projection optimization (cheaper on large committed trees)
remains a valid future refinement; it changes the hash values but not the
contract, and only matters for performance / a remote consumer.
