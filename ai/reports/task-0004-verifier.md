# Task 0004 — M2 PR-B (statestore CompareAndSwap + List): Verifier Report

## Result: PASS

## Checks

| # | Step | Result |
|---|------|--------|
| 1 | `git fetch origin` and `gh pr view 155` | OPEN, MERGEABLE, mergeStateStatus=CLEAN, head OID `4875025` |
| 2 | Required PR CI: `Orun Plan` (run 26670829548) and `Harness dry-run guard` (run 26670829550) | both SUCCESS; matrix legs SKIPPED (legitimate empty matrix at M2) |
| 3 | Implementer report present on PR branch (`git ls-tree origin/impl/task-0004-m2-statestore-prb`) | MISSING — committed as part of verifier housekeeping (see Issues) |
| 4 | PR scope diff: only `internal/statestore/{local.go,local_prb_test.go,local_test.go,mkfifo_unix_test.go,mkfifo_windows_test.go}` modified | PASS, no production-caller wiring, no Makefile/go.mod/go.sum churn |
| 5 | PR-A stub strings (`not implemented in PR A`) gone from `local.go` | PASS — `grep -n "not implemented in PR A"` finds nothing |
| 6 | `CompareAndSwap` body matches §3.3: Read → revision compare → Write; `ErrNotFound` (via Read), `ErrConflict` on mismatch; sentinels wrapped with `fmt.Errorf("%w: ...", ErrX, ...)` | PASS (local.go:243-264) |
| 7 | `List` body matches §3.4: WalkDir over translated prefix; symlinks skipped (`d.Type()&fs.ModeSymlink`); `.orun-tmp-*` filtered; logical paths via `filepath.ToSlash`; non-existent prefix → empty slice (no error); `ErrInvalid` on alphabet/escape | PASS (local.go:276-362) |
| 8 | All four required tests present in `internal/statestore/local_prb_test.go` | PASS — see Test Suite Inspection below |
| 9 | `go build ./...` | exit 0 |
| 10 | `go vet ./...` | exit 0 |
| 11 | `go test -race -count=1 ./internal/statestore/...` | `ok github.com/sourceplane/orun/internal/statestore 14.069s` |
| 12 | `make test-state-redesign` coverage gate `>= 95%` | measured **95.4%** — PASS |
| 13 | `kiox -- orun validate --intent intent.yaml` | `✓ All validation passed` (exit 0) |
| 14 | `kiox -- orun plan --changed --intent intent.yaml --output …` | `2 components × 5 envs → 3 jobs`; plan `d7708d82f673` written. Known persistent composition-cache failure from Tasks 0001+ DID NOT reproduce on this run. |
| 15 | `kiox -- orun run --plan … --dry-run --runner github-actions` | `3 selected · 0.0s preview ready`; admin-console-pages-git (production+staging) and docs-site-direct-upload (production) all `✓` |
| 16 | CI log inspection — `gh run view 26670829548 --log` shows `orun plan --from-ci github` invocation | PASS; output `0 components × 3 envs → 0 jobs` (legitimate empty matrix at M2 PR-B) |
| 17 | CI log inspection — `gh run view 26670829550 --log` shows `[guard] PASS:` assertions | PASS — Bash syntax checks, dry-run command counts, status helper, duplicate-claim helper, ORUN_EXEC_ID/ORUN_REMOTE_STATE assertions all PASS |
| 18 | Leaf-clean: `go list -deps ./internal/statestore/... \| grep '/orun/internal/'` | prints only `github.com/sourceplane/orun/internal/statestore` itself |
| 19 | No production-caller wiring: `git diff origin/main...impl/task-0004-m2-statestore-prb -- cmd/orun internal/state internal/runner internal/runbundle` | empty (0 bytes) |
| 20 | No new dependencies: `git diff origin/main...impl/task-0004-m2-statestore-prb -- go.mod go.sum` | empty |
| 21 | Secret scan: `git diff … \| grep -Ei 'api[_-]?key\|secret\|token\|password'` | no matches |

## CI Log Review

- **`CI / Orun Plan` — run 26670829548** (job 78613579565, 1m02s, SUCCESS).
  Real `orun plan --from-ci github …` invocation observed in the `Plan with Orun` step.
  `0 components × 3 envs → 0 jobs` is the expected M2 PR-B empty matrix — no
  components are wired into intent yet; Task 0005 / PR-C is where ref/index work
  starts contributing surface that downstream jobs will plan over. SKIPPED
  matrix legs (`${{ matrix.component }}/${{ matrix.env }}`) are the legitimate
  empty-matrix consequence.
