# Implementation Status — orun-catalog-state

> Live tracker for the CS1 → CS9 milestones (`implementation-plan.md`). Updated as
> each milestone lands.

| Milestone | Status | PR | Notes |
|-----------|--------|----|-------|
| CS1 — Lossless object-model catalog (`Path`) | **Done** | — | `nodes.ComponentIdentity.Path` added; mapped in `objplan/catalog.go:mapManifest`. One-time catalog-id change (below). |
| CS2 — `internal/objcatalog` read view | **Done** | — | `Reader.Load → CatalogView` (catalog + components + graph + tolerant `impact/`). Missing `impact/` → `Ownership == nil`. 92.8% coverage. |
| CS3 — ownership map + fingerprints | **Done** | — | Ownership map (`impact/ownership.json`) + the per-component virtual Merkle tree (`impact/fingerprints/<name>.json`). Fingerprints derived in `catalogresolve` over the candidate read-set, content-hashed, deterministic (clean→edit→clean returns the same subtree); folded into the catalog Merkle root. |
| CS4 — `internal/affected` engine | **In progress** | — | Engine core landed: `Detector`/`Result`, the ownership→intent→dependency-closure pipeline, `GitChangeSource`, intent classification (none/global/components) + intent-impact (all/watch/none) + structural detection. **Still to do:** `FingerprintChangeSource` (PR2) and the catalog **watch enrichment** so `watch` mode has real data (PR3, per your decision). |
| CS5 — migrate `plan/run --changed` | Not started | — | |
| CS6 — cockpit read seam + drill-down + changed view | Not started | — | |
| CS7 — `orun catalog affected` | Not started | — | |
| CS8 — parity + determinism gate | Not started | — | |
| CS9 — `orun catalog refresh` repoint | Not started | — | |

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
