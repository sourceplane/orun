# Task 0035 — Verifier Report (PR #175)

**Subject:** PR #175 — `task-0034-catalogstore-c4-pr3-resolver` (Catalog Store C4 PR-3 — read-side Resolver + RebuildIndexes)
**Branch:** `task-0034-catalogstore-c4-pr3-resolver`
**Milestone:** C4 — `internal/catalogstore` Writer + Resolver atomic writes (**closes C4**)
**Verdict:** **PASS (with verifier-attached coverage fix, path a)**
**Merged:** squash → `main` commit `42eace5f7a475bbf48d82b9f1847031e25068c1f` on 2026-05-31T12:51:14Z; branch deleted.

---

## Summary

PR #175 ships the entire read side of `internal/catalogstore` per
`catalog-store.md` §4: all five Resolver methods (`ResolveCurrentSource`,
`ResolveSource`, `ResolveCatalog`, `ResolveComponent`,
`ResolveComponentLatest`) implementing the refs-first → walk-fallback →
typed-sentinel ladder, plus `RebuildIndexes` (added to the `Resolver`
interface) with a T-STORE-3 scrub-then-rebuild byte-identity test.

Behaviour is correct. The §4 fallback ladder resolves `current → latest →
main` in the documented order, returns the correct typed sentinels
(`ErrComponentNotFound` vs `ErrCatalogNotFound`) with intact `errors.Is`
chains, honours `ctx` cancellation, and surfaces non-`NotFound` read errors
rather than masking them as fallthrough. `RebuildIndexes` reconstructs the
local + global indexes from authoritative manifests deterministically (sorted
ascending by key, stable comparator tiebreaks), producing byte-identical
index documents across a scrub-then-rebuild cycle. No raw FS imports leak
into `internal/catalogstore/` (`os` / `io/ioutil` / `path/filepath` grep
empty); all body reads/writes route through the injected `statestore` seam.
Zero live `ErrNotImplemented` Resolver surfaces remain — the sentinel exists
only as a definition (`errors.go:59`), comments, and test pins.

The single acceptance failure was coverage: the implementer landed
`internal/catalogstore` at **88.9 %**, below the **90 %** hard floor / 91 %
target. `internal/catalogstore` has **no CI coverage gate**, so green CI did
NOT enforce the floor — the verifier re-measured and adjudicated by hand. The
gap was concentrated in the NEW `rebuild.go` bodies (72–79 %): reachable
walk/merge/write logic, not validated-input defensive returns. Per the
three-branch policy (mirroring the Task 0033 PR-2 rescue), this is path (a) —
attach focused coverage-only tests on the PR branch rather than bouncing back
to the implementer. Coverage is now **90.3 %**, clearing the floor with a
healthy margin (deliberately above the bare floor to avoid the documented
executionstate "flap problem").

## Coverage Delta

| Phase                                          | Coverage   |
| ---------------------------------------------- | ---------- |
| Implementer baseline (HEAD before fix)         | 88.9 %     |
| After verifier batch 1 (corrupt-skip/dedup/sort) | 89.5 %   |
| After verifier batch 2 (invalid-key surfaces)  | 89.8 %     |
| After verifier batch 3 (resolver pr/fallback)  | 90.2 %     |
| **After verifier batch 4 (read-error inject)** | **90.3 %** |
| Hard floor                                     | 90.0 %     |
| Target                                         | 91.0 %     |

Per-function highlights post-fix:

```
rebuild.go:  RebuildIndexes 93.5%, listAllSources 84.2%, collectAllCatalogs 85.2%,
             listManifestsForSource 81.8%, rebuildSourceGlobalIndex 81.8%,
             rebuildCatalogGlobalIndex 81.8%, writeComponentGlobalIndexPlain 81.8%,
             buildComponentGlobalIndexShard 100%, sortSourcesByKey 100%, sortCatalogsByKey 100%
resolver.go: ResolveCurrentSource/ResolveSource/ResolveCatalog/readSourceByKey 100%,
             ResolveComponent 94.1%, ResolveComponentLatest 91.7%,
             sourceRefPathForSelector/catalogRefPathForSelector 92.3%,
             readCatalogByKeys 83.3%, fallbackMostRecentSource 87.5%,
             fallbackMostRecentCatalog 84.6%
```

## Verifier-Attached Fix

Two NEW test files added on the PR branch (commit `cb563be`,
`package catalogstore_test`, mirroring the existing `spyStore` / `make*`
harness conventions — no production code changed):

1. **`internal/catalogstore/rebuild_coverage_test.go`** (321 lines) — rebuild
   branch coverage:
   - corrupt `source.json` skip; corrupt `catalog.json` skip;
   - shared-`catalogKey` dedup (seen-set) across multiple sources;
   - merge-order comparator tiebreaks (same `createdAt` / different source;
     same source / different catalog);
   - invalid source/catalog/component key surfaces (via a `writeRaw` helper
     that seeds structurally-valid JSON at well-formed paths whose embedded
     key fails `Validate*Key` at rebuild time, exercising the
     `rebuildSourceGlobalIndex` / `rebuildCatalogGlobalIndex` /
     `writeComponentGlobalIndexPlain` error arms).

