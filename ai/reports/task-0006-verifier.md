# Task 0006 — Verifier Report (M2 PR-C — typed refs + indexes)

## Result: PASS

PR #156 (`impl/task-0005-m2-statestore-prc` → head `a8a580a`) verified PASS against the
Verifier Standard in `agents/orchestrator.md` and the M2 "Done when" criteria in
`specs/orun-state-redesign/implementation-plan.md`. PR-C diff is exactly the four
files implied by the task scope plus the implementer report; nothing else changed.
Merge protocol executed: squash-merge, branch deleted, local `main` fast-forwarded,
working tree clean.

## Checks

### 1. Repo state and PR metadata

| Step | Result |
| --- | --- |
| `git fetch origin` | ok |
| `git status --short` (pre-merge) | only orchestrator state files dirty (expected; checked out before merge) |
| `git log --oneline -5 origin/main` | tip `5495f3c` Task 0004 verifier note (Task 0005 not yet merged) |
| `gh pr view 156` | OPEN, MERGEABLE, mergeStateStatus CLEAN, head `a8a580a` matches local PR branch |
| Required check `CI / Orun Plan` (run `26671612378`) | SUCCESS |
| Required check `Harness dry-run guard` (run `26671612360`) | SUCCESS |
| Other rollup checks | SKIPPED legs are the empty-matrix fanout / compose / verify rows for the conformance workflow — correct behaviour for a code-only PR with `0 components × 3 envs → 0 jobs` |

### 2. Diff audit

```
ai/reports/task-0005-implementer.md    78 +
internal/statestore/indexes.go         98 +
internal/statestore/indexes_test.go   188 +
internal/statestore/refs.go           262 +
internal/statestore/refs_test.go      449 +
5 files changed, 1075 insertions(+)
```

| Narrow diff | Result |
| --- | --- |
| `cmd/orun internal/state internal/runner internal/runbundle` | EMPTY — no production-caller wiring |
| `internal/statestore/{paths,errors,store,local}.go` | EMPTY — PR-A / PR-B surface untouched |

### 3. Spec conformance (code-path inspection)

| Item | data-model.md / state-store.md anchor | Code reference | Result |
| --- | --- | --- | --- |
| `LatestRevisionRef` fields + JSON tags | data-model.md §6.1 | `refs.go:34-39` | byte-match (`revisionKey`, `revisionId`, `planHash`, `createdAt`) |
| `LatestExecutionRef` fields + JSON tags | data-model.md §6.2 | `refs.go:42-48` | byte-match |
| `TriggerRef` fields + JSON tags | data-model.md §6.3 | `refs.go:53-60` | byte-match |
| `NamedRef` fields + JSON tags | data-model.md §6.4 | `refs.go:64-70` | byte-match |
| `RevisionIndexEntry` | data-model.md §7.1 | `indexes.go:27-34` | byte-match |
| `ExecutionIndexEntry` | data-model.md §7.2 | `indexes.go:38-45` | byte-match |
| Path construction via `paths.go` only | state-store.md §2.1 | `refs.go`, `indexes.go` | no `"refs/"` / `"indexes/"` literal in source — verified by `TestRefs_NoStringConcatenationInPaths` (`refs_test.go:424-434`) and by hand grep |
| CAS forwards `prev.Revision` directly, no re-read | state-store.md §6 (caller owns retry) | `refs.go:129-131` (`casRef`) | helper is a single line: `store.CompareAndSwap(ctx, path, prev.Revision, marshalCanonicalJSON(v))` — no Read inside |
| Index writers use `CreateIfAbsent` (duplicates → `ErrExists`) | state-store.md §3.3 | `indexes.go:66`, `indexes.go:85` | both writers route through `store.CreateIfAbsent` |
| `RebuildIndexes` is a stub returning `%w` of `ErrInvalid` | task scope + implementation-plan.md M2 | `indexes.go:94-98` | returns `fmt.Errorf("%w: RebuildIndexes is deferred to M3+", ErrInvalid)` |
| Canonical JSON: `SetIndent("", "  ")`, `SetEscapeHTML(false)`, trailing `\n` | test-plan.md byte-stability | `refs.go:82-93` (`marshalCanonicalJSON`) | encoder configures both options; `Encoder.Encode` retains the trailing newline (comment makes intent explicit) |
| Byte-stability tests via `internal/testfx/statefs.AssertJSONFile` | test-plan.md | `refs_test.go:65,220,279,372`; `indexes_test.go:58,132` | covered for every ref shape (`LatestRevisionRef`, `LatestExecutionRef`, `TriggerRef` latest+scope, `NamedRef`) and both index shapes |
| TriggerRef helper covers both `latest.json` and `<scope>.json` | data-model.md §6.3 | `refs.go:179-202` (`TriggerRefScope.path()`) | single struct with `Latest bool` + `Scope string`; `Latest=true` routes through `TriggerLatestRefPath`, `Latest=false` validates and routes through `TriggerScopeRefPath` |
| All errors wrap one of the four sentinels | state-store.md §1 | `refs.go:101` (`ErrInvalid` decode wrap); rest propagate driver errors unchanged | no new sentinels introduced (errors.go diff empty) |
| No new path helpers in `paths.go` | task constraint | `git diff` against `paths.go` | empty |
| Doc comments on every exported symbol | M2 "Done when" cross-cutting | inspected `refs.go` + `indexes.go` | all 24 exported symbols (4 ref structs, 1 trigger scope struct, 12 read/write/CAS helpers, 2 index structs, 4 index helpers, `RebuildIndexes`) carry godoc-style comments |

