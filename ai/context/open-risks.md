# Open Risks

## Spec Drift

- **R-001: `flyingmutant/rapid` vs `pgregory.net/rapid`.** `specs/orun-state-redesign/test-plan.md` §1 references `github.com/flyingmutant/rapid` as the property-based test library. `go.mod` already pins `pgregory.net/rapid v1.1.0` (the same library under its current import path). Implementer agents must use `pgregory.net/rapid`; a small spec-clarification proposal should be filed under `/ai/proposals/` when convenient. Not a blocker for any milestone.

## Compatibility

- **R-002: ExecID shape preservation for github-artifacts cross-spec compatibility.** The new revision/execution keys defined in `data-model.md` must remain compatible with the existing `gh-{run_id}-{attempt}-{sha}` ExecID format produced by `internal/runbundle`. Cross-check during M4 (executionstate + runner bridge) and M5 (CLI rewire).

## Local-only Phase 1 Boundary

- **R-003: Out-of-scope creep.** Phase 1 explicitly excludes R2/S3/Cloud StateStore driver, Supabase/DO coordination, distributed locking, TUI surface changes, and deletion of legacy `.orun/executions/` paths. Implementer prompts must call this out per task to prevent drift.

## Operational

- **R-008: `internal/executionstate` zero-margin coverage floor (CI-stability
  liability).** The package sits at *exactly* 90.0 % statement coverage — the
  Makefile `test-state-redesign` gate floor — with no buffer. On the Task 0034
  PR #175 CI run it measured **89.6 %** (Linux + `-race`) and the `test` job
  failed; a rerun measured 90.0 % and went green. The package is byte-identical
  to main and untouched by Phase 2 C4. Root: the low-coverage functions are the
  EXDEV/hardlink mirror + legacy-resolver fallback paths (`bridge.go`
  `linkArtifact`/`destAbs` 75 %, `resolver.go` legacy walks 83–87 %,
  `writer.go` `finalizeExecution`/`scanForNextRunSeq` 77 %) whose branch
  execution is environment-sensitive (darwin vs linux, race scheduler). Any
  future PR can randomly red on this floor even when it doesn't touch the
  package. **Mitigation (deferred, NOT to be folded into a catalog PR):** add
  2–4 targeted tests to lift `internal/executionstate` ~1–2 pp off the exact
  floor, OR widen the Makefile gate tolerance (e.g. floor 89.5 % with the
  documented target staying 90 %). **Trigger to action:** if the flap recurs a
  third time on any PR, the orchestrator scopes a standalone micro-task. First
  observed 2026-05-31 (Task 0034 / PR #175, now merged at `42eace5`; the flap
  did NOT recur on the Task 0035 verifier's PR-branch or post-merge main CI
  runs). **Status: open, low-priority, watch.**

## Operational (historical)

- **R-004: `ai/` directory rebuilt from scratch.** The 2026-05-30 pivot left the old `ai/` tree as unstaged deletions in `git status`. The orchestrator has rebuilt the four compact context files plus `state.json` and `task-0001.md`. The deletions of old TUI-era tasks/reports must be committed (or reverted) when the first M0 PR opens — currently they sit dirty in the working tree. Implementer for Task 0001 will fold these `ai/` deletions into their PR so main has a coherent tree. **Resolved** at PR #152 (Task 0001).

- **R-005: Implementer "implemented locally" anti-pattern.** Task 0007 (M3 PR-A) was the first observed case of an implementer running quality gates to green on a local branch but never committing, pushing, or opening a PR. The implementer report was filed without a `PR Number` line. **Mitigation landed:** Task 0008 corrective delivery chore committed the staged tree (`96621ed`), pushed branch `impl/task-0007-m3-revision-pra`, opened PR **#157**, and backfilled the PR number into both implementer reports (`500218c`). PR #157 is OPEN+MERGEABLE+CLEAN with required CI SUCCESS at log level. **Status: mitigated for this incident; pattern guard remains open.** **Forward action:** future implementer prompts must keep the `PR Creation Requirement` section explicit and acceptance criteria should include a `gh pr list --head <branch>` check returning a non-empty array. If the same anti-pattern recurs, escalate by adding a pre-flight self-check to the implementer prompt template.

## Spec Adjudication In Flight

- **R-006: Claim-first writer ordering vs `cli-surface.md` §1.2 step-7.** Task 0007 implementer chose to reserve the index slot via `statestore.CreateIfAbsent` BEFORE writing any body file (preventing two concurrent writers with the same `(TriggerKey, planHash)` from clobbering each other's revision body via `Write` last-write-wins). Spec lists indexes as the last step. Rationale recorded in `ai/reports/task-0007-implementer.md` § "Step-Order Deviation From cli-surface.md §1.2 (claim-first)"; refs still land after bodies, preserving `state-store.md` §6 crash-recovery invariants. **Adjudication owner:** Task 0009 verifier — accept-and-document inline OR file `ai/proposals/task-0007-spec-update.md`. **Status: open until verifier rules.**

- **R-007: `version.json` helper location.** Task 0007 places `EnsureStateStoreVersion` in `internal/revision` rather than introducing a `statestore.StateStoreVersionPath()` helper alongside `paths.go`. Implementer flagged this in `ai/reports/task-0007-implementer.md` § "Open Items For Verifier" as a small follow-up. **Adjudication owner:** Task 0009 verifier — defer-with-Risk-Note vs require-relocate-now. **Status: open until verifier rules.**
