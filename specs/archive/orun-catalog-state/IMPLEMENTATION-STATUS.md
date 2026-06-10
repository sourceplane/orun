# Implementation Status — orun-catalog-state

> Live tracker for the CS1 → CS9 milestones (`implementation-plan.md`). Updated as
> each milestone lands.

| Milestone | Status | PR | Notes |
|-----------|--------|----|-------|
| CS1 — Lossless object-model catalog (`Path`) | **Done** | — | `nodes.ComponentIdentity.Path` added; mapped in `objplan/catalog.go:mapManifest`. One-time catalog-id change (below). |
| CS2 — `internal/objcatalog` read view | **Done** | — | `Reader.Load → CatalogView` (catalog + components + graph + tolerant `impact/`). Missing `impact/` → `Ownership == nil`. 92.8% coverage. |
| CS3 — ownership map + fingerprints | **Done** | — | Ownership map (`impact/ownership.json`) + the per-component virtual Merkle tree (`impact/fingerprints/<name>.json`). Fingerprints derived in `catalogresolve` over the candidate read-set, content-hashed, deterministic (clean→edit→clean returns the same subtree); folded into the catalog Merkle root. |
| CS4 — `internal/affected` engine | **Done** | — | Engine core + `GitChangeSource` (#251); `FingerprintChangeSource` (#252); catalog **watch-enrichment** — `spec.change.watches` is now a component.yaml field carried into the resolved manifest → node spec, so the engine's `watch` intent-impact reads real per-component data (optional/pointer ⇒ no hash churn for watch-less components). |
| CS5 — migrate `plan/run --changed` | **Done** | — | PR1 (substrate): dependency-edge `include` mode carried through the catalog; engine `Result.Selection` = DirectlyChanged ∪ **include:always** forward closure. **PR2-gate (CS8):** `cmd/orun/changed_parity_test.go` locked the engine selection against the legacy `collectChangedComponents → ResolveComponentSet` over include:always/if-selected, multi-change, intent-impact watch/all/none, and nested dirs. **PR3 (live cutover):** `plan --changed` routed through `engineChangedSelection` (refresh-if-needed → load the full object-model catalog → `affected.Detect` → Selection mapped to names). **PR2-removal (legacy path retired, done):** with the CS8 parity + determinism gate green, the engine is now the **single** `--changed` selection path. The legacy fallback in `plan`/`run --changed` is gone (an engine error now surfaces instead of silently diverging), `orun components --changed` (`listComponents`) routes through the same `engineChangedSelection`, and the entire legacy file-walking selector cluster is deleted: `collectChangedComponents`, `changedFilesSet`, `instancesFromMergedComponents`, `semanticIntentDiff`, `printIntentDiffExplanation`, `watchesIntersect`, and the `isPathChanged`/`isFileChanged`/`isIntentPathChanged`/path helpers — along with their now-obsolete `changed_selection_test.go` + `changed_filter_test.go`. The parity gate was converted from engine-vs-legacy to **golden-based** (the captured selections it had proven), so it keeps guarding the engine without the retired oracle. `--explain` ref-resolution output (`printExplainInfo`) is unaffected; the intent-diff sub-output never fired on the engine (primary) path, so its removal is not a user-facing change. |
| CS6 — cockpit read seam + drill-down + changed view | **Done** | — | PR1 (#256): `viewmodel` catalog view-models (`CatalogView`/`ComponentRow` + Q2 overlay/filter, `ComponentView`). PR2: `internal/cockpit/catalogread` — the catalog data provider composing objcatalog + the engine's live `FingerprintChangeSource` overlay + the view-models. 94.7% coverage. PR3 (live-view ticker, done): the cockpit arms a `refreshInterval` ticker (D-9) in `Init` that silently re-runs `LoadWorkspace` off the UI thread (`workspaceRefreshedMsg`, best-effort), so local edits and other processes' writes appear without a keystroke; `ctrl+r` still forces an immediate reload. PR4 (read-side freshness gate, done): `LoadWorkspace` serves the component list from the object-model catalog at `catalogs/current` when it is **fresh** for the workspace (`services.freshCatalogComponents`, design.md §3.1); a clean tree reads from state, a dirty/changed/absent one **falls back to the live intent loader** so uncommitted edits show immediately. PR5 (drill-down data layer, done): the component detail surfaces `spec.change.watches` (`ComponentSummary.Watches`); the component→executions join (§5/G-1) is served by scan+filter over `RunSummary.Components`. **PR6 (changed/affected overlay wired, done):** `LoadWorkspace` now annotates whichever component list it renders with the Q2 overlay via `services.catalogChangeOverlay` (composing `catalogread` → `internal/affected`), setting `ComponentSummary.Changed`/`ChangeKind` (`changed` = directly-changed, `affected` = transitive dependent). Computed independently of the freshness gate so a **dirty tree's edits surface as badges** on the live-loader list; best-effort (absent store / detection error ⇒ no badges, never a failure). Browse renders distinct changed/affected dots and a **show-only-changed filter** (`c`), composable with `/` search. **PR7 (drill-down navigation + env selector + run action, done):** `enter` on a Browse row opens a dedicated **component page** (`internal/tui/views/component_page.go`, `ModeComponent`) — detail (path/envs/profile/deps/watches + change badge) plus a **Recent executions** section (the §5 join, newest-first); `enter` on an execution hands off to the Activity `run→job→logs` drilldown (`ActivityModel.FocusRun`), completing catalog→component→job→logs from the state store. A cockpit **selected-env** (environments.md §1) is cycled with `e` (catalog/component surfaces), defaults to the first env / last-used (`prefs.SelectedEnv`), and shows in the header. `r` on the component page launches a **component-scoped run for the selected env** (only when active-in-env) via the existing generate→confirm→`objrun` path (`runAfterGenerate`); `g` still opens the composer. The single-env *redesign* remains the separate `orun-env-scoping` epic (L-1). Q-9 (overlay env-filtering) left as a non-blocking UX refinement. |
| CS7 — `orun catalog affected` | **Done** | — | New CLI: reads the object-model catalog, runs `affected.Detect` over `--base/--head/--files`, emits `CatalogAffectedResult` (the three sets + selection + confidence/needsFullResolve/intentMode + catalogId), text or `--json`. Exit 6 when no catalog/impact index. A no-parity-risk engine consumer (done before CS5 per the inline-vs-discovery decision). |
| CS8 — parity + determinism gate | **Done** | — | The three gates that lock the migration (test-plan.md §2/§4/§5). **Selection parity (PG-1/PG-2, `cmd/orun/changed_parity_test.go`):** the engine's `--changed` selection == the legacy `collectChangedComponents → ResolveComponentSet` over a multi-component fixture, across include:always/if-selected, multi-change, intent-impact watch/all/none, **and nested component dirs** (a deeply-nested file maps to its owner by longest-prefix on both paths). **Catalog parity guard (CG-1/CG-2, `internal/tui/services/catalog_parity_test.go`):** the cockpit's graph-path `[]ComponentSummary` (`freshCatalogComponents`, served from the object-model catalog) equals the intent-path summaries (`componentSummaries`, live loader) **field-for-field** (name/type/domain/path/envs/profile/dependsOn/watches) over a workspace with nested dirs, input files, dependency edges, and a multi-env subscription — the standing guard for the lossy-graph "dropped Path" class (S-6). Building it surfaced two representational splits the cockpit would otherwise render inconsistently across the freshness boundary, now **aligned in the graph mapping**: `Path` reduced to the component dir (`componentDir`, matching `loader.defaultComponentPath`) and `DependsOn` mapped from dependency keys back to component names (`dependencyNames`). **Determinism (P-2, `internal/objplan/catalog_determinism_test.go`):** `AssembleCatalog` produces a byte-identical catalog Merkle id across 128 random component/fingerprint orderings — proving `impact/ownership.json` + `impact/fingerprints/` (and every artifact under the root) are order-insensitive. **PG-3 (`--explain`)** holds by construction: the migrated `--changed` reuses the same ref-resolution/intent-diff printer (`printExplainInfo`/`semanticIntentDiff`), untouched by the engine cutover. **P-7** (fingerprint clean→dirty→clean) remains covered in `catalogresolve/fingerprint_test.go`. With the gate green, CS5's legacy-path removal (PR2) is unblocked. |
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
