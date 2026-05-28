# Task 0144.1

Agent: Verifier

## Current Repo Context

- The user explicitly redirected orchestration from the earlier PR #142 cleanup path to Orun TUI cockpit work. Task 0144 is the resulting Implementer task for the TUI Phase 1 foundation.
- PR #143 is open from branch `impl/task-0144-tui-foundation` into `main`: `Task 0144: Orun Cockpit TUI Phase 1 foundation`.
- Implementer report: `ai/reports/task-0144-implementer.md`. It claims the PR is ready for verifier, CI is green, all Go tests pass, `orun tui` is registered, remote-state validation fails closed, and `internal/tui` does not shell out to the `orun` CLI.
- PR #142 remains open and dirty from the earlier failed verifier path. Do not merge or repair PR #142 as part of this task. Verify only PR #143.
- Current local branch may already be `impl/task-0144-tui-foundation`; before merging, confirm the PR branch is up to date with GitHub and that local state is clean.

## Objective

Verify PR #143 against Task 0144, the TUI cockpit spec pack, and the Verifier Standard in `agents/orchestrator.md`. If verification PASSes and required CI/log inspection is acceptable, merge PR #143, sync local `main`, and leave the repo clean. If verification FAILs, leave PR #143 open and document precise blockers in `ai/reports/task-0144-verifier.md`.

## PR Boundary

The verifier must evaluate exactly the Task 0144 PR boundary:

1. TUI dependencies and Cobra command entry point:
   - pinned Charm dependencies in `go.mod` / `go.sum`;
   - `cmd/orun/command_tui.go` plus root registration;
   - `--remote-state` / `--backend-url` validation that aborts before Bubble Tea launch when no backend URL is configured.
2. `internal/tui` foundation:
   - app/model/keymap/theme structure;
   - service boundary under `internal/tui/services`;
   - minimal views/events required for a compiling read-only shell.
3. Read-only service slice:
   - `LiveOrunService.LoadWorkspace` uses internal Orun packages directly;
   - local `ListRuns` uses state store execution data;
   - single-shot local `TailLogs` or explicit conservative stubs for non-Phase-1 behavior.
4. Minimal Bubble Tea shell:
   - async workspace load on init;
   - loading/error states;
   - three-panel/status/key-hint rendering;
   - quit/reload/focus/help basics.
5. Tests/report/metadata:
   - focused TUI/Cobra/service/model tests;
   - `ai/reports/task-0144-implementer.md` with PR #143;
   - CI checks green and inspected.

Out of scope for this verifier:

- Do not require full Browse filters, dependency tree, Plan Studio, Run Dashboard, Log Explorer follow mode, History replay, remote cockpit polling, command palette execution, plan diff, failure workbench, or explain mode.
- Do not repair or close PR #142.
- Do not implement new code except a small verifier-only artifact or trivial metadata/report fix if needed. If code changes are required, mark FAIL or coordinate a fix on PR #143, then re-run CI before merge.

## Read First

- `ai/tasks/task-0144.md` — implementer contract and acceptance criteria.
- `ai/reports/task-0144-implementer.md` — implementer claims, checks, assumptions, and spec proposal note.
- `agents/orchestrator.md` — Verifier Standard and Verifier Merge Protocol, especially lines 331-372.
- `.kiro/specs/orun-tui-cockpit/requirements.md` — Requirements 1, 2, 9, 12, 13, and 14 for Phase 1 foundation.
- `.kiro/specs/orun-tui-cockpit/design.md` — System Architecture, Three-Panel Layout, Service Layer Architecture, Event / Message Flow, Data Models, Cobra Registration, Remote State Integration, Testing Strategy.
- `.kiro/specs/orun-tui-cockpit/tasks.md` — Phase 1 tasks 1-13 and known `rapid` module-path mismatch.
- PR #143 diff, commits, files, and CI logs:
  - `gh pr view 143 --json number,title,body,headRefName,baseRefName,state,isDraft,mergeStateStatus,statusCheckRollup,commits,files,url`
  - `gh pr diff 143 --name-only`
  - `gh run view 26608568355 --log`
  - `gh run view 26608568360 --log`

