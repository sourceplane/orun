# Task 0147.1

Agent: Verifier

## Current Repo Context

- Task 0147 was scoped as the first TUI Cockpit Phase 3 execution slice: local dry-run `LiveOrunService.RunPlan`, Plan Studio `d` dry-run transition, and a minimal Run Dashboard event timeline.
- Implementer opened PR #146: `feat(tui): wire Plan Studio dry-run via LiveOrunService.RunPlan (Task 0147)` from branch `impl/task-0147-tui-dryrun` at head `59d21029bc92ed7fb7fae55e5cb7540bb2cd32ae`.
- PR #146 is open, not draft, and mergeStateStatus is `CLEAN`. CI status at orchestration time: CI / Orun Plan succeeded in run `26611755239`; orun remote-state conformance / Harness dry-run guard succeeded in run `26611755245`; matrix jobs were skipped as expected for this non-component diff.
- Important verifier pre-check: the PR branch currently does not contain `ai/reports/task-0147-implementer.md`. The verifier must require/commit an implementer report with the real PR number before PASS. If the report cannot be obtained or authored from PR evidence, verification must FAIL and leave PR #146 open.

## Objective

Verify PR #146 against Task 0147, the implementer evidence, `.kiro/specs/orun-tui-cockpit` Requirements 4.5, 5.5, 6.1-6.4, 6.9, 12.5-12.6, 13.2, 13.4-13.5, and 14.8, plus the Verifier Standard in `agents/orchestrator.md`. If and only if local verification, implementer-report completeness, PR CI/log inspection, and code-path review all PASS, merge PR #146, sync local `main`, and leave the repo clean.

## PR Boundary

The verifier must keep review scoped to the exact Task 0147 boundary:

1. `LiveOrunService.RunPlan` supports local dry-run only, calls `internal/runner.Runner` directly, and emits `services.RunEvent` values through the existing channel contract.
2. Plan Studio review key `d` starts `RunPlan(DryRun: true)` from the current generated plan and transitions to Run Dashboard only after run startup succeeds.
3. `RunViewModel` accumulates/renders a minimal streaming event timeline and stops re-arming after `RunEventRunDone`.
4. Focused service/root/view tests cover dry-run event streaming, fail-closed unsupported modes, Plan Studio transition/error behavior, and dashboard completion behavior.

No scope expansion: do not require real apply/destroy execution, remote-state execution or polling, full Log Explorer, History/Replay, command-palette completion, Phase 4 features, or GitHub CLI UX follow-ups.

## Read First

- `agents/orchestrator.md` — Verifier Standard and Verifier Merge Protocol, especially lines 331-372.
- `ai/tasks/task-0147.md` — original implementer contract and acceptance criteria.
- `ai/reports/task-0147-implementer.md` — implementer report. If missing from PR #146 branch, treat report completion as a required verifier precondition/fix before PASS.
- `.kiro/specs/orun-tui-cockpit/requirements.md` — Requirements 4.5, 5.5, 6.1-6.4, 6.9, 12.5-12.6, 13.2, 13.4-13.5, and 14.8.
- `.kiro/specs/orun-tui-cockpit/design.md` — §5 Event / Message Flow, §6 TUI Data Models (`RunEvent`), §7 key binding map, and service layer architecture.
- `.kiro/specs/orun-tui-cockpit/tasks.md` — Phase 3 tasks 18-20 and checkpoint 25; treat stale `github.com/flyingmutant/rapid` references as non-blocking unless code reintroduces that import.
- PR #146 diff, commits, status checks, and GitHub Actions logs.

## Required Outcomes

- [ ] Inspect PR #146 diff, commits, status checks, and implementer evidence.
- [ ] Ensure `ai/reports/task-0147-implementer.md` exists on the PR branch and contains the real PR number `#146`; if missing, commit a verifier-created report completion/fix to the PR branch or FAIL with a blocker.
- [ ] Confirm PR #146 maps to exactly Task 0147 and does not include unrelated scope.
- [ ] Run the local validation commands listed below.
- [ ] Inspect CI logs with `gh run view`, including successful jobs, and confirm expected commands actually ran.
- [ ] Inspect code paths for dry-run-only safety, no `exec.Command`, no `os/exec`, no subprocess invocation of `orun` under `internal/tui/**`, no accidental destructive execution path, cancellation-aware channel closure, and no duplicate/dropped terminal run behavior.
- [ ] Verify tests cover service validation, dry-run stream closure/done sentinel, Plan Studio `d` dispatch/error behavior, and Run Dashboard done/no-rearm behavior.
- [ ] Write `ai/reports/task-0147-verifier.md` with Result PASS or FAIL and evidence.
- [ ] If PASS and CI is green, merge PR #146, checkout `main`, fast-forward pull from `origin/main`, and leave `git status --short` clean.
- [ ] If FAIL, leave PR #146 open and document precise blockers in the verifier report.

## Non-Goals

- Do not implement Phase 3 follow-up features beyond tiny verifier-only report/state corrections: real execution, remote-state polling, Log Explorer, History/Replay, command palette, and Phase 4 features remain future tasks.
- Do not broaden the PR into TUI visual redesign or full DAG/timeline polish.
- Do not change public CLI runner semantics unless a tiny fix is strictly required to preserve Task 0147 acceptance; otherwise FAIL and send back to implementer.
- Do not silently accept a missing implementer report or a placeholder `PR Number: TBD` report.

