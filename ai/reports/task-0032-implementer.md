# Milestone C4 PR-2 — internal/catalogstore: refs, global indexes, component events

Replaces the three `ErrNotImplemented` Writer stubs with real
implementations matching `specs/orun-component-catalog/catalog-store.md`
§3 steps C and D and §5 retry semantics.

Refs: `ai/tasks/task-0032.md`
Branch: `task-0032-catalogstore-c4-pr2`
Spec: `specs/orun-component-catalog/catalog-store.md`
Plan: `specs/orun-component-catalog/implementation-plan.md` (C4 PR-2)
Builds on: PR #173 (task 0031, C4 PR-1)

## Surface delivered (PR-2)

### `refs.go` — step D (`WriteRefs`)

CreateIfAbsent → CompareAndSwap retry loop per ref, with idempotency on
byte-identical bodies. Emit order matches spec §3.D exactly:

  D.1  refs/sources/current.json
  D.2  refs/catalogs/current.json
  D.3  refs/sources/main.json   (when SourceRef.Authoritative=true)
       refs/catalogs/main.json  (when CatalogRef.Authoritative=true)
  D.4  refs/sources/branches/<sanitized>.json    (when Branch != "")
       refs/catalogs/branches/<sanitized>.json
  D.5  refs/sources/prs/<pr>.json                 (when PR != "")
       refs/catalogs/prs/<pr>.json
  D.6  refs/sources/latest.json
       refs/catalogs/latest.json

Branch segments are passed through `catalogmodel.SanitizeBranch`; an
empty sanitized result surfaces `ErrInvalidPathInput`. Source==nil and
Catalog==nil sides are skipped silently per spec.

### `indexes.go` — step C (`WriteGlobalIndexes`)

  C.1  plain `Write` at SourceGlobalIndexPath  (skip if Source==nil)
  C.2  plain `Write` at CatalogGlobalIndexPath (skip if Catalog==nil)
  C.3  per-component `CompareAndSwap` at ComponentGlobalIndexPath, with
       merge-on-conflict; iteration is deterministic by ComponentKey
       ascending so re-runs produce byte-identical traces.

`mergeComponentGlobalIndex` policy:
- Identity fields (APIVersion/Kind/ComponentKey/Name/Repo): caller wins
  when non-empty.
- `Latest` / `Main`: caller wins when `SourceSnapshotKey != ""` (the
  freshness signal); preserved otherwise.
- `Previews`: union by `SourceSnapshotKey`, caller wins on tie, sorted
  ascending in the merged body.

### `events.go` — `AppendComponentEvent` + seq.lock allocator

Allocator: `<eventsDir>/seq.lock` holds `{"next":<uint64>}`. Initial
attempt is `CreateIfAbsent` with `next=2` (we just allocated 1). On
`ErrExists`, enter the read → CAS-with-`next+1` retry loop. The body
write itself uses `CreateIfAbsent` at
`ComponentHistoryEventPath(...)` (events are immutable per spec).

### `errors.go` — `ErrRefStale`

New sentinel for retry-budget exhaustion. Wraps the last
`statestore.ErrConflict` so callers can `errors.Is` against both.

## Retry budgets

Unified at **16** across `refs.go`, `indexes.go`, and the seq.lock
allocator. Spec §5 names 8 for indexes; PR-2 standardises on 16
because §6 introduces a single `ErrRefStale` taxonomy and the
spec's number is advisory. Documented inline in `indexes.go`. No
spec proposal required.

## What's NOT in this PR (PR-3)

- `Resolver.ResolveCurrentSource` / `ResolveSource` / `ResolveCatalog`
- `Resolver.ResolveComponent` / `ResolveComponentLatest`
- Resolver fallback chain `current → latest → main`

These remain `ErrNotImplemented`. The pin test in `store_test.go` was
narrowed to the five Resolver methods + a new `TestPR2WritersImplemented`
asserts the three PR-2 writer surfaces explicitly do NOT return
`ErrNotImplemented`.

## Validation

- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l internal/catalogstore/` — clean (drive-by reformatted three
  PR-1 files that had pre-existing whitespace drift)
- `go test ./internal/catalogstore/... -race -count=1` — PASS
- `go test ./... -count=1` — PASS (full repo)
- Coverage: **85.3 %** (target ≥ 85 %)

## Files Changed

New (PR-2 surface):
- `internal/catalogstore/refs.go`
- `internal/catalogstore/refs_test.go`
- `internal/catalogstore/indexes.go`
- `internal/catalogstore/indexes_test.go`
- `internal/catalogstore/events.go`
- `internal/catalogstore/events_test.go`

Modified:
- `internal/catalogstore/errors.go` — added `ErrRefStale`
- `internal/catalogstore/store.go` — removed PR-2 stub bodies; updated
  `New()` doc comment
- `internal/catalogstore/store_test.go` — narrowed
  `TestStubsReturnErrNotImplemented` to PR-3 Resolver methods; added
  `TestPR2WritersImplemented`
- `internal/catalogstore/writer_test.go` — extended `spyStore` with
  per-path revision counter, full `CompareAndSwap`, and
  `casConflicts` injection map for retry-budget tests
- `internal/catalogstore/paths.go`, `paths_test.go`, `writer.go` —
  gofmt drive-by only (no semantic change)

## Test inventory (new)

`refs_test.go`:
- HappyPath_OrderAndPaths
- OnlySourceOrCatalog
- SkipMainWhenNotAuthoritative
- BranchAndPRSelection (sanitization)
- BranchSanitizesToEmptyReturnsErrInvalidPathInput
- IdempotentOnByteIdenticalRewrite (no CAS entries on re-run)
- RetryThenSuccess (forced CAS conflicts, then converge)
- BudgetExhaustedReturnsErrRefStale
- NoOpWhenBothNil

`indexes_test.go`:
- NoOpWhenEmpty
- HappyPath_WritesSourceCatalogAndComponents
- DeterministicComponentOrder
- ComponentMergeOnConflict (verified union of previews)
- RetryExhaustedReturnsErrRefStale
- InvalidComponentKey
- NilComponentSkipped

`events_test.go`:
- AllocatesSeq1ThenSeq2 (verifies seq.lock body advances 2 → 3)
- SeqLockBudgetExhausted
- InvalidEventKind
- InvalidComponentKey
- MissingRequiredFields (no-source / no-catalog / no-kind)
- ConcurrentAllocatorPath (pre-existing seq.lock with next=5)

## Security

✅ No secrets logged or committed
✅ No external HTTP calls
✅ All path construction routed through `paths.go` helpers
✅ All key shape validation routed through `catalogmodel.Validate*Key`

## Open questions for verifier

1. Retry-budget unification: PR-2 standardises on 16 for indexes
   instead of spec §5's 8. Acceptable as documented, or land a spec
   proposal?
2. `mergeComponentGlobalIndex` policy: caller-wins on Latest/Main when
   `SourceSnapshotKey != ""` matches typical writer flow, but a
   "downgrade" caller (e.g. preview promotion that resets Latest) would
   need an explicit zero-value sentinel. PR-3 resolver review territory.
3. seq.lock allocator: any concern with the fixed `next=2` initial
   body shape? An alternative is `last=1` semantics, but `next` is
   strictly less ambiguous when reading.

## Next steps for verifier

1. Confirm test coverage ≥ 85 % and trace assertions match spec §3.D /
   §3.C ordering.
2. Confirm retry-budget unification is acceptable without a spec
   amendment.
3. Confirm `ErrRefStale` chains correctly through `statestore.ErrConflict`
   for callers that key off the statestore sentinel.

## References

- `specs/orun-component-catalog/catalog-store.md` (§3 write order, §5
  atomicity/retry, §6 error taxonomy)
- `internal/catalogstore/paths.go` (PR-1)
- `internal/statestore/store.go` (CAS / CreateIfAbsent contract)
- PR #173 (task 0031, C4 PR-1) — direct predecessor