## Required Outcomes

- [ ] Determine PASS or FAIL for PR #143 with clear evidence.
- [ ] Verify PR #143 maps to Task 0144 and does not include PR #142 CLI repair work.
- [ ] Verify implementation uses Orun internals directly and does not shell out to the `orun` CLI from `internal/tui`.
- [ ] Verify `orun tui` command registration and remote-state fail-closed behavior.
- [ ] Verify focused tests/build and Orun validation pass locally or document exact blocker.
- [ ] Inspect GitHub Actions logs, not status summaries only, and confirm expected Orun plan/dry-run commands ran successfully where applicable.
- [ ] Verify secret safety: no credentials, raw tokens, signed URLs, or secret-bearing logs committed or printed in reports.
- [ ] Evaluate spec drift: the implementer report notes `github.com/flyingmutant/rapid` vs `pgregory.net/rapid`. Decide whether this is an acceptable compatibility note, requires a formal proposal, or requires a spec edit before merge.
- [ ] Write `ai/reports/task-0144-verifier.md`.
- [ ] If PASS and CI checks pass: merge PR #143, checkout `main`, fast-forward pull from `origin/main`, and leave `git status --short` clean.
- [ ] If FAIL: leave PR #143 open, do not merge, and document blockers and recommended next move.

## Constraints

1. Verification must trust repo/code reality over the implementer report. Inspect code paths directly.
2. Merge only if both local checks and required CI/log inspection are acceptable.
3. Never merge with unresolved verification blockers or failing/queued/unknown required checks.
4. Do not broaden the task into later TUI phases; Phase 1 foundation may contain explicit stubs for later behavior if they fail loudly and are documented.
5. If the verifier adds `ai/reports/task-0144-verifier.md` or a small verification-only fix before merge, commit it to PR #143, push, and wait for CI again before merging.
6. After a PASS merge, update local `main` and keep the working tree clean. Do not leave the task branch checked out.
7. Keep PR #142 explicitly out of scope. It remains a separate open-risk item unless the user directs otherwise.

## Acceptance Criteria

✅ PR identity and scope are correct:
```bash
gh pr view 143 --json number,title,state,isDraft,mergeStateStatus,headRefName,baseRefName,files
```
Expected: PR #143 is open, not draft, targets `main`, `mergeStateStatus` is acceptable before merge, and files are Task 0144 TUI/spec/orchestration/report files only.

✅ PR #143 does not contain PR #142 GitHub CLI repair files:
```bash
gh pr diff 143 --name-only | grep -E '^(cmd/orun/command_github.go|website/docs/cli/orun-github.md|docs/github-log-pull-ux-review.md|examples/apps/api-edge/component.yaml)$' && exit 1 || true
```
Expected: no matches.

✅ Command registration and help work:
```bash
go run ./cmd/orun --help | grep -iE 'tui|cockpit'
go run ./cmd/orun tui --help
```

✅ Remote-state validation fails before launch when no URL is configured:
```bash
env -u ORUN_BACKEND_URL go run ./cmd/orun tui --remote-state
```
Expected: non-zero exit and clear error similar to `--remote-state requires --backend-url or ORUN_BACKEND_URL`.

✅ TUI packages compile and focused tests pass:
```bash
go test ./internal/tui/... -count=1
go test ./cmd/orun/ -run 'Test.*Tui|Test.*TUI|TestRootCommand' -count=1
go build ./cmd/orun/
go test ./cmd/orun/... -count=1
go test ./internal/state/... ./internal/statebackend/... -count=1
```

✅ Optional broader regression if time permits or if focused tests reveal risk:
```bash
go test ./... -count=1
```

