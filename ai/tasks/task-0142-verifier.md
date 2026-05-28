# Task 0142

Agent: Verifier

## Current Repo Context

- Task 0141.1 verified PASS and merged PR #141. GitHub Artifacts Requirement 11 is complete: `orun github runs --details` now downloads manifest-only data and prints Level 2 shard detail.
- An unplanned PR is now open: PR #142 (`happy-patch-113`, title `chore: update happy-patch-113`). It contains GitHub CLI fixes from a walkthrough plus large orchestration/spec files and a dummy component change.
- PR #142 is currently the only open PR and must be stabilized before the orchestrator creates new implementation work. This verifier task exists because the orchestrator must generate a verifier when a PR is open and must not skip an unverified, potentially scope-broad PR.

## Objective

Verify PR #142 against the Orchestrator Verifier Standard and decide PASS or FAIL. The primary goal is to protect `main`: confirm whether the PR is a coherent, reviewable GitHub Artifacts UX follow-up, whether CI is green, whether its code changes are correct and tested, and whether unrelated orchestration/TUI/spec/dummy files should block merge.

## PR Boundary

Verifier scope is PR #142 only:

1. Code behavior under review:
   - `cmd/orun/command_github.go` normalizes `orun github pull --orun-dir` as a working directory whose `.orun/` child is used, while accepting paths already ending in `.orun`.
   - `orun github status` registers the same resolution flags used by `pull`/`logs`: `--run-id`, `--exec-id`, `--sha`, `--branch`, `--latest`, `--failed`.
2. Documentation under review:
   - `website/docs/cli/orun-github.md` documents the changed `--orun-dir` semantics, full-SHA caveat, status flags/examples, resolution order, and `--job` matching caveat.
   - `docs/github-log-pull-ux-review.md` records the walkthrough and follow-up friction list.
3. Scope-risk files under review:
   - `.kiro/specs/orun-tui-cockpit/**`, `orun-tui-cockpit.md`, `agents/orchestrator.md`, historical `ai/tasks/**`, `ai/waiting_for_input.md`, and `examples/apps/api-edge/component.yaml` are included in PR #142 but are not obviously part of the GitHub CLI UX fix. The verifier must classify these as acceptable context/docs, required follow-up cleanup, or merge blockers.

No new implementation scope should be added by the verifier except verification-only report/state updates or tiny task-scoped fixes required for the PR to pass.

## Read First

- `agents/orchestrator.md` — Verifier Standard and Verifier Merge Protocol (sections 331-372).
- PR #142 diff and commits:
  - `gh pr view 142 --json number,title,body,headRefName,baseRefName,state,isDraft,mergeStateStatus,statusCheckRollup,commits,files,url`
  - `gh pr diff 142`
- `docs/github-log-pull-ux-review.md` — walkthrough findings that motivated the code/doc changes.
- `website/docs/cli/orun-github.md` — public docs changed by this PR.
- `cmd/orun/command_github.go` and `cmd/orun/command_github_test.go` — changed CLI behavior and test coverage.
- `.kiro/specs/github-artifacts/requirements.md` — Requirements 10, 11, 14, 17, 20, 21 for GitHub artifact inspection context.
- `ai/context/current.md`, `ai/context/task-ledger.md`, `ai/state.json` — current orchestration state before verification.

## Required Outcomes

- [ ] Verify PR #142's code behavior with local tests and code-path inspection.
- [ ] Inspect CI logs for PR #142, not only check summaries. Do not merge unless all required CI checks have passed.
- [ ] Decide whether PR #142's large unrelated-looking file set is acceptable or must be cleaned up before merge.
- [ ] If verification PASS: merge PR #142, checkout/sync `main`, leave the local repo clean, and write `ai/reports/task-0142-verifier.md` on `main` with evidence.
- [ ] If verification FAIL: leave PR #142 open, write `ai/reports/task-0142-verifier.md` with exact blockers and recommended changes.
- [ ] Update orchestration state files consistently with the result.

## Non-Goals

- Do not implement new GitHub CLI UX features beyond PR #142's submitted changes.
- Do not start TUI cockpit Phase 1.
- Do not implement structured `--job` matching, zero-job status messaging, short-SHA expansion, table formatting, pull progress, or logs banners unless they are already in PR #142 and need verification.
- Do not rewrite the TUI cockpit spec pack during verification.
- Do not merge if CI is queued, failing, cancelled, or unknown.

## Constraints

