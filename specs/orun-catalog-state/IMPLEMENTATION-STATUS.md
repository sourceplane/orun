# Implementation Status — orun-catalog-state

> Live tracker for the CS1 → CS9 milestones (`implementation-plan.md`). Updated as
> each milestone lands.

| Milestone | Status | PR | Notes |
|-----------|--------|----|-------|
| CS1 — Lossless object-model catalog (`Path`) | **Done** | — | `nodes.ComponentIdentity.Path` added; mapped in `objplan/catalog.go:mapManifest`. One-time catalog-id change (below). |
| CS2 — `internal/objcatalog` read view | Not started | — | |
| CS3 — ownership map + fingerprints | Not started | — | |
| CS4 — `internal/affected` engine | Not started | — | |
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
