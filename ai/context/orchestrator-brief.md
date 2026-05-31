# Orchestrator Brief — Cycle 6

## Cache Fingerprint
- generated_at: 2026-05-31
- cycle_seq: 6
- head_sha: c2d7b9d72ddc33fc86920c3ed06227ddb8f94123
- state_json_sha256: 56e2d46ddc1e075b8cb2e7db34b8045b2b8c7c4d3d173378cfc23c70ef2618bf
- merged_pr_count: 167 (gh pr list --state merged --limit 200 | wc -l)
- open_pr_count: 0
- worktree_dirty: ai/ only (this brief + state.json + current.md +
  task-ledger.md + ai/tasks/task-0032.md + ai/reports/task-0031-verifier.md
  + ai/reports/task-0030-implementer.md (relocated from reports/))

If any value above no longer matches the live repo at the start of the
next cycle, treat this brief as stale and re-derive from `ai/state.json`
+ `git log` + `gh pr list`.

## Where We Are
Phase 2 (`specs/orun-component-catalog/`), Milestone C4. PR-1
(Task 0030 / PR #173 / squash `c2d7b9d`) merged 2026-05-31. The
`internal/catalogstore` package is online with paths, errors, and the
body writer (steps A + B from `catalog-store.md` §3). Three Writer
methods remain stubbed with typed `ErrNotImplemented` and are the
exact subject of PR-2.

`Resolver` interface is also stubbed (5 methods) and stays stubbed
through PR-2 — those bodies are PR-3 scope.

## What Just Happened (Cycle 5 + 6 combined)
- Cycle 5: Task 0030 implementer pushed PR #173. Orchestrator scoped
  Task 0031 (verifier on PR #173). Single bookkeeping commit on `main`
  (`af2a67d`).
- Cycle 6 (this cycle): Verifier executed Task 0031 inline.
  Re-ran all 12 Required Outcomes:
  - PR boundary: 8 `internal/catalogstore/` files only ✅
  - No raw FS imports ✅ (grep returns 0 matches)
  - `go vet ./...`, `go build ./...`, `go test ./... -race -count=1`,
    `make verify-generated` all green ✅
  - `internal/catalogstore` coverage 90.7 % under `-race -count=1` ✅
  - Phase 1 floors held byte-for-byte (statestore 95.7, revision 90.3,
    executionstate 90.0) ✅
  - Phase 2 floors held (catalogmodel 91.1, sourcectx 91.1,
    catalogresolve 90.9) ✅
  - Code-path inspection: `CatalogGraphKinds()` drives B.2 ordering;
    spy-order test asserts B.1→B.2→B.3→B.4 with fixed graph order;
    pre-flight `ErrInputsInconsistent` exercised for cat↔src,
    manifest↔src, manifest↔cat shapes (no writes before fail);
    double-wrap mismatch sentinels chain `errors.Is` to BOTH typed
    sentinel and `statestore.ErrExists` ✅
  - Stub-pin test locks all 8 deferred surfaces (3 Writer + 5 Resolver) ✅
  - B.4 uses plain `Write` not CAS (matches spec — local indexes are
    rebuildable) ✅
  - PR #173 was MERGEABLE/CLEAN; kiox guards green; merged via
    `gh pr merge 173 --squash --delete-branch`.
- Verifier-only fix applied post-merge: relocated implementer report
  from non-canonical `reports/task-0030-catalogstore-c4-pr1.md` to
  `ai/reports/task-0030-implementer.md` and removed the stray
  `reports/` directory.
- Cycle 6 (continued): Orchestrator scoped Task 0032 (C4 PR-2
  implementer). State files updated; bookkeeping commit pending.

## Open Questions / Unresolved
None blocking. PR-2 implementer may surface a minor question about
which sentinel name to use for retry-exhausted (single `ErrRefStale`
covering refs + global-index + event-allocator, vs split sentinels).
The Task 0032 prompt allows either — must be documented in the report.

## Next Cycle Hypothesis
Implementer ships Task 0032 → opens PR (likely #174) on branch
`task-0030-catalogstore-c4-pr2` (or similar). PR-2 surface:

- New: `refs.go`, `refs_test.go`, `indexes.go`, `indexes_test.go`,
  `events.go`, `events_test.go`.
- Edited: `errors.go` (+`ErrRefStale`), `store.go` (replace 3 stubs),
  `store_test.go` (drop 3 from stub-pin), `writer_test.go` (extend
  spy `CompareAndSwap`).

Coverage on `internal/catalogstore` should rise from 90.7 % to ≥ 91 %.

If PR opens green and MERGEABLE/CLEAN, cycle 7 = Task 0033 verifier.
If PR fails or stalls, cycle 7 = Task 0033 = remediation note in the
same PR (don't open a new PR).

After PR-2 is merged, cycle 8 will scope Task 0034 (C4 PR-3
implementer — `resolver.go` reader fallback chain
`current → latest → main`, all 5 Resolver methods, `RebuildIndexes`).
That closes C4 and unlocks C5 (CLI surface).

## Hand-off Pointers
- Active task agent: `ai/tasks/task-0032.md` (implementer prompt).
- Spec sources: `specs/orun-component-catalog/catalog-store.md` §3.C,
  §3.D, §6.
- PR-2 baseline: `internal/catalogstore/store.go` (frozen `RefUpdate`
  / `GlobalIndexUpdate` shapes), `writer.go` (`createOrReconcile`
  pattern, `preflightCatalogInputs`, encoder choice — `PrettyEncode`
  for body writes), `errors.go` (taxonomy with double-wrap pattern).

## Working Notes for Next Orchestrator Cycle
1. Warm-boot: read this brief, `ai/state.json`, `ai/context/current.md`
   top-of-file, last entry of `ai/context/task-ledger.md`. Skill:
   `orun-saas-orchestration`.
2. Fingerprint check: HEAD should be ≥ `c2d7b9d` plus this cycle's
   bookkeeping commit. If `ai/state.json` sha matches the value above,
   no surprise mutations occurred.
3. Expected state: 0 open PRs (between cycles) OR 1 open PR if
   implementer has pushed Task 0032's PR. If 1 open PR ready for
   verification, scope Task 0033 verifier.
4. If implementer has not yet pushed (still working): no orchestrator
   action this cycle. Brief becomes a no-op observation.

## Risk Notes
- `internal/catalogstore` floor of 90 % has only 0.7 % buffer. PR-2
  must add net coverage to keep buffer ≥ 1 % for PR-3 headroom.
  Task 0032 prompt enforces ≥ 91 %.
- Spy `CompareAndSwap` extension is the most error-prone part of PR-2
  test plumbing. Implementer prompt explicitly requires
  `cas:<path>:<oldRev>` trace entries and conflict-injection, with
  byte-identical-trace determinism test.
- The seq.lock allocator path is not pinned by spec to an exact
  filename — Task 0032 prompt suggests
  `<srcKey>/<catKey>/history/components/<name>/events/seq.lock`
  (natural co-location with events). If implementer disagrees, they
  must document in the report; verifier confirms.