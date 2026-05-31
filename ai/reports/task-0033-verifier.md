# Task 0033 — Verifier Report (PR #174)

**Subject:** PR #174 — `task-0032-catalogstore-c4-pr2` (Catalog Store C4 PR-2 — writer surface)
**Branch:** `task-0032-catalogstore-c4-pr2`
**Verdict:** **PASS (with verifier-attached coverage fix, path a)**

---

## Summary

PR #174 implements the writer surface for `internal/catalogstore` per
catalog-store.md §3 (steps A, B, C, D) and the task-0032 spec — refs,
global indexes, component events, sources/catalog snapshots, and the
seq.lock allocator. Behaviour is correct: every spec-required ordering
invariant is preserved, sentinel chains are intact (`ErrSourceMismatch`,
`ErrCatalogMismatch`, `ErrManifestMismatch`, `ErrInputsInconsistent`,
`ErrRefStale`, `ErrInvalidPathInput`, all wrapping the underlying
`statestore.ErrConflict` / `statestore.ErrExists` for `errors.Is`),
no raw FS imports leak into `internal/catalogstore/`, and adjacent
packages (statestore, revision, executionstate, catalogmodel,
sourcectx, catalogresolve) are green.

The single acceptance failure was coverage: the implementer landed at
**85.3 %**, below the **90 %** hard floor required for PR-2 (PR-1
baseline was 90.7 %; the writer surface added many defensive branches
without paired tests). Per task-0033 Outcome 11 path (a), the verifier
attached focused coverage-only tests on the PR branch rather than
bouncing the PR. Coverage is now **90.1 %** (484/537 statements,
+4.8 pp from baseline), clearing the floor.

## Coverage Delta

| Phase                                 | Coverage    | Statements |
| ------------------------------------- | ----------- | ---------- |
| Implementer baseline (HEAD before fix) | 85.3 %      | 458 / 537  |
| After verifier batch 1 (validation)   | 86.6 %      | 465 / 537  |
| After verifier batch 2 (error paths)  | 89.0 %      | 478 / 537  |
| **After verifier batch 3 (final)**    | **90.1 %**  | **484 / 537** |
| Hard floor                            | 90.0 %      | 484 / 537  |
| Original PR-1 baseline                | 90.7 %      | (n/a)      |

Per-file coverage summary (post-fix):

```
events.go           89.7 %
indexes.go          88.5 %
paths.go            ~93 %
refs.go             ~88 % (writeRefCAS 92.3 %)
store.go           100 %
writer.go           ~88 % (createOrReconcile 80 %, reconcileSourceBody 83 %)
```

## Verifier-Attached Fix

Two files modified on the PR branch (commit `bf608b2`):

1. **`internal/catalogstore/writer_test.go`** — Extended the existing
   `spyStore` (per the PR's "inline locally; PR-3 may extract storetest"
   convention at the top of the file) with four one-shot error-injection
   maps:
   - `readErr` — fail next `Read(p)` once with the supplied error.
   - `casErr` — fail next `CompareAndSwap(p, …)` once with the supplied
     error (used to drive non-`ErrConflict` CAS branches).
   - `createNStdE` — fail next `CreateIfAbsent(p, …)` once with a
     non-`ErrExists` error (used to drive the defensive
     "neither-Exists-nor-Conflict" arm).
   - `writeErr` — fail next `Write(p, …)` once with the supplied error.

   Original behaviours unchanged; existing tests pass.

2. **`internal/catalogstore/verifier_coverage_test.go`** — NEW (28
   focused tests, `package catalogstore_test`) covering:

   - **WriteGlobalIndexes**: `ValidateSourceKey` / `ValidateCatalogKey`
     rejection of malformed snapshot keys; component-key validation;
     pre-existing-identical-merge convergence (≤ 1 CAS); component
     `CreateIfAbsent` non-Exists error; component Read error; component
     decode error; component CAS non-Conflict error; Source/Catalog
     `Write` error paths (C.1 / C.2).
   - **WriteRefs**: scope arms previously unexercised — catalog-only
     (nil source closure short-circuit); Authoritative main (D.3); Branch
     (D.4); PR (D.5); branch sanitized-to-empty rejection;
     `CreateIfAbsent` non-Exists error; post-Exists Read error; CAS
     non-Conflict error.
   - **AppendComponentEvent**: malformed source/catalog snapshot key
     rejection; key-without-slashes rejection; allocator
     `CreateIfAbsent` non-Exists error; allocator CAS non-Conflict
     error; allocator Read error; corrupt seq.lock (next=0,
     unparseable JSON); body-path `CreateIfAbsent` failure.
   - **WriteCatalogSnapshot / WriteSourceSnapshot**: invalid manifest
     name (path-validation error); manifest `CreateIfAbsent` non-Exists
     error; manifest post-Exists Read error; source post-Exists Read
     error; `writeLocalIndexes` Write error.

