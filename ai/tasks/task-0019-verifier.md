# Task 0019 — Verifier

Agent: Verifier

## Current Repo Context

- Active spec: `specs/orun-state-redesign/` (Phase 1, local-only). Active milestone: **M5 — CLI rewire**. M5.a (PR #161 → `7a9c494`) and M5.b (PR #162 → `59d06f3`) closed PASS. **Task 0019 = M5.c implementer is now complete** — PR #163 open against `main`, branch `impl/task-0019-m5c-orun-read-commands-rewire`.
- PR #163 head SHA: `947773d` (commit) + housekeeping commit `fb364f1` (implementer report). `mergeable=MERGEABLE`, `mergeStateStatus=CLEAN`. Required CI checks both PASS on head SHA: `CI / Orun Plan` (run `26686932774`, 56s, SUCCESS); `Harness dry-run guard` (run `26686932783`, 13s, SUCCESS). 5 matrix legs SKIPPED (empty matrix at M5.c — same shape as M5.a/M5.b).
- Implementer report: `ai/reports/task-0019-implementer.md`. Diff stat per `gh pr view 163`: 8 files changed, +959 / −12.
- Files added/modified per the implementer report:
  - new: `cmd/orun/read_resolve.go` (98 LOC) — CLI-side glue calling `executionstate.ResolveExecution` / `revision.ResolveRevision`.
  - new: `cmd/orun/bridge_mirror_warn.go` (57 LOC) — best-effort `events/<seq>-bridge-mirror-failed.json` scanner.
  - modified: `cmd/orun/command_status.go` (+10) — `--revision` / `--exec-id` flags; existing `--all` retained.
  - modified: `cmd/orun/command_logs.go` (+25 / −11) — three-tier resolution ladder per `cli-surface.md` §4.
  - modified: `cmd/orun/command_describe.go` (+161) — `revision` / `trigger` / `execution` aliases + triplet exposure.
  - modified: `cmd/orun/command_get.go` (+166) — revision-first `get plans` table with legacy fallback.
  - new: `cmd/orun/command_read_revision_test.go` (385 LOC) — fresh / legacy / mixed workspaces; `bridge-mirror-failed` stderr; new flags / aliases.
  - artifact: `ai/reports/task-0019-implementer.md`.
- Implementer reports the persistent local `kiox -- orun plan --changed` composition-cache quirk reproduced; CI run on a clean machine is authoritative (matches the carry-forward note in `ai/state.json`).
- Implementer reports zero spec proposals filed and zero edits to writer/runner/spec-behavioral code.

## Objective

Verify PR #163 against Task 0019 acceptance criteria, the M5.c "done when" list in `specs/orun-state-redesign/implementation-plan.md`, and the broader Verifier Standard from `agents/orchestrator.md` (sections 349–417). Confirm read-only scope discipline. Confirm both PR CI checks PASS at log level on the final head SHA. If PASS, merge PR #163, fast-forward `main`, and leave the local repo clean. If FAIL, leave the PR open with explicit blockers.

## PR Boundary (must match Task 0019 implementer)

- Read-only consumers in `cmd/orun/` only: `command_status.go`, `command_logs.go`, `command_describe.go`, `command_get.go`, plus the two new helper files (`read_resolve.go`, `bridge_mirror_warn.go`) and a single new test file (`command_read_revision_test.go`).
- No writer/runner/executor/executionstate-writer/revision-writer/statestore edits. No spec behavioral changes. No new event kinds. No `--persist-revision`. No `orun state migrate` (M5.d). No TUI changes.

If the PR diff strays outside this boundary, FAIL on overreach.

## Read First

- `agents/orchestrator.md` — Verifier Standard (sections 349–392) and Verifier Merge Protocol (sections 393–417).
- `ai/tasks/task-0019.md` — original implementer prompt.
- `ai/reports/task-0019-implementer.md` — implementer claims.
- `specs/orun-state-redesign/implementation-plan.md` §M5 (esp. §M5.c done-when).
- `specs/orun-state-redesign/cli-surface.md` §3, §4, §5, §6, §9.
- `specs/orun-state-redesign/compatibility-and-migration.md` §1, §3, §4 (canonical resolution chain + reader fallback).
- `specs/orun-state-redesign/data-model.md` §3, §4, §6, §7, §9 (esp. §9.1 `bridge-mirror-failed` payload schema).
- `specs/orun-state-redesign/test-plan.md` §1, §3, §8.
- PR #163 diff: `gh pr diff 163` and `gh pr view 163 --json …`.

## Required Outcomes

- [ ] PR #163 maps to exactly Task 0019; no overreach.
- [ ] All four read commands (`status`, `logs`, `describe`, `get plans`) actually call into `executionstate.ResolveExecution` / `revision.ResolveRevision` (not duplicated fallback logic in CLI).
- [ ] Three-tier resolution ladder on `orun logs` matches `cli-surface.md` §4 exactly.
- [ ] New flags wired and exposed: `--revision <key>`, `--exec-id <key>`, `--all` on `status` / `logs` per cli-surface.md §3.2 / §4.
- [ ] `orun describe revision|trigger|execution …` aliases functional; existing `describe run …` continues to work via the new resolver with legacy fallback. Triplet (revisionKey + executionKey + legacyExecID) appears in describe output where applicable.
- [ ] `orun get plans` renders revision-first table when revisions exist, legacy plan-hash table otherwise; `--json` returns stable-keyed structured output with trailing newline.
- [ ] `bridge-mirror-failed` events surface as one-line stderr warnings (one per distinct execution); malformed `events/` directories degrade silently; commands never block or exit non-zero on the event channel.
- [ ] Coverage gates held on final head SHA: `internal/statestore` ≥ 95 %, `internal/revision` ≥ 90 %, `internal/executionstate` ≥ 90 %.
- [ ] Local quality gates green: `go build ./...`, `go vet ./...`, `go test -race ./...`, `make test-state-redesign`.
- [ ] PR CI checks PASS at log level on final head SHA: `CI / Orun Plan` and `Harness dry-run guard`. Confirm by `gh run view <id> --log` (not just status summary).
- [ ] No secrets / tokens / user emails in any changed log path.
- [ ] No `time.Now()` direct calls introduced; clock seam preserved.
- [ ] Errors flow through `errors.Is` / `errors.As`.

## Non-Goals

- No verifier-side feature work. Verifier may add a verifier report and, if essential, one tiny verification-only fix; otherwise the PR branch should not be modified.
- Do not edit specs unless filing a proposal under `ai/proposals/task-0019-spec-update.md` for genuine drift.
- Do not start M5.d (`orun state migrate`).

## Verification Steps

1. **Read implementer report and diff.** `gh pr view 163 --json …` (already cached in this prompt's Current Repo Context). `gh pr diff 163` for the full patch. Confirm files match the boundary above.
2. **Scope-discipline scan.** Confirm zero edits under `internal/runner/`, `internal/runbundle/`, `internal/executor/`, `internal/executionstate/` writer paths, `internal/revision/` writer paths, `internal/statestore/`, or `internal/state/` (other than read-only API consumption surfaced through `read_resolve.go`).
3. **Local checkout + walks.**
   ```
   git fetch origin pull/163/head:verify/task-0019
   git checkout verify/task-0019
   go build ./...
   go vet ./...
   go test -race ./...
   make test-state-redesign
   ```
   Expect statestore ≥ 95 %, revision ≥ 90 %, executionstate ≥ 90 %.
4. **End-to-end CLI walk** in a fresh temp workspace:
   ```
   orun plan && orun run && orun status && orun logs && orun describe revision latest
   ```
   Confirm output shape matches the M5.c §Acceptance Demo / `cli-surface.md` shape.
5. **Legacy-fallback walk.** Synthesize a workspace containing only `.orun/executions/<id>/` (no `revisions/`) and run `orun status`, `orun logs`, `orun describe run latest`, `orun get plans`. Confirm transparent fallback, no panics.
6. **`bridge-mirror-failed` surfacing.** Drop a hand-crafted `events/<seq>-bridge-mirror-failed.json` (matching `data-model.md` §9.1 schema) into a temp execution directory; run each of the four read commands; confirm a single one-line stderr warning per distinct execution, no blocking, no exit-code change. Then drop a malformed payload; confirm silent degradation.
7. **New flags + aliases.** Exercise `--revision`, `--exec-id`, `--all` on `status` and `logs`; exercise `describe revision <key>`, `describe trigger <name>`, `describe execution <key>`. Confirm triplet (revisionKey, executionKey, legacyExecID) is present in describe output.
8. **`orun get plans --json`.** Confirm stable JSON key order and trailing newline. Confirm legacy fallback when no revisions exist.
9. **`kiox -- orun validate / plan / run --dry-run`.** Run the three-step Orun walk in `examples/`. Record the persistent composition-cache quirk if it reproduces (CI is authoritative). The verifier report's "Local Resource Evidence" section should explicitly note this if observed.
10. **PR CI log review.** Use `gh run view <run-id> --log` (NOT just `--json conclusion`) for both required checks on the final head SHA. Confirm the expected commands actually executed (no skip-by-condition silent passes). Confirm no logged secrets.
11. **Spec drift.** If you observe behavior that does not match `cli-surface.md` / `compatibility-and-migration.md` / `data-model.md` §9.1 exactly, file `ai/proposals/task-0019-spec-update.md` and decide PASS-with-followup vs FAIL.

## Acceptance Criteria

✅ Diff stays within PR Boundary above (read-only `cmd/orun/*` + the two new helpers + the new test + `ai/reports/task-0019-implementer.md`).
✅ All five M5.c "done when" items satisfied.
✅ All checks in Verification Steps green at log level.
✅ Coverage gates preserved on `internal/statestore`, `internal/revision`, `internal/executionstate`.
✅ Both required PR CI checks PASS on final head SHA per `gh run view --log`.
✅ Repo health green; no spec proposals filed unless genuine drift detected.
✅ MergeStateStatus stays `CLEAN` through final push.

If any check fails, FAIL the verification and leave PR #163 open with explicit blockers in the verifier report.

## Verifier Merge Protocol (per `agents/orchestrator.md`)

- If PASS:
  1. Optionally commit the verifier report to the PR branch (`git checkout impl/task-0019-m5c-orun-read-commands-rewire && git add ai/reports/task-0019-verifier.md && git commit -m "Task 0019 verifier report" && git push`); wait for CI to re-run if you push.
  2. `gh pr merge 163 --squash --delete-branch`.
  3. `git checkout main && git pull --ff-only origin main`.
  4. `git status --short` — resolve any verifier-created local changes before ending the verifier task.
  5. Update `ai/state.json` (`completed` += "0019", advance `current_task` to "0020", `task_agent` → `ai/reports/task-0019-verifier.md`, `next_focus` → M5.d), `ai/context/current.md`, `ai/context/task-ledger.md`, `ai/waiting_for_input.md`.
- If FAIL: leave PR #163 open. Do not merge. Document blockers in the verifier report and `ai/waiting_for_input.md` if human intervention is needed; otherwise update `ai/state.json` notes and let the orchestrator route a follow-up implementer fix-task on the same PR/branch.
- Never merge a PR with unresolved verification blockers or failing CI on final head SHA.

## When Done Report

Save to `ai/reports/task-0019-verifier.md` with sections (concise — see `agents/orchestrator.md` budget):

- Result: PASS | FAIL
- Checks (each verification step + outcome)
- Issues (any, with severity)
- CI Log Review (run IDs + log-level evidence for `CI / Orun Plan` and `Harness dry-run guard`)
- Coverage Evidence (statestore / revision / executionstate %)
- Live Resource Evidence (the temp-workspace walk results)
- Secret Handling Review (confirmation of no exposure)
- Risk Notes (residual risk; carry forward the M5.b risk register, plus anything new)
- Spec Proposals (links + one-line reason; "none" is acceptable)
- Recommended Next Move (expected: emit M5.d implementer task to close M5)
