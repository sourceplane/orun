# Task 0011 — Verifier Report (M3 PR-B)

- **PR**: https://github.com/sourceplane/orun/pull/158
- **Branch**: `impl/task-0010-m3-revision-prb`
- **Head SHA verified (pre-merge)**: `ec74af10244dd5a50d949f8c154d750aed6924d7` (re-checked after the verifier-side commit on this branch)
- **Spec sources**: `specs/orun-state-redesign/{data-model.md,compatibility-and-migration.md,state-store.md,design.md,test-plan.md,implementation-plan.md}`

## Result

**PASS**

## Checks

| Step | Command / Inspection | Outcome |
| --- | --- | --- |
| 1 | `git fetch origin` + `git checkout main` + `git pull --ff-only` | clean; main at `7f1e53d` |
| 1 | `gh pr view 158 …` | `state=OPEN`, head SHA `ec74af1`, both required CI checks SUCCESS |
| 2 | `git diff --stat origin/main...pr-158` | 12 files, +1966/-22 — exactly the M3 PR-B surface |
| 2 | overreach diff `cmd/orun internal/state internal/runner internal/runbundle` | empty |
| 2 | overreach diff `internal/triggerctx internal/statestore internal/testfx/statefs` | empty |
| 2 | M4 leakage diff `internal/executionstate` | empty (path does not exist on the PR tree) |
| 2 | `git ls-tree -r pr-158 -- internal/revision/` | matches expected file set (errors, legacy, manifest, resolver, model, writer + their tests; PR-A files preserved) |
| 3 | `manifest.go` ↔ `data-model.md` §4 | RevisionManifest field order matches spec; trailing `\n`, `SetIndent("  ")`, `SetEscapeHTML(false)` via `marshalCanonicalJSON`; doc comments cite §4 |
| 3 | `manifest.go` `WriteManifest` | uses `store.Write` on `ManifestPath(revKey)` (NOT `CreateIfAbsent`). See Adjudication §1 — re-runs are byte-identical for the same `(rev, trig)` so the Write semantics are safe; the implementer's "idempotent re-run surfaces ErrExists" claim is inaccurate but the **behavior is still idempotent** (re-running writes the same bytes). Documented as a non-blocking Risk Note. |
| 3 | `manifest.go` `UpdateLatestExecutionSummary` | Read → bytes-equal short-circuit → CAS; bounded by `casRetryBudget=5`; on exhaustion returns `fmt.Errorf("%w: …", statestore.ErrConflict, …)` → `errors.Is(err, statestore.ErrConflict)` works |
| 3 | `resolver.go` seven branches in compat §3 order | confirmed; explicit comment markers on each branch; `ResolveSource` enum-ish strings stable |
| 3 | resolver is read-only | `rg "store\.(Write|CreateIfAbsent|CompareAndSwap)" internal/revision/resolver.go` → no matches |
| 3 | `legacy.go` path helpers | no string concat outside this file; both helpers route through `statestore.ValidateComponent` |
| 3 | `writer.go` compat-mirror promoted from PR-A stub | `writeCompatibilityMirror` uses `store.Write` for BOTH `plans/<hex>.json` and `plans/latest.json`. See Adjudication §3. |
| 3 | `errors.go` | declares `ErrAmbiguousArg` + `ErrComponentRunUnchanged` only, both with doc comments; `rg "var Err" internal/statestore/` shows no new sentinels there |
| 3 | imports leaf-clean | `go list -deps ./internal/revision \| rg "/orun/internal/"` → only `statestore`, `triggerctx`, `model`, `trigger` (the last two are transitive via triggerctx — allowed) |
| 4 | `go build ./...` | exit 0 |
| 4 | `go vet ./...` | exit 0 |
| 4 | `go test -race -count=1 ./internal/revision/... ./internal/statestore/... ./internal/triggerctx/...` | all green |
| 4 | `go test -race -count=1 ./...` | all green (entire repo) |
| 4 | `make test-state-redesign` | `statestore: 96.1%`, `revision: 90.4%` — both gates pass |
| 4 | `rg "(token\|password\|secret\|key=)" internal/revision/` | only test field names (`latestExecutionKey`, `RevisionKey`) — no plaintext credentials |
| 5 | resolver_test.go branch matrix | branches 1, 2, 3, 4, 5, 6, 7 each tested with explicit `ResolveSource` assertion or sentinel `errors.Is`; branch-3 NotFound fallthrough → branch-7 explicitly tested (`TestResolveRevision_Branch3_NotFoundFallsThroughToBranch7`); branch-5 length floor tested (`TestResolveRevision_LegacyHashShortRejected`); file-precedes-revision-key precedence tested |
| 5 | manifest_test.go | golden, idempotent, nil store, bad key, bad trigger, UpdateLatestExecutionSummary happy / idempotent short-circuit / not-found / nil store / decode error / cross-exec CAS |
| 5 | writer_compat_test.go | true-path bytes byte-identical; false-path absent; `sha256:` strip path; JobCount threaded; `normalizeLegacyChecksum` table; `legacyPlanPath` validation; `isHexLower` |
| 6 | `gh run view 26674136935 --log` | "Plan with Orun" step shows `0 components × 3 envs → 0 jobs` (legitimate empty-matrix shape — code-only PR) |
| 6 | `gh run view 26674136948 --log` | full `[guard] PASS:` battery (bash syntax, command-count thresholds, duplicate-claim PASS+FAIL, status helper PASS+FAIL, exported env asserts) |
| 6 | matrix SKIPs | five legitimate empty-matrix skips, same shape as #152/#155/#156/#157 |
| 7 | secret hygiene | clean |