### 4. Local quality gates

| Command | Result |
| --- | --- |
| `go build ./...` | ok (exit 0, no output) |
| `go vet ./...` | ok (exit 0, no output) |
| `go test -race -count=1 ./internal/statestore/...` | `ok …/internal/statestore 14.072s` |
| `make test-state-redesign` | coverage gate prints `measured: 96.1%` (≥ 95 %, hits stretch ≥ 96 %) |
| `go test -cover ./internal/statestore/...` | `coverage: 96.1% of statements` (second-source confirms) |
| `go list -deps ./internal/statestore` filtered for `/orun/internal/` | only the package itself — leaf-clean |
| `kiox exec -- orun validate --intent examples/intent.yaml` | EXPECTED quirk — kiox cwd is `example-platform-repo`, not the orun repo. Pinning intent path produces a kiox-snapshot manifest schema mismatch (`apps/admin-console/component.yaml` line 12 unmarshal). CI is authoritative per task constraint #7. |
| `kiox exec -- orun plan --changed …` | not run — composition-cache quirk known since Task 0001 |
| `kiox exec -- orun run --plan … --dry-run` | skipped (no plan produced) |

### 5. CI log review

`CI / Orun Plan` — run `26671612378`, job `78615799965`:

```
Run orun plan \
  --from-ci github \
  --event-file "$GITHUB_EVENT_PATH" \
  --intent examples/intent.yaml \
  --artifact github \
  --github-output
…
│ 0 components × 3 envs → 0 jobs
│ mode: changed-only
│ plan: 67a2b1028667
✓ uploaded plan artifact: orun.v1.gh-26671612378-1-67a2b1028667.plan.sha256-67a2b.created (2279 bytes)
```

Confirmed: command shape exactly matches expected (`--from-ci github`, `--intent
examples/intent.yaml`); legitimate empty-matrix shape recorded; plan artifact
uploaded; job outputs (`job-matrix`, `plan-checksum`, `exec-id`) set.

`Harness dry-run guard` — run `26671612360`, job `78615799968`:

```
[guard] PASS: Bash syntax checks
[guard] PASS: Dry-run: at least 2 foundation@dev.smoke commands (5)
[guard] PASS: Dry-run: at least 1 api@dev.smoke command (3)
[guard] PASS: Dry-run output contains all required command/assertion markers
[guard] PASS: duplicate-claim helper: PASS case (A=2 B=0)
[guard] PASS: duplicate-claim helper: PASS case (A=0 B=2)
[guard] PASS: duplicate-claim helper: FAIL case (A=2 B=2 — both executed)
[guard] PASS: duplicate-claim helper: FAIL case (A=0 B=0 — neither executed)
[guard] PASS: duplicate-claim helper: FAIL case (A=1 B=0 — partial count)
[guard] PASS: duplicate-claim helper: FAIL case (A=3 B=0 — unexpected count)
[guard] PASS: status helper: PASS case (all success)
[guard] PASS: status helper: PASS case (all completed)
[guard] PASS: status helper: FAIL case (job missing|status=pending|running|failed|blocked)
[guard] PASS: ORUN_EXEC_ID is exported
[guard] PASS: ORUN_REMOTE_STATE is exported
[guard] PASS: assert_exactly_one_duplicate_claimant is called in harness
[guard] PASS: assert_jobs_all_succeeded is called in harness
[guard] PASS: Background PID tracking (HARNESS_PIDS) present
[guard] PASS: Signal-safe cleanup trap present
[guard] PASS: INT signal in cleanup trap
[guard] PASS: TERM signal in cleanup trap
[guard] PASS: jq preflight check present
[guard] PASS: orun binary preflight check present
[guard] PASS: Repo-linkage preflight check present
```

