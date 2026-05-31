# Task 0031 Verifier Report — PR #173 (C4 PR-1)

Result: PASS

## Summary
PR #173 (`feat(catalogstore): C4 PR-1 — paths, errors, Writer`) verified
against Task 0030 acceptance. All 12 Required Outcomes met. CI green
(4/4 SUCCESS), MERGEABLE/CLEAN, all coverage floors held byte-for-byte,
spy-based call-order test confirms B.1→B.2→B.3→B.4 ordering, double-wrap
sentinel pattern confirmed via `errors.Is` assertions on both target
chains, stub-pin test locks all PR-2/PR-3 deferrals to
`ErrNotImplemented`. Merged at `c2d7b9d` and branch deleted. Implementer
report relocated from `reports/` to canonical `ai/reports/` per Required
Outcome 11 (verifier-only fix path).

## Checks

### Build / Vet
    go vet ./...        — clean (no output, exit 0)
    go build ./...      — clean (no output, exit 0)
    go mod tidy         — no diff
    make verify-generated — "✅ generated artifacts up-to-date"

### Tests
    go test ./internal/catalogstore/... -race -count=1 -cover
        ok  github.com/sourceplane/orun/internal/catalogstore  1.775s
        coverage: 90.7% of statements    ← above 90 % floor

    go test ./... -race -count=1
        all packages ok (statestore 21.590s, executionstate 30.429s,
        revision 19.908s, catalogstore 1.775s, plus all Phase-1/Phase-2
        siblings — no FAIL anywhere)

### Coverage Floors (re-measured byte-for-byte)
    internal/statestore       95.7 %   (floor 95.7) ✓
    internal/revision         90.3 %   (floor 90.3) ✓
    internal/executionstate   90.0 %   (floor 90.0) ✓
    internal/catalogmodel     91.1 %   (floor 91.1) ✓
    internal/sourcectx        91.1 %   (floor 91.1) ✓
    internal/catalogresolve   90.9 %   (floor 90.9) ✓
    internal/catalogstore     90.7 %   (new floor 90) ✓

### Static Guards
    grep -rn '"os"|"io/ioutil"|"path/filepath"' internal/catalogstore/
        → no matches (exit 1) — no raw FS imports ✓

    PR diff names (gh pr diff 173 --name-only):
        internal/catalogstore/{doc,errors,paths,paths_test,store,
                               store_test,writer,writer_test}.go
        reports/task-0030-catalogstore-c4-pr1.md
    → eight package files + implementer report only. Forbidden files
      (refs.go, indexes.go, resolver.go) ABSENT. No churn outside
      `internal/catalogstore/`. ✓

### Kiox Guards
    kiox -- orun validate --intent intent.yaml      ✓ Intent valid
    kiox -- orun plan --changed                     2 components × 5 envs → 3 jobs
                                                    plan id: 046ae2d18919
    kiox -- orun run --plan plan.json --dry-run     3 selected, all ✓ green
    Not a no-op — plan resolved 3 jobs across admin-console-pages-git
    and docs-site-direct-upload. Dry-run preview clean.

## CI Log Review
    gh pr view 173 --json statusCheckRollup
        Orun Plan                        SUCCESS   COMPLETED
        Harness dry-run guard            SUCCESS   COMPLETED
        test                             SUCCESS   COMPLETED
        (matrix expansions)              SKIPPED   COMPLETED  (normal)
    Final state pre-merge: MERGEABLE / mergeStateStatus: CLEAN.

## Code Path Inspection (read, not just trusted)

1. `internal/catalogstore/writer.go::WriteCatalogSnapshot` (lines 67-149)
   - Pre-flight `preflightCatalogInputs(src, cat, manifests)` runs at
     line 70 BEFORE any `s.state.CreateIfAbsent` / `s.state.Write`
     issues. Confirmed by structural read AND by
     `TestWriteCatalogSnapshot_PreflightInconsistent_Cat*` /
     `_Manifest*` asserting `len(spy.trace) == 0` after
     `ErrInputsInconsistent` returns.
   - Graph write loop iterates `CatalogGraphKinds()` (line 104),
     not `range graphByKind`. Order-independent input order test at
     `writer_test.go:295-325` feeds graphs in randomised map order
     and asserts the trace has graph creates in fixed
     `dependencies, systems, apis, resources, owners` sequence.

2. `internal/catalogstore/store.go` (lines 148-151)
   - Compile-time interface assertions present:
        var _ Writer   = (*store)(nil)
        var _ Resolver = (*store)(nil)
        var _ Store    = (*store)(nil)
   - Stub policy: every `Resolver` method, plus `WriteRefs`,
     `WriteGlobalIndexes`, `AppendComponentEvent`, returns the
     typed `ErrNotImplemented`. Pinned by
     `TestStubsReturnErrNotImplemented` (store_test.go:16-37) which
     iterates 8 closures and asserts
     `errors.Is(err, catalogstore.ErrNotImplemented)`. A future
     accidental nil-return is an immediate test break.