2. **`internal/catalogstore/resolver_coverage_test.go`** (193 lines) —
   resolver branch coverage:
   - PR-selector arms (present / absent / missing-PR error);
   - §4 fallback-walk skip branches (non-matching entries, corrupt bodies,
     no-source → `CatalogNotFound`, source-but-no-catalog → `CatalogNotFound`,
     most-recent pick);
   - non-`NotFound` read-error injection on `ResolveComponentLatest`,
     `ResolveComponent`, `ResolveCatalog` (error surfaced, not swallowed).

All new tests pass under `-race` and were verified with `-v` not to be
silently early-returning. Only the two test files were staged (orchestrator
state artifacts left untouched/uncommitted per scope).

## Assertions Verified

- `go vet ./...` — clean (VET_EXIT=0).
- `go build ./...` — clean.
- `go test -race ./internal/catalogstore/...` — PASS, **90.3 %** coverage.
- `go test -race -count=1 ./...` — PASS (all packages green, no race).
- `make verify-generated` — "✅ generated artifacts up-to-date".
- `grep -RE '^\s*"(os|io/ioutil|path/filepath)"' internal/catalogstore/`
  — empty (no raw FS imports).
- Adjacent floors held: statestore, revision, executionstate, catalogmodel,
  sourcectx, catalogresolve — all green (no regression).
- T-STORE-3 scrub-then-rebuild byte-identity test present and green across all
  three index kinds.

## Adjudications

- **RefSelector.Snapshot — ACCEPT-AND-DOCUMENT.** The spec
  (`catalog-store.md` lines 45–46) reserves `Snapshot` for the C8 diff command
  ("Implementations may add Snapshot…"). The implementer wired it through
  `readSourceByKey`, a spec-permitted permissive superset. Not a defect; not
  flagged for change.
- **Three open implementer questions:**
  1. *Test-side CGI (component-global-index) helper duplication* — harness
     hygiene only; no contract impact. No action.
  2. *Rebuild does not scrub stale index files* — most likely belongs to C8
     (`catalogdiff`/`validate`/`rebuild`); RebuildIndexes here rewrites the
     authoritative set deterministically. Carried to C8, no proposal filed.
  3. *Silent corrupt-manifest skip* — acceptable for a rebuild that must make
     progress over a partially-corrupt store; the skip is now covered by
     attached tests. No proposal filed.
- **Retry-budget 8→16 drift** — remains non-escalated (advisory wording,
  harmless single-writer Phase 2; revisit at C9 / Phase 3).

## Branches Left Unexercised (Documented)

After the attached tests, residual uncovered arms in
`rebuildSourceGlobalIndex` / `rebuildCatalogGlobalIndex` /
`writeComponentGlobalIndexPlain` (now 81.8 %) are genuinely-unreachable
defensive returns: path-builder errors on already-validated keys, and
`PrettyEncode` errors on guaranteed-serializable structs. These are the
standard "defensive returns that can't be reached without constructing an
artificial failure on a stdlib helper" branches; carried at zero coverage
rather than introducing white-box hooks, keeping the suite on the public
interface.

## Carry-Forward

- **R-008 (`internal/executionstate` zero-margin floor flap).** On the PR #175
  `test` job the package measured 89.6 % vs its 90.0 % floor and flapped red
  once; a rerun went green. Byte-identical to main, untouched by C4. Recorded
  as R-008 in `ai/context/open-risks.md` — do NOT fix inside a catalog PR;
  scope a standalone micro-task only if it recurs a third time.

## Commits

- `cb563be` test(catalogstore): lift coverage to 90.3 % with verifier-attached
  rebuild/resolver tests (path a) — pushed to
  `origin/task-0034-catalogstore-c4-pr3-resolver` (`477b882..cb563be`).
- Squash-merged into `main` as
  `42eace5f7a475bbf48d82b9f1847031e25068c1f` at 2026-05-31T12:51:14Z; branch
  deleted.
- PR CI on `cb563be`: `test` (state-redesign-tests) PASS, `Orun Plan` PASS,
  `Harness dry-run guard` PASS, `orun remote-state conformance` PASS; matrix
  legs skipping legitimately (empty matrix). mergeStateStatus CLEAN.

## Final Verdict

**PASS — MERGED.** PR #175 is in `main`; behaviour, §4 fallback-ladder
ordering, typed-sentinel `errors.Is` chains, ctx cancellation, T-STORE-3
byte-identity, race safety, and no-raw-FS guarantees all hold; coverage now
clears the 90 % hard floor at **90.3 %**. **Milestone C4 CLOSES.** C5
(`orun catalog *` CLI) becomes the active milestone.