## Constraints

1. PR #146 may merge only when both local verification and PR CI/log inspection are acceptable.
2. The TUI service layer must call Orun internals directly; shelling out to `orun` from `internal/tui/**` is a blocker.
3. Task 0147 is dry-run only. Any path that allows destructive TUI execution (`DryRun:false`, real apply/destroy, or remote-state execution) is a blocker.
4. Unsupported modes must fail closed with clear errors: nil plan, `DryRun:false`, and `RemoteState:true` must not start a runner.
5. Run event sending must be cancellation-aware or non-blocking enough that runner hooks cannot deadlock if the UI stops reading.
6. Secret safety: verifier reports may mention file paths, job ids, component names, check ids, checksums, and run ids, but must not include credentials, tokens, signed URLs, or raw secret values.
7. If verification adds the verifier report, missing implementer report, or small report-only corrections to the PR branch, push them and wait for PR CI to be green again before merging.

## Acceptance Criteria

✅ PR #146 corresponds exactly to Task 0147 and includes a committed `ai/reports/task-0147-implementer.md` with the real PR number.

✅ Local checks pass from repo root:

```bash
go test ./internal/tui/... -count=1
go test ./internal/runner/... -count=1
go test ./cmd/orun/... -count=1
go build ./cmd/orun/...
```

✅ Orun validation/dry-run checks pass or verifier records an exact accepted no-op/blocker:

```bash
/Users/irinelinson/.local/bin/kiox -- orun validate --intent intent.yaml
/Users/irinelinson/.local/bin/kiox -- orun plan --changed --intent intent.yaml --output plan.json
/Users/irinelinson/.local/bin/kiox -- orun run --plan plan.json --dry-run --runner github-actions
```

✅ Code inspection confirms `internal/tui/**` contains no `exec.Command`, no `os/exec`, and no subprocess invocation of `orun`.

✅ `LiveOrunService.RunPlan` is fail-closed for unsupported inputs and supported only for local dry-run requests with a non-nil plan.

✅ RunPlan emits at least job-start, terminal job, and `RunEventRunDone` behavior where the underlying runner provides jobs, closes the channel on completion/cancellation, and does not block forever.

✅ Plan Studio `d` dispatches dry-run only from Review with a generated plan; startup errors stay in Plan Studio with an error banner; Run Dashboard mode is entered only after successful `RunPlan` startup.

✅ Run Dashboard accumulates run events, renders useful job/timeline status, surfaces failed job error text, and stops issuing `WaitForRunEvent` after `RunEventRunDone`.

✅ GitHub Actions logs for PR #146 are inspected and show required CI success; mergeStateStatus remains CLEAN before merge.

✅ If PASS, PR #146 is merged and local `main` is synced and clean. If FAIL, PR #146 remains open with blockers.

## Verification Steps

1. Confirm repo and PR state:

```bash
git status --short
gh pr view 146 --json number,title,state,headRefName,baseRefName,mergeStateStatus,isDraft,statusCheckRollup,commits,files,url
```

2. Check report presence and scope:

```bash
git ls-tree -r origin/impl/task-0147-tui-dryrun --name-only ai/reports/task-0147-implementer.md
git diff --stat origin/main...origin/impl/task-0147-tui-dryrun
git diff --name-status origin/main...origin/impl/task-0147-tui-dryrun
git diff origin/main...origin/impl/task-0147-tui-dryrun -- internal/tui ai/tasks/task-0147.md ai/reports/task-0147-implementer.md go.mod go.sum
```

3. Inspect safety-sensitive code paths:

```bash
grep -RInE 'exec\.Command|os/exec|"orun"' internal/tui || true
git diff origin/main...origin/impl/task-0147-tui-dryrun -- internal/tui/services/run_service.go internal/tui/model.go internal/tui/views/plan_studio.go internal/tui/views/run_view.go internal/tui/events/run_events.go
```

4. Run local checks:

```bash
go test ./internal/tui/... -count=1
go test ./internal/runner/... -count=1
go test ./cmd/orun/... -count=1
go build ./cmd/orun/...
/Users/irinelinson/.local/bin/kiox -- orun validate --intent intent.yaml
/Users/irinelinson/.local/bin/kiox -- orun plan --changed --intent intent.yaml --output plan.json
/Users/irinelinson/.local/bin/kiox -- orun run --plan plan.json --dry-run --runner github-actions
```

5. Inspect CI logs, not just status summaries:

```bash
gh run view 26611755239 --json name,conclusion,status,jobs
gh run view 26611755245 --json name,conclusion,status,jobs
gh run view 26611755239 --log --job 78418978435
gh run view 26611755245 --log --job 78418978299
```

If the verifier pushes report-only fixes, re-read the latest PR status checks and inspect the new run logs before merging.

6. Write `ai/reports/task-0147-verifier.md` with the mandatory sections below.
7. If PASS, merge per `agents/orchestrator.md` Verifier Merge Protocol. If FAIL, leave PR #146 open.

## When Done Report

Write `/ai/reports/task-0147-verifier.md` with these sections:

- Result: PASS or FAIL
- Checks
- Issues
- CI Log Review
- Scope / Overreach Review
- Safety Review
- Secret Handling Review
- Spec Proposals
- Risk Notes
- Recommended Next Move
- PR Number