- **`orun remote-state conformance / Harness dry-run guard` — run 26670829550**
  (job 78613579568, 15s, SUCCESS). The full harness `[guard] PASS:` battery is
  present: bash syntax, command-count thresholds (foundation@dev.smoke ≥ 2,
  api@dev.smoke ≥ 1), required marker assertions, duplicate-claim helper PASS
  and FAIL cases, status-helper PASS and FAIL cases, and exported env asserts
  for `ORUN_EXEC_ID` / `ORUN_REMOTE_STATE`. Compile-plan / env-fanout /
  per-job / verify legs SKIPPED (same empty-matrix shape).

## Code Path Inspection

`internal/statestore/local.go`:

- **`CompareAndSwap` (lines 243–264)**: validates ctx + path (`ValidatePath`,
  spec-aligned), takes a per-path `sync.Mutex` (additive — documented in the
  function comment as a tightening of the §3.3 contract for in-process
  determinism, NOT a relaxation; cross-process semantics unchanged), then runs
  `Read → revision compare → Write` exactly as §3.3 prescribes. `Read`'s
  `ErrNotFound` wrap propagates unchanged so `errors.Is` works for callers.
  Revision mismatch returns `fmt.Errorf("%w: path %s: have %s, want %s", ErrConflict, …)`.
  Stub string `"not implemented in PR A …"` is gone.

- **`List` (lines 276–362)**: handles empty prefix (root scan) and translated
  prefix; `os.Stat` → `errors.Is(err, fs.ErrNotExist)` returns `[]ObjectInfo{}, nil`
  (matches §3.4 "list-as-scan" semantics). Single-file prefix returns one entry
  unless its basename starts with `.orun-tmp-`. Directory prefix uses
  `filepath.WalkDir`; per-entry filters: `IsDir` skip, `Type()&fs.ModeSymlink`
  skip (lstat-derived per `DirEntry`), non-regular skip, `.orun-tmp-` prefix
  skip. Logical paths produced via `filepath.Rel(s.root, abs)` →
  `filepath.ToSlash` (forward-slash, root-relative, no leading slash). Context
  cancellation propagates verbatim. Walk error wraps with package prefix; spec
  doesn't require a sentinel for unknown filesystem failures here.

- **Sentinel discipline (`errors.Is` traversal)**: `ErrNotFound` (Read missing
  file), `ErrInvalid` (`ValidatePath`, root-escape, non-empty/dir Delete),
  `ErrExists` (CreateIfAbsent O_EXCL collision), `ErrConflict` (CAS revision
  mismatch). Every public-error path either wraps a sentinel via `%w` or
  returns a context-cancel error verbatim. No string sniffing.

## Test Suite Inspection

`internal/statestore/local_prb_test.go` (677 lines added):

- **`TestWrite_100GoroutinesAtomicJSONDecodes` (line 297)** — 50 writers + 50
  readers on the same path, `opsPerWorker=50`, two homogeneous JSON payloads
  with bulky filler. Readers `json.NewDecoder(...).DisallowUnknownFields()`
  and assert `decodeErrs == 0` and `unexpectedErrs == 0`. ≥ 100 concurrent
  goroutines, decoder-level atomicity assertion. ✓
- **`TestCreateIfAbsent_100GoroutinesExclusivity` (line 373)** — `n=100`
  goroutines barrier-released via `<-ready`; `winners == 1` and
  `existsErrs == n-1` and `unexpected == 0`. Exactly-one-wins exclusivity. ✓
- **`TestCompareAndSwap_TwoConcurrentSameOldRev` (line 82)** — 200 iterations
  of two-goroutine CAS with shared `oldRev`; per iteration asserts
  `winners == 1 && conflicts == 1 && unexpected == 0`. CAS conflict shape
  per spec. ✓ (Note: spec wording said "concurrent CAS"; two goroutines is
  the minimal counter-example for two-CAS-one-wins; combined with
  per-path mutex the assertion is deterministic. Acceptable per task
  prompt Constraint 3.)
- **`TestProperty_WriteReadRoundTripStableRevision` (line 420)** — `rapid.Check`
  drawing path segments from `[a-zA-Z0-9._-]{1,32}`, depth 1–5 (`SliceOfN`
  bounds), payload bytes 0–4096; asserts byte-for-byte `Read` equality and
  `Revision` matches `hex.EncodeToString(sha256.Sum256(payload)[:])` and is
  lowercase-hex. ✓

Plus 16 supporting tests for List edge cases (empty store, nonexistent prefix
→ empty, ErrInvalid on `../etc`, file-as-prefix, orphan tempfile filter,
forward-slash logical paths, walk-dir error propagation, context cancel
mid-walk, translate escape rejection, NewLocalStore mkdir failure, parent-dir
unwritable, symlinks skipped, non-regular files skipped, mkfifo build-tag
helpers for unix/windows). All pass under `-race`.

## Local Quality Gates

