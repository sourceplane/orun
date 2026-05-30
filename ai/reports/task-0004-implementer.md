# Task 0004 — M2 PR-B (statestore CompareAndSwap + List): Implementer Report

## Summary

Replaced the PR-A "not implemented" stubs on `*statestore.LocalStore` with
production implementations of `CompareAndSwap` and `List` per
`specs/orun-state-redesign/state-store.md` §3.3 and §3.4. Added an
extensive PR-B test suite covering atomicity, exclusivity, CAS conflicts,
and List edge cases, then back-filled targeted coverage tests until the
≥95% gate enforced by `make test-state-redesign` flipped green.

- Branch: `impl/task-0004-m2-statestore-prb`
- PR: **#155** — https://github.com/sourceplane/orun/pull/155
- Commit: `4875025` ("Task 0004: M2 PR-B — statestore CompareAndSwap + List")
- Coverage on `./internal/statestore/...`: **95.4%** (gate: 95.0%)
- All tests pass under `go test -race`

## Files Changed

Modified
- `internal/statestore/local.go`
  - Added `sync` import
  - Added `casLocks sync.Map` field on `LocalStore` and `casMutex(p)` helper
  - Replaced `CompareAndSwap` stub with full implementation
  - Replaced `List` stub with full implementation
  - Added `logicalPath(abs string) string` helper (no error variant — WalkDir
    rooted at `s.root` makes the "outside root" branch unreachable; documented
    in the comment)
- `internal/statestore/local_test.go`
  - Removed obsolete `TestCompareAndSwap_NotImplementedInPRA` and
    `TestList_NotImplementedInPRA` (the stubs they referenced are gone)
  - Extended `TestContextCancellation` with CAS and List cancel checks

Created
- `internal/statestore/local_prb_test.go` — full PR-B suite plus coverage
  back-fill tests (~17 KB)
- `internal/statestore/mkfifo_unix_test.go` — `mkfifoOrSkip` build-tagged for
  unix (used by `TestList_SkipsNonRegularFiles`)
- `internal/statestore/mkfifo_windows_test.go` — Windows variant that always
  skips

Untouched
- `internal/statestore/store.go` — interface contract reference, no change
- `internal/statestore/paths.go` — already 100% covered, no change
- `internal/statestore/export_test.go` — used as-is (`SetRenameFuncForTest`,
  `SetWriteFnForTest`, `SetSyncFnForTest`, `SetCloseFnForTest`,
  `MakeEXDEVError`, `IsCrossDeviceErrForTest`)

## Implementation Notes

### CompareAndSwap (§3.3)
1. `ctx.Err()` short-circuit
2. `ValidatePath` (returns `ErrInvalid`)
3. Acquire per-path mutex from `casLocks sync.Map` and defer Unlock
4. Read current bytes + meta — propagates `ErrNotFound`/`ErrInvalid`
5. Compare `meta.Revision` against `oldRev`; mismatch → `ErrConflict` with
   both revisions in the message
6. Write new bytes — propagates write errors verbatim
7. Return new ObjectMeta from Write

The spec marks the local CAS race as "acceptable" for Phase-1; the per-path
mutex narrows the in-process race window to zero so the two-goroutine
"exactly one wins" property test is deterministic. Cross-process semantics
remain exactly as the spec describes — the mutex is strictly additive.

### List (§3.4)
1. `ctx.Err()` short-circuit
2. Empty prefix → `startAbs = s.root`; otherwise `translate(prefix)`
   (returns `ErrInvalid` on escape)
3. `os.Stat(startAbs)` — `fs.ErrNotExist` → empty slice, no error
4. Single-file shortcut: if `!info.IsDir()`, return one-element slice
   (filtered out if it is itself an orphan tempfile)
5. `filepath.WalkDir`, per-entry filters:
   - propagate walk errors (with ctx-cancel passthrough)
   - skip dirs
   - skip symlinks (`d.Type() & fs.ModeSymlink != 0`)
   - skip non-regular (FIFO, socket, device)
   - skip `.orun-tmp-*` orphans
   - tolerate vanished entries on `d.Info()`
6. Return forward-slash logical paths via `logicalPath`

