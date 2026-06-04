# Object-model — post-M12 follow-ups

The **M0–M13 roadmap is complete** (see `IMPLEMENTATION-STATUS.md`): the
content-addressed object model is the sole execution representation, the legacy
`internal/state` file store is deleted, and the coexistence flags are removed.

This file records the **optional, deferred** work that remains. None of it is
required for correctness — the object model is fully functional without it.
Each item is independent; pick by value, not order.

---

## 1. Resume — carry step logs forward (small)

**What.** Cross-run resume (`feat(run): cross-run skip-completed resume`, #244)
skips jobs that already succeeded in a prior run of the same `--exec-id`. Today
the resumed jobs carry their **statuses** (and step statuses) into the new
sealed execution, but **not their prior step-log content** — the prior
execution's log blobs become unreferenced (and are eventually GC'd), so the
re-run's sealed execution has no logs for the cached jobs.

**Why.** Faithful parity with the legacy resume, where the reused on-disk exec
dir kept the prior logs. A user inspecting a resumed run currently sees the
cached jobs as succeeded but can't read their logs from the new execution.

**Scope.** Read the prior steps' log blobs (`objread.StepLog`) for the resumed
jobs and re-attach them to the new run's working tree before/at seal. Needs a
small capability on `internal/objrun` / `internal/runworktree` to pre-attach
logs for jobs that are not executed this run (the live `AfterStepLog` hook only
fires for jobs that actually run). Bounded; one package seam + the cmd wiring +
a test.

**Status.** Deferred — shipped as a documented v1 limitation in #244.

---

## 2. Packfile delta compression (larger, profiling-gated)

**What.** The object store writes loose, zstd-compressed objects today. Git-style
packfiles with delta compression would shrink on-disk size for large histories
(many revisions/executions sharing structure).

**Why.** The "disk win" (content addressing + sharing) already beats a
copy-per-run layout (`objmodele2e.TestObjectModelDedupDiskWin`), but packing is
the next lever for very large repos.

**Scope.** A new milestone, not a small change: pack format, pack/unpack, GC
interaction, read-path fallthrough (loose → pack). `design.md` lists it as a
"deferred milestone, profiling-gated" — i.e., do it only when profiling on real
histories shows loose objects are the bottleneck.

**Status.** Deferred to Phase-3 (`design.md`, `risks-and-open-questions.md`).

---

## 3. `objectstore` atomic-write fault-injection seam (small, test-only)

**What.** `internal/objectstore` is gated at 90% coverage; the residual
uncovered lines are defensive filesystem-error returns in the atomic write path
(temp-file / fsync / rename failures). The test environment runs as root, so
permission-based fault injection is bypassed; covering these branches needs a
fault-injection FS seam (the pattern `internal/statestore` uses with
`writeFn`/`syncFn`/`closeFn`).

**Why.** Lift `objectstore` from the 90% practical gate toward the 95%
aspiration, and exercise the error-wrapping branches.

**Scope.** Add injectable write/fsync/rename/createTemp seams to the local store
(mirroring `statestore`) + tests. Test-only production-code indirection.

**Status.** Deferred (`test-plan.md` §`internal/objectstore`) — judged not worth
the production-path indirection for error-wrapping branches.

---

## Explicitly out of scope here

`orun state migrate` / catalog migrators referenced in the **other** spec trees
(`specs/orun-state-redesign/`, `specs/orun-component-catalog/`) describe those
efforts' own designs. The command was removed in the object-model cutover; those
specs are stale with respect to it, but reconciling them belongs to those
efforts, not this one.