✅ TUI service layer does not shell out to `orun` subprocesses:
```bash
grep -R "exec.Command" internal/tui && exit 1 || true
grep -R '"orun"' internal/tui/services && exit 1 || true
```
Expected: no matches indicating subprocess invocation of the CLI.

✅ Orun validation remains healthy if `intent.yaml` exists:
```bash
/Users/irinelinson/.local/bin/kiox -- orun validate --intent intent.yaml
/Users/irinelinson/.local/bin/kiox -- orun plan --changed --intent intent.yaml --output plan.json
/Users/irinelinson/.local/bin/kiox -- orun run --plan plan.json --dry-run --runner github-actions
```
If the changed plan is a no-op, record that result.

✅ GitHub Actions logs are inspected:
```bash
gh run view 26608568355 --log | grep -E 'orun (validate|plan|run)|run --plan|plan --changed' || true
gh run view 26608568360 --log | grep -E 'Harness dry-run guard|orun|remote-state' || true
```
Expected: logs support the reported successful CI checks; no secret exposure.

✅ Secret scan / safety review:
```bash
git diff main...HEAD -- . ':(exclude)go.sum' | grep -Ei '(token|secret|password|credential|signed|authorization|api[_-]?key)' || true
```
Expected: no committed secret values; benign references in docs/code/report are acceptable when not values.

✅ Verifier Merge Protocol followed:
- PASS requires local checks + acceptable CI logs + no blockers.
- If PASS, merge PR #143 via `gh pr merge 143 --merge --delete-branch` or repository-appropriate merge method, then `git checkout main && git pull --ff-only origin main`.
- If FAIL, leave PR #143 open.

## Verification

Perform these steps in order:

1. Confirm clean local state and fetch latest refs:
   ```bash
   git status --short --branch
   git fetch origin
   gh pr checkout 143
   git status --short --branch
   ```
2. Review Task 0144 prompt, implementer report, and PR metadata/diff.
3. Inspect changed code paths manually:
   - `cmd/orun/command_tui.go`
   - `cmd/orun/commands_root.go`
   - `internal/tui/model.go`, `app.go`, `keymap.go`, `theme.go`
   - `internal/tui/services/*.go`
   - representative views/events tests.
4. Run the acceptance commands above.
5. Inspect CI logs for PR #143 runs, including successful jobs.
6. Check for overreach and spec drift. Specifically evaluate whether bundling `.kiro/specs/orun-tui-cockpit/**`, `agents/orchestrator.md`, `ai/tasks/task-0144.md`, and `orun-tui-cockpit.md` in the implementation PR is acceptable under the user's explicit pivot or should be split. Do not fail solely for this if it is needed to make Task 0144 self-contained, but record the rationale.
7. Write `ai/reports/task-0144-verifier.md` with the required sections below.
8. If PASS, commit the verifier report to PR #143 if repository practice requires it before merge, wait for CI if a commit is pushed, merge PR #143, sync `main`, and confirm `git status --short` is clean.
9. If FAIL, commit/push the verifier report only if appropriate for the PR branch; otherwise leave it local or open a follow-up as repo practice dictates, and do not merge.

## When Done Report

Write `/ai/reports/task-0144-verifier.md` with:

- Result: PASS or FAIL
- Summary
- PR / Branch / Merge Status
- Checks Run: exact commands and outcomes
- Code Path Review: command registration, service boundary, no-shell-out, remote-state fail-closed, model/view basics
- CI Log Review: run IDs, job names, conclusions, and whether expected commands appeared in logs
- Scope / Overreach Review: include PR #142 separation and TUI spec/orchestrator-doc bundling decision
- Secret Handling Review
- Spec Proposals: especially `rapid` module-path mismatch and whether a formal proposal/spec edit is required
- Issues: blockers vs non-blocking concerns
- Risk Notes
- Recommended Next Move
- Merge Evidence if PASS: merge commit, main HEAD, clean worktree proof