Full battery emitted — bash syntax, command-count thresholds, duplicate-claim
helper PASS+FAIL, status helper PASS+FAIL, exported env asserts. The Go test
package re-emits the same battery (also PASS). No required leg SKIPPED that
should have run.

### 6. Secret hygiene & production-grade basics

| Check | Result |
| --- | --- |
| `rg -i "(token\|password\|secret\|key=)" refs.go indexes.go refs_test.go indexes_test.go` | clean — no matches in either source or tests |
| Deterministic JSON | `marshalCanonicalJSON` configures `SetEscapeHTML(false)` + `SetIndent("", "  ")`; trailing `\n` is the encoder's natural terminator. Comment in code makes byte-stability intent explicit. |
| Doc comments on every exported symbol | confirmed above (M2 cross-cutting "Done when") |
| Logging of sensitive material | none — package is pure read/write helpers, no logging |

## Issues

None. No verifier fixes were required; no production-code edits made; no
spec proposals filed.

## CI Log Review

Run IDs:
- `26671612378` — `CI / Orun Plan` — SUCCESS at `2026-05-30T02:11:01Z` — `orun plan --from-ci github --intent examples/intent.yaml --artifact github` actually executed; empty-matrix legitimate; artifact uploaded.
- `26671612360` — `orun remote-state conformance / Harness dry-run guard` — SUCCESS at `2026-05-30T02:10:25Z` — full `[guard] PASS:` battery emitted (24 distinct PASS lines per leg, both raw and Go-test legs).

No required check was SKIPPED. The four SKIPPED rows in the rollup
(`${{ matrix.component }}/${{ matrix.env }}`, `Compile plan`, `Run: ${{ matrix.job }}`,
`Env fanout: ${{ matrix.env_name }}`, `Verify remote status and logs`) are the
expected fanout legs that downsize to zero when the changed-plan matrix is empty
(0 components × 3 envs → 0 jobs); they are not required checks.

## Risk Notes

1. **`RebuildIndexes` stub** — returns `%w: deferred…` of `ErrInvalid`. This is the
   intentional shape per task scope; the real implementation lands in M3+ when
   revisions actually populate. Any caller that mistakenly invokes it today gets
   a sentinel-routed failure rather than silent corruption — acceptable.
2. **In-process CAS mutex** — unchanged from PR-A; PR-C inherits the existing
   single-process guarantee. Multi-process safety is deferred to remote drivers
   (R2/GCS/Azure) per `state-store.md` §3.3.
3. **PR-C surface deferred to M3 callers** — typed helpers exist with no
   production wiring (`cmd/orun`, `internal/state`, `internal/runner`,
   `internal/runbundle` byte-identical to `origin/main`). M3 (`internal/revision`)
   is the first consumer; until then refs/indexes structs are dead code on
   `main` exercised only by the package's own tests. This is the planned PR-C
   shape, not a defect.
4. **Helper-level CAS does not re-read** — by design (caller owns retry per
   state-store.md §6). M3 callers must thread `prev ObjectMeta` from a prior
   `Read*` and implement the loser-retry loop. Documented in the godoc on each
   `CAS*` helper; no enforcement at the type system level beyond the function
   signature.
5. **`DisallowUnknownFields` strict decode** — `unmarshalRef` rejects extra
   fields. Forward compatibility (e.g. M3+ adds a field to `LatestRevisionRef`)
   will require the new field to land before any writer can emit it. This is
   acceptable for Phase 1 byte-stability but worth flagging when M3 design
   reviews schema evolution.
6. **`marshalCanonicalJSON` panics on encode failure** — only a programmer
   mistake (channels, funcs, cyclic structs) can trigger it; the typed structs
   here cannot. Acceptable trade-off.

## Spec Proposals

None required, as the implementer expected. PR-C lands cleanly against
`data-model.md` §6 / §7 and `state-store.md` §1 / §2.1 / §3.3 / §6.

## Recommended Next Move

**M2 closes with this merge.** Next orchestrator cycle should scope **M3 —
`internal/revision`** as the first consumer of these typed helpers. Suggested
PR-A scope: revision write-ordering (plan.json → indexes/revisions/<key>.json
via `WriteRevisionIndex` → `CASLatestRevisionRef` loser-retry loop) per
`state-store.md` §3 and `design.md` §5.1. Keep M3 PR-A leaf-clean against
`internal/statestore` (already a pure dependency) and avoid pulling in
`internal/state` until the revision package itself is stable. Stretch M3 PR-B
should land trigger-ref maintenance (the per-trigger latest + per-scope pointer
write order) once revisions populate.

## PR Number and Merge

PR **#156** — squash-merged into `main`. Merge commit SHA recorded below post-merge.