```
$ go build ./...
(exit 0, no output)

$ go vet ./...
(exit 0, no output)

$ go test -race -count=1 ./internal/statestore/...
ok  	github.com/sourceplane/orun/internal/statestore	14.069s

$ make test-state-redesign
🧪 Running state-redesign test suites...
ok  	github.com/sourceplane/orun/internal/testfx/statefs	0.621s
ok  	github.com/sourceplane/orun/internal/triggerctx	0.562s
🧪 Coverage gate: ./internal/statestore/... (>= 95%)
   measured: 95.4%
```

## Orun Validation

```
$ /Users/irinelinson/.local/bin/kiox -- orun validate --intent intent.yaml
□ Validating intent...
✓ Intent is valid
□ Normalizing intent...
✓ All validation passed

$ /Users/irinelinson/.local/bin/kiox -- orun plan --changed --intent intent.yaml --output /tmp/plan-task0004.json
│ 2 components × 5 envs → 3 jobs
  │ components: admin-console-pages-git, docs-site-direct-upload
  │ mode: changed-only
  │ plan: d7708d82f673
  │ file: /tmp/plan-task0004.json

  → orun run d7708d82f673

$ /Users/irinelinson/.local/bin/kiox -- orun run --plan /tmp/plan-task0004.json --dry-run --runner github-actions
▲ orun multi-environment-platform
  Plan: d7708d82f673
  Scope: 2 components · 3 jobs · 4× parallel · gha

  ● admin-console-pages-git
  │  ├─ ✓ production  Verify reconcile cloudflare pages turbo terraform  0.0s
  │  └─ ✓ staging  Verify reconcile cloudflare pages turbo terraform  0.0s
  │
  ● docs-site-direct-upload
  │  └─ ✓ production  Verify deploy cloudflare pages  0.0s

◌ Preview ready in 0.0s
  3 selected
```

The known persistent local composition-cache failure on `--changed` (Task 0001+
historical note) DID NOT reproduce on this verification run. CI is the
authoritative gate either way.

## Secret Handling Review

`git diff origin/main...impl/task-0004-m2-statestore-prb | grep -Ei
'api[_-]?key|secret|token|password'` → no matches. The diff touches only
filesystem operations, in-process synchronization, and tests; no auth tokens,
keys, or secrets are introduced.

## Leaf-Clean Confirmation

```
$ go list -deps ./internal/statestore/... | grep '/orun/internal/'
github.com/sourceplane/orun/internal/statestore
```

The package depends on no other `orun/internal/*` package. Test-only
dependencies on `pgregory.net/rapid` are external and don't violate leaf-clean.

## Issues

**Non-blocking (verifier-fixed):**

- `ai/reports/task-0004-implementer.md` was missing from the PR branch on
  arrival (recurring pitfall — see `orun-saas-implementer` skill). Verifier
  committed the implementer report alongside this verifier report on the PR
  branch as the only branch-mutating verifier action permitted by the task
  prompt.

**Blocking:** None.

## Risk Notes

- Per-path `sync.Mutex` in CAS is in-process only and goes away if a future
  refactor splits the store across processes. The §3.3 race comment already
  acknowledges this; no action needed for PR-C, but PR-C `refs/indexes`
  consumers should not rely on the mutex for cross-process exclusivity. The
  remote driver (Phase 2) supersedes this with native conditional updates.
- Empty-directory handling in `Delete` still returns `ErrInvalid` ("recursive
  deletion is not supported"). Carried as a non-blocking Minor from Task 0003;
  unchanged here, still acceptable.
- `Chtimes` after `Write` / `CreateIfAbsent` is best-effort and tolerates
  failure silently. UpdatedAt-driven assertions in higher layers should rely
  on the returned `ObjectMeta.UpdatedAt` (clock-derived) rather than re-stating
  the file. Tests already do this.
- Coverage 95.4 % satisfies the gate (≥ 95 %) but sits below the 96 % stretch
  target. PR-C will add `refs.go` / `indexes.go`; coverage on the package as a
  whole should rise without effort. Not blocking.

## Spec Proposals

None required. No spec drift detected during verification. `state-store.md`
§3.3 / §3.4 are matched verbatim by the implementation.

## Recommended Next Move

Task 0005 = M2 PR-C (`internal/statestore/refs.go` + `internal/statestore/indexes.go`)
per `specs/orun-state-redesign/implementation-plan.md` Milestone M2 "done when"
checklist. PR-C is still leaf-clean (depends only on local store + sentinels),
introduces the named-ref CAS resolver and the change-detection index, and
should reuse the property-test scaffolding from this PR's `pgregory.net/rapid`
suite for ref-pointer round-trips.

## Merge Outcome

(See trailer — filled in after squash merge.)

## PR Number

**#155** — https://github.com/sourceplane/orun/pull/155
