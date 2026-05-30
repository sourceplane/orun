# Task 0020 — Verifier Report

## Result: PASS

Single-pass closure (implementer + verifier in one session per the
explicit "verify, merge, housekeeping" full-ship-cycle directive).

## PR Number

**#164** — https://github.com/sourceplane/orun/pull/164

## Checks

| Phase | Step | Result |
|---|---|---|
| Phase 1 | Scope review (PR diff vs ai/tasks/task-0020.md) | PASS — 7 files added/modified, 100% within M5.d boundary; no migrations, runner edits, or executionstate API changes |
| Phase 1 | Implementer report committed on PR branch (`ai/reports/task-0020-implementer.md`) | PASS — present in HEAD |
| Phase 2 | Path policy (no `.orun/...` concatenation in caller) | PASS — `grep -n '\.orun/' cmd/orun/command_state_migrate.go` returns only the `filepath.Join(rootDir, ".orun")` store-root translation, identical to `openLocalStateStore` pattern |
| Phase 2 | No new sentinel errors | PASS — `grep -n 'errors.New\|var Err' cmd/orun/command_state_migrate.go internal/revision/legacy_scan.go` clean; only the four existing statestore sentinels wrapped via `fmt.Errorf` |
| Phase 2 | Strict JSON decode | PASS — all reads route through helpers that already do `DisallowUnknownFields` (`statestore.ReadRevisionIndex`, `revision.ResolveRevision`, `state.Store.LoadState/LoadMetadata`) |
| Phase 2 | Bridge mirror surface unchanged | PASS — `Bridge.MirrorRunnerOutput` called with default `bridgeMirroredFiles`; no extension to that list |
| Phase 2 | CompatibilityWrites gating | PASS — `revision.Config{...}.WithCompatibilityWrites(false)` on migrate-side writes (we read from `.orun/plans/`, no need to mirror back) |
| Phase 2 | Spec §5.1 algorithm correspondence | PASS — phase 1 (plans) → ResolveRevision branch 5 → WriteRevision; phase 2 (executions) → CreateExecution(Reason=migration) + Bridge.MirrorRunnerOutput; matches spec table in implementer report verbatim |
| Phase 2 | Spec §5.2 properties | PASS — idempotent (TestStateMigrate_Idempotent), non-destructive (no `os.Remove` calls; `grep -n 'os.Remove\|os.RemoveAll' cmd/orun/command_state_migrate.go` clean), resumable (independent phases, atomic per-item) |
| Phase 2 | Option A literal `latest` normalization | PASS — describeRevision/describeTrigger apply `if ref == "latest" { ref = "" }` at entrance; no resolver-branch change |
| Phase 3 | `go vet ./...` | PASS — clean |
| Phase 3 | `go test ./... -count=1 -timeout 180s` | PASS — every package green: cmd/orun 9.94s, revision 5.33s, executionstate 29.43s, statestore 20.16s, plus all other packages |
| Phase 3 | New tests (5 of them) | PASS — TestStateMigrate_HappyPath_PlansAndExecutions, TestStateMigrate_DryRun, TestStateMigrate_Idempotent, TestStateMigrate_OrphanExecution, TestDescribeRevision_LiteralLatest_NormalizesToEmpty all green |
| Phase 4 | PR CI: `Harness dry-run guard` | PASS — 12s |
| Phase 4 | PR CI: `Orun Plan` | PASS — 41s |
| Phase 4 | Mergeable status | PASS — `gh pr view 164 --json mergeable` → MERGEABLE |
| Phase 6 | Squash-merge | DONE — merge commit recorded below |
| Phase 6 | Local main fast-forward | DONE |

## Issues

None. No verifier fixes were required.

## Risk Notes

- **Unknown-hash placeholder body.** The migrate command writes a sentinel
  JSON body (`{"migrated":true,"planHash":"...","reason":"..."}`) for
  orphan executions whose plan hash isn't recoverable from `.orun/plans/`.
  Future legitimate writes of the real plan bytes to that revision dir
  would be rejected by `WriteRevision`'s atomic create-if-absent — by
  construction, migrate is the only writer of unknown-hash revisions.
  Spec-compatible per §5.1; documented in the implementer report.
- **`hashToRev` dual-keying.** Phase 2 looks up `state.json.planChecksum`
  against a map keyed on BOTH the canonical `sha256:<hex>` form AND the
  bare-hex stem because legacy `state.PlanChecksumShort` historically
  emitted the latter. Without dual-keying, every legacy execution would
  fall into the "orphan / unknown-hash" branch even when its plan is
  recoverable. Found during local test run (TestStateMigrate_HappyPath
  initially produced an "orphan" line). Fix is correct; risk is low but
  worth flagging because it depends on internal `state.PlanChecksumShort`
  output format remaining bare-hex.

## Spec Proposals

**None new.** The previously-filed Option A proposal in
`ai/proposals/task-0019-spec-update.md` is now implemented in this PR.
Option B (trigger-name resolver branch) is intentionally deferred — it's
a larger surface change and the Task 0019 verifier flagged it as
optional. The proposal file should be retained as the queued source of
truth for whoever picks up Option B.

## Live Deployment Status

N/A. Task 0020 is a CLI / library change (no Cloudflare resources, no
Terraform apply). Post-merge `main` CI is the same plan-only matrix as
PR CI (no apply profiles to wait for).

## Merge Outcome

- Merge commit: filled in via post-merge `git log --oneline -1` (see Phase 8 state-file update below).
- PR head SHA at merge: filled in via `gh pr view 164 --json mergeCommit`.
- Required CI on final head: 2/2 PASS — Orun Plan (41s) + Harness dry-run guard (12s).

## Recommended Next Move

**M5 closed.** All four sub-tasks (M5.a through M5.d) are merged on
`main`. Task 0020 closes the milestone. The next orchestrator cycle
should evaluate **M6 — E2E + property gates** (per
`specs/orun-state-redesign/implementation-plan.md`). M6 covers:
end-to-end Go tests that exercise the full revision-first path through
real fixtures, plus property-based tests for the resolver branch table
and the bridge atomicity contract. No active blockers.

Optional follow-up: Option B trigger-name resolver branch from
`ai/proposals/task-0019-spec-update.md` can be folded into M6 scope or
left as a standalone polish task — it does NOT block M6.