## Adjudications

### 1. Branch 3 (revision-key) `ErrNotFound` fallthrough — ACCEPTED with Risk Note

The seven-branch chain in `compatibility-and-migration.md` §3 is normative on **order**, not on **bubble-vs-fallthrough**. The implementer's reading — that a regex-matching arg might also be a known named ref or legacy hash — is logically defensible, but in practice `revisionKeyPattern` requires a `rev-` prefix that cannot collide with branch 4 (single-component named refs lack `/`) or branch 5 (lowercase hex digits only, no hyphen). The fallthrough is therefore **paranoia code with no observable behavior difference** from a bubble-up implementation in any reachable input. No spec amendment required; the branch ordering reads correctly either way. Captured in Risk Notes.

### 2. `JobCount` Option A — ACCEPTED with Risk Note

Threading `Config.JobCount` into `RevSummary.JobCount` matches `data-model.md` §3 (`RevSummary` carries `jobCount` as planner-supplied scalar; spec does not prescribe deriving it from `plan.json`). `0 = unknown` is a faithful encoding because no in-tree caller currently produces real job counts (M5 wires the planner). The residual ambiguity between "no jobs" and "unknown count" lives in the `0` literal and **does not collapse to "lying"** — the persisted value is exactly what the caller passed. A future plan that genuinely emits zero jobs (no-op plan) would also persist 0, indistinguishable from "unknown"; M5 can either disambiguate at the caller (`-1` for unknown, `0` for empty plan) or accept the convention. Logged as a Risk Note for M5. No spec amendment needed in PR-B scope.

### 3. Compat-mirror `latest.json` last-write-wins via `Write` — ACCEPTED with Risk Note

`compatibility-and-migration.md` §2 prescribes BOTH `.orun/plans/<checksum>.json` and `.orun/plans/latest.json` as legacy aliases written byte-identical to canonical `plan.json`. The spec's idempotence guarantee (§3 reader fallback notes "running twice produces no new files") is preserved by the implementer because:

- The checksum mirror path uses `store.Write` (not `CreateIfAbsent` as the implementer's report claims). Re-runs of `WriteRevision` on the same plan bytes therefore overwrite the alias with **byte-identical content** — naturally idempotent at the bytes level. The implementer report's claim that `ErrExists` is treated as success is **incorrect about the code** but **harmless about the behavior**: there is no `ErrExists` path because Write does not surface one.
- The latest-pointer path uses `store.Write` — last-write-wins, matching §2's "alias" semantics where multiple writers race for the same alias and the loser's bytes equal the winner's bytes for the same `(planHash, planBytes)` pair.

The behavior matches the spec; the **implementer report's prose is mildly inaccurate about the implementation**. The verifier raises this as a non-blocking documentation drift in the implementer report, not a code bug. No spec amendment required. The implementer should correct the prose in their next report cycle if convenient. No proposal filed.

## Issues

None blocking.

## Risk Notes

1. **Branch-3 fallthrough is paranoia.** `revisionKeyPattern` cannot collide with branches 4/5/6 in practice; the fallthrough adds no observable behavior. Acceptable. A future spec polish to compatibility-and-migration.md §3 could make the precedence-collision rule explicit ("first match wins; ErrNotFound terminates"), but it is not required for PR-B.
2. **`JobCount=0` ambiguity** between "empty plan" and "unknown count" persists in the on-disk shape. M5 callers (planner-aware `orun plan`) will encode the real count; until then, manifests under M3 will show `summary.jobCount = 0` for every revision. A future ML/CLI display layer should not equate 0 with "no work" — the `summary.activeEnvironments` field is the authoritative job indicator until M5 lands.
3. **`UpdateLatestExecutionSummary` has no callers in tree.** The helper ships in PR-B but its M4 call site (`internal/executionstate` writer) is the next milestone. Tests exercise the helper directly via injected stores; production exposure is zero until Task 0012 wires the executionstate bridge.
4. **`WriteManifest` overwrites unconditionally via `store.Write`.** This is correct (the manifest is a derived projection — re-deriving from the same `(rev, trig)` produces byte-identical bytes), but means a corrupted on-disk manifest is silently replaced by the next `WriteRevision` on the same revision. No detection mechanism beyond `UpdateLatestExecutionSummary`'s `DisallowUnknownFields` strict-decode (which only fires on the per-execution update path).
5. **Implementer report prose vs. code drift.** `ai/reports/task-0010-implementer.md` claims the checksum mirror uses `CreateIfAbsent` with `ErrExists`-as-success; the code uses `Write`. Behavior matches spec; report prose does not match code. Non-blocking, flagged for the implementer's awareness.

## Spec Proposals

**None required.** All three adjudication points adjudicated inline with Risk Notes.

## Recommended Next Move

M3 closes on this merge. Next is **Task 0012 = M4 implementer** scoping `internal/executionstate` + runner bridge per `implementation-plan.md` Milestone M4. The M4 implementer will be the first real caller of `UpdateLatestExecutionSummary` and will validate the helper's CAS + idempotent-short-circuit posture under load.

## PR Number + Merge SHA

- **PR**: #158
- **Merge commit SHA**: _to be filled in after squash-merge_
