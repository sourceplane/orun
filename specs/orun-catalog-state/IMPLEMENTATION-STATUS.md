# Implementation Status — orun-catalog-state

> Live tracker for the CS1 → CS9 milestones (`implementation-plan.md`). Updated as
> each milestone lands.

| Milestone | Status | PR | Notes |
|-----------|--------|----|-------|
| CS1 — Lossless object-model catalog (`Path`) | **Done** | — | `nodes.ComponentIdentity.Path` added; mapped in `objplan/catalog.go:mapManifest`. One-time catalog-id change (below). |
| CS2 — `internal/objcatalog` read view | **Done** | — | `Reader.Load → CatalogView` (catalog + components + graph + tolerant `impact/`). Missing `impact/` → `Ownership == nil`. 92.8% coverage. |
| CS3 — ownership map + fingerprints | **Done** | — | Ownership map (`impact/ownership.json`) + the per-component virtual Merkle tree (`impact/fingerprints/<name>.json`). Fingerprints derived in `catalogresolve` over the candidate read-set, content-hashed, deterministic (clean→edit→clean returns the same subtree); folded into the catalog Merkle root. |
| CS4 — `internal/affected` engine | **Done** | — | Engine core + `GitChangeSource` (#251); `FingerprintChangeSource` (#252); catalog **watch-enrichment** — `spec.change.watches` is now a component.yaml field carried into the resolved manifest → node spec, so the engine's `watch` intent-impact reads real per-component data (optional/pointer ⇒ no hash churn for watch-less components). |
| CS5 — migrate `plan/run --changed` | **In progress** | — | PR1 (substrate): dependency-edge `include` mode carried through the catalog (component.yaml → manifest → graph edge → node → objcatalog), and the engine now computes `Result.Selection` = DirectlyChanged ∪ **include:always** forward closure — the parity-correct plan/run job set (vs `Affected` = cockpit blast radius). **PR2 (next):** wire `plan/run --changed` onto the engine + the parity gate, then remove the old `collectChangedComponents` path. |
| CS6 — cockpit read seam + drill-down + changed view | **In progress** | — | PR1 (#256): `viewmodel` catalog view-models (`CatalogView`/`ComponentRow` + Q2 overlay/filter, `ComponentView`). PR2: `internal/cockpit/catalogread` — the catalog data provider composing objcatalog + the engine's live `FingerprintChangeSource` overlay + the view-models (`CatalogView(withOverlay)`, `ComponentView(key)`). 94.7% coverage. **Next:** wire into the live TUI (`LoadWorkspace` freshness gate, refresh hook, ticker, drill-down, run action) — the interactive pieces. |
| CS7 — `orun catalog affected` | **Done** | — | New CLI: reads the object-model catalog, runs `affected.Detect` over `--base/--head/--files`, emits `CatalogAffectedResult` (the three sets + selection + confidence/needsFullResolve/intentMode + catalogId), text or `--json`. Exit 6 when no catalog/impact index. A no-parity-risk engine consumer (done before CS5 per the inline-vs-discovery decision). |
| CS8 — parity + determinism gate | Not started | — | |
| CS9 — `orun catalog refresh` repoint | Not started | — | |

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