1. The verifier must follow `agents/orchestrator.md` sections 331-372: inspect prompt + PR + report if present, validate acceptance criteria, inspect CI logs, PASS/FAIL, and merge only on PASS plus green CI.
2. Treat unplanned scope seriously. PR #142 contains code changes plus large orchestration/spec additions and a dummy component label change; the verifier must explicitly explain why each category is acceptable or blocking.
3. Do not rely on the PR body; it only says `Created by rh-ghflow` and is not a useful implementer report.
4. If the implementer report is missing, record that as a process issue. It is a blocker only if evidence cannot otherwise establish scope, tests, and intent.
5. Secret safety: do not print GitHub tokens, signed artifact URLs, bearer headers, or raw credentials in the verifier report.
6. Leave the local repository clean at the end. If you create a verifier report/state updates, commit them to the appropriate branch according to PASS/FAIL handling.

## Acceptance Criteria

✅ PR #142 code review confirms `--orun-dir` normalization is correct:
- Default `.` resolves to `./.orun`.
- `/tmp/foo` resolves to `/tmp/foo/.orun`.
- `/tmp/foo/.orun` remains `/tmp/foo/.orun` for back-compat.
- The value passed to `runbundle.Hydrate` matches the documented semantics.

✅ PR #142 code review confirms `orun github status` has the six expected resolution flags registered and wired to `runGithubStatus`'s existing resolver inputs.

✅ Local tests pass, at minimum:
```bash
go test ./cmd/orun/ -run 'TestGithub(Status|Pull|Logs|Runs)|TestGithubCommand' -v
go test ./internal/artifactstore/github/... -v
go test ./internal/runbundle/... -v
go test ./cmd/orun/... -v
```

✅ Build passes:
```bash
go build ./cmd/orun/
```

✅ Orun validation is handled truthfully:
- If no root `intent.yaml` exists, record N/A.
- If changed component files make an intent available, run the appropriate `/Users/irinelinson/.local/bin/kiox -- orun validate --intent <path>` and/or explain why it is not applicable.

✅ PR CI logs are inspected and all required checks are green. If CI remains queued or unstable, FAIL or defer merge; do not merge.

✅ Scope decision is explicit:
- PASS only if the verifier concludes the spec/orchestration files and dummy component change are intentional, non-harmful, and acceptable in the same PR.
- FAIL if the PR should be narrowed, retitled/re-described, split, or have dummy/unrelated files removed before merge.

✅ Secret safety review confirms no credentials, signed URLs, or token-bearing strings are introduced in changed files or logs.

## Verification

Suggested sequence:

1. Confirm branch and PR state:
```bash
git status --short --branch
gh pr view 142 --json number,title,state,isDraft,mergeStateStatus,statusCheckRollup,commits,files,url
```

2. Inspect changed files and diffs:
```bash
gh pr diff 142 --name-only
gh pr diff 142 -- cmd/orun/command_github.go cmd/orun/command_github_test.go website/docs/cli/orun-github.md docs/github-log-pull-ux-review.md examples/apps/api-edge/component.yaml
```

3. Run local tests/build listed in Acceptance Criteria.

4. Inspect CI logs:
```bash
gh run view <run-id> --log
```
Look for the actual commands run and their results, especially `go test`, `orun plan`, and dry-run guard output.

5. Perform scope/overreach review:
- Determine whether `.kiro/specs/orun-tui-cockpit/**`, `orun-tui-cockpit.md`, and `agents/orchestrator.md` should be in PR #142 or split.
- Determine whether historical task prompts under `ai/tasks/` and `ai/waiting_for_input.md` are acceptable to land now.
- Determine whether `examples/apps/api-edge/component.yaml` label `trigger: pr-142-dummy-change` is a temporary CI trigger that must be removed before merge.

6. Write `ai/reports/task-0142-verifier.md` with Result PASS/FAIL, Checks, PR/CI Evidence, Code Review Notes, Scope/Overreach Review, Secret Safety Review, Issues, Risk Notes, and Recommended Next Move.

7. If PASS and CI is green: merge PR #142, sync `main`, then update state files/report on `main` and leave repo clean. If FAIL: leave PR open and make sure blockers are clear.

## PR Creation Requirement

The Implementer has already created PR #142. The Verifier must not create a new implementation PR for the same work unless PR #142 is explicitly failed and a cleanup/fix PR is required by the orchestrator.

## When Done Report

Write `/ai/reports/task-0142-verifier.md` with:

- `## Result: PASS` or `## Result: FAIL`
- `## Checks`
- `## PR/CI Evidence`
- `## Code Review Notes`
- `## Scope/Overreach Review`
- `## Secret and Safety Review`
- `## Issues`
- `## Risk Notes`
- `## Recommended Next Move`
- `## PR Number`