3. `internal/catalogstore/errors.go` + `writer.go::reconcileSourceBody`
   / `createOrReconcile`
   - Mismatch sentinels are package-level `errors.New(...)` values,
     not wrappers — the wrapping happens at the return site:
        return fmt.Errorf("%w: %w", ErrSourceMismatch, origErr)
        // origErr is the statestore.ErrExists from CreateIfAbsent
   - Confirmed by `errors.Is(err, catalogstore.ErrSourceMismatch)`
     AND `errors.Is(err, statestore.ErrExists)` BOTH succeeding in
     the mismatch tests (writer_test.go:228-233, 345-351, 431-435).
     Negative case at writer_test.go:498-503 confirms the wrap is
     conditional on the `ErrExists` branch — non-`ErrExists` failure
     does NOT classify as `ErrSourceMismatch`.

4. Step B.4 (local indexes) — writer.go line 242 uses
   `s.state.Write(ctx, p, encoded, statestore.WriteOptions{})`
   directly (overwrite-OK), NOT `CreateIfAbsent`. The trace assertion
   at writer_test.go:286-290 confirms post-`catalog.json` ops are
   `write:` (Write), not `create:` (CreateIfAbsent), matching the
   spec's "rebuildable" contract.

5. Encoder choice — body writes use `catalogmodel.PrettyEncode`
   (confirmed via grep, not shown above). `CanonicalEncode` is not
   called anywhere in the writer, consistent with the implementer's
   stated reservation of canonical encoding for hashing in PR-2/PR-3.
   No hashing in PR-1; consistency held.

## Coverage Evidence
See "Coverage Floors" section above — every floor held to the tenth.
`internal/catalogstore` coverage 90.7 % under `-race -count=1` (the
implementer's claimed value reproduces exactly).

## Issues
None. The implementer report path deviation noted in Task 0031 was
resolved by the verifier per Required Outcome 11(a): the report at
`reports/task-0030-catalogstore-c4-pr1.md` was `git mv`d to
`ai/reports/task-0030-implementer.md` (canonical) on `main` after
merge. The 593-char stub previously at that path is overwritten with
the merged report's full content.

## Risk Notes
- C4 PR-2 will need to extract or duplicate the spy implementation in
  `writer_test.go` if PR-2 tests want order assertions on
  `WriteRefs` / `AppendComponentEvent`. Currently the spy is unexported
  inside `writer_test.go` and would need to be either lifted into a
  shared `internal/catalogstore/storetest` helper or duplicated. Flag
  for the implementer prompt.
- The graph kind whitelist in `ValidateGraphKind` is anchored to
  `CatalogGraphKinds()`. Any future graph-kind addition (e.g. a new
  edge type) must update both the model and the validator together;
  currently nothing forces them to stay in sync beyond this validator
  (no enum-style exhaustiveness check). Low risk while graphs are
  hand-written, worth a future lint.
- Coverage at 90.7 % leaves 0.7 % headroom over the floor. PR-2 should
  target >91 % to keep buffer; if it lands at 90.x it should hold by
  re-measure.

## Spec Proposals
_none_ — PR followed `catalog-store.md` §1-§6 exactly. No drift.

## Recommended Next Move
Task 0032 = C4 PR-2 implementer:
- Implement `WriteRefs` (refs/{current,latest,main,branches/<x>,prs/<n>})
  using `CompareAndSwap` per `catalog-store.md` §3.C.
- Implement `WriteGlobalIndexes` (top-level indexes/) per §3.D.
- Implement `AppendComponentEvent` with seq.lock retry-up-to-16 per
  §3.D's history-event contract.
- Add `ErrRefStale` (§6 error taxonomy).
- DO NOT touch `resolver.go` / fallback chain / `RebuildIndexes` —
  those belong to PR-3.
- New file scope: `internal/catalogstore/{refs.go, refs_test.go,
  indexes.go, indexes_test.go}` plus minor edits to `errors.go`
  (add `ErrRefStale`) and `store.go` (delete the three
  `ErrNotImplemented` stubs and update the stub-pin test).

## PR #173 Merge Status
- Merged: PR #173 squashed-and-merged at `c2d7b9d` on `main`.
- Branch `task-0030-catalogstore-c4-pr1` deleted at remote.
- `main` tip pre-bookkeeping: `c2d7b9d`. `git status --short` clean.