## Assertions Verified

- `go vet ./...` — clean.
- `go build ./...` — clean.
- `go test -race -count=1 ./internal/catalogstore/...` — PASS, 90.1 % coverage.
- `go test -race -count=1 ./...` — PASS (all packages green, no race).
- `grep -RE '^\s*"(os|io/ioutil|path/filepath)"' internal/catalogstore/`
  — empty (no raw FS imports).
- `errors.Is` chain preservation — every test that matches a typed
  sentinel also passes (`statestore.ErrConflict`, `statestore.ErrExists`)
  where the source code documents dual wrapping.

## Branches Left Unexercised (Documented)

A small number of defensive branches remain at zero coverage. None
affect the behavioural contract; all are defended by upstream
validation that runs first:

- `paths.go` first-line `CatalogDir` error returns inside path
  builders (e.g., `ComponentLocalIndexPath`, `CatalogRevisionDir`) —
  unreachable through the public Store API because `WriteCatalogSnapshot`
  and `WriteGlobalIndexes` validate the keys upstream in
  `preflightCatalogInputs` / `Validate{Source,Catalog}Key`.
- `events.go:98` `componentKeyTail` "no slash present" return —
  unreachable because `ValidateComponentKey` rejects 1-segment keys
  before tail extraction.
- `writer.go:23,27` `WriteSourceSnapshot` path/encode error returns —
  guarded by `ValidateSourceKey` upstream; `PrettyEncode` cannot fail on
  a `SourceSnapshot` with a validated key.
- `refs.go:54-75, 81-129` `addSource` / `addCatalog` closures'
  `pathFn` and `PrettyEncode` error returns — `pathFn` is one of the
  fixed `SourceRefPath` / `CatalogRefPath` family operating on validated
  inputs; only the branch-sanitized-to-empty case is reachable, and that
  is covered.
- `refs.go:176-181` `writeRefCAS` re-Read-after-conflict error branch —
  symmetric to the post-Exists Read branch covered by
  `TestWriteRefs_PostExistsReadErrorSurfaces`. Exercising it from a
  black-box test would require timing hooks on the spy
  (`readErr`-after-N-calls). Branch left documented.
- `events.go:153-159` `json.Marshal(seqLockEnvelope)` encode error —
  `seqLockEnvelope` is a fixed-shape struct; Go's `json.Marshal` cannot
  fail on it.
- `writer.go:239-244` `writeLocalIndexes` `PrettyEncode(body)` error —
  `body` is `any` from a caller-supplied map; in practice the catalog
  resolver only feeds JSON-marshalable shapes.

These are the standard "defensive returns that can't be reached without
constructing an artificial failure on a stdlib helper" branches. Carrying
them at zero coverage rather than introducing white-box hooks keeps the
test suite using only the public `Store` interface.

## Spec Gaps Surfaced

None. The PR matches catalog-store.md §3 precisely; the implementer
report at `ai/reports/task-0032-implementer.md` is accurate.

## Commits

- `bf608b2` test(catalogstore): verifier-attached coverage fix for
  task-0032 (path a) — pushed to
  `origin/task-0032-catalogstore-c4-pr2`.
- Squash-merged into `main` as `73c6e8e1193f73e1562e5874a326f4d4478e3a30`
  at 2026-05-31T11:30:39Z.
- Post-merge main CI (`Orun Plan` run 26711390108, `CI` run
  26711390118, `state-redesign-tests` run 26711390118): all PASS.

## Final Verdict

**PASS — MERGED**. PR #174 is in `main`; main-branch CI is green.
Behaviour, ordering, sentinel chains, race safety, and no-raw-FS
guarantees all hold; coverage now clears the 90 % hard floor at
90.1 %.