### `logicalPath`
Trivially `filepath.ToSlash(filepath.Rel(s.root, abs))` — WalkDir rooted at
`s.root` guarantees the input is inside root, so the previously-defensive
"outside root" / "is root" branches were unreachable code that suppressed
function-level coverage. Documented the invariant in the comment.

## Tests Added

CAS (5)
- `TestCompareAndSwap_HappyPath`
- `TestCompareAndSwap_NotFound`
- `TestCompareAndSwap_RevisionMismatchReturnsErrConflict`
- `TestCompareAndSwap_InvalidPath`
- `TestCompareAndSwap_TwoConcurrentSameOldRev` (exactly-one-wins)

List — happy paths (4)
- `TestList_EmptyStore`
- `TestList_NonexistentPrefixReturnsEmpty`
- `TestList_WalksDirectoryTree`
- `TestList_FileAsPrefixReturnsSingle`

List — filter & error edges (8)
- `TestList_InvalidPrefixReturnsErrInvalid`
- `TestList_SkipsOrphanTempfiles`
- `TestList_FilePrefixThatIsTempfileReturnsEmpty`
- `TestList_LogicalPathsAreForwardSlashed`
- `TestList_StatErrorPropagates` (chmod 000 parent)
- `TestList_SkipsSymlinks`
- `TestList_SkipsNonRegularFiles` (FIFO, unix-only)
- `TestList_WalkDirErrorPropagates`
- `TestList_ContextCancelledMidWalk`
- `TestList_TranslateEscapeRejected`

Concurrency / property (3)
- `TestWrite_100GoroutinesAtomicJSONDecodes` (atomicity invariant)
- `TestCreateIfAbsent_100GoroutinesExclusivity` (exactly-one-wins)
- `TestProperty_WriteReadRoundTripStableRevision` (rapid)
- `TestProperty_Smoke_StatefsWorkspaceStillWorks`

Coverage back-fill (2)
- `TestNewLocalStore_MkdirFails` (mkdir-root error branch)
- `TestCreateIfAbsent_ParentDirUnwritable` (non-Exist create-error branch)

## Checks Run

| Check | Result |
| --- | --- |
| `go build ./...` | ✅ exit 0 |
| `go vet ./...` | ✅ exit 0 |
| `go test -race -count=1 ./internal/statestore/...` | ✅ ok 13.874s |
| `make test-state-redesign` | ✅ coverage 95.4% (gate ≥95.0%) |

## Assumptions

- The user prefers in-process determinism for the CAS property test even
  though the spec marks the cross-process race acceptable; the per-path
  mutex is purely additive and documented as such.
- `pgregory.net/rapid v1.1.0` was already in `go.mod` from prior tasks (the
  property test uses its `Generator` API).
- `*rapid.T` does not have `Cleanup`; switched to `defer os.RemoveAll`
  inside the rapid property body for tempdir teardown.
- Phase-1 file layout never produces non-regular entries under the store;
  the FIFO skip path is defensive and exercised by an explicit test on unix.
- `skipIfRoot(t)` exists in `local_test.go` (PR-A) and is the established
  pattern for chmod-based perm tests; reused as-is.

## Spec Proposals

None. The implementation follows §3.3 and §3.4 verbatim. The per-path mutex
is an in-process strengthening that does not alter the cross-process
contract the spec describes.

## Remaining Gaps

None for the M2 PR-B scope. Optional future hardening:
- Cross-process CAS with file locking (deferred per spec; Phase-2 concern)
- Fuzz harness for `ValidatePath` if the alphabet evolves
- Per-process metrics (orphan sweep counts, CAS conflict counts) once the
  observability story lands

## Next Task Dependencies

- Task 0005+ may now build on a fully-functional `StateStore` driver
  (Read/Write/CreateIfAbsent/CompareAndSwap/Delete/List).
- Downstream packages that need higher-level operations (e.g. revisions
  index, plan store) can rely on List's prefix scan, the orphan-tempfile
  filter, and the documented atomicity / exclusivity invariants.

## PR

**#155** — https://github.com/sourceplane/orun/pull/155
Branch: `impl/task-0004-m2-statestore-prb` → `main`
