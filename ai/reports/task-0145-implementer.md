# Task 0145 — Implementer Report

## Summary

Superseded PR #142 with a clean, focused PR that preserves only the
task-scoped GitHub CLI UX work from PR #142 commit `ddbec4c` and adds
the direct unit coverage Task 0142's verifier flagged as missing.

The successor PR contains exactly four product files plus this report:

- `cmd/orun/command_github.go` — `--orun-dir` normalization (factored
  into a small `normalizeOrunDir()` helper) and `github status`
  selector flag registration.
- `cmd/orun/command_github_test.go` — five new focused tests covering
  the two `--orun-dir` cases Task 0142 called out (parent input
  resolves to `<parent>/.orun`; an already-`.orun` path is unchanged),
  empty-input defaulting, plus `github status` selector flag
  registration and cobra parse-time acceptance.
- `website/docs/cli/orun-github.md` — public docs updated to match the
  final CLI behavior (`--orun-dir` semantics, status selectors,
  resolution order, `--job` substring-match note, full-SHA requirement).
- `docs/github-log-pull-ux-review.md` — short UX-review note that
  directly motivates this exact CLI/docs fix. No roadmap promises, no
  TUI/orchestrator scope creep (verified: only a passing visual
  reference to the existing TUI renderer).

No files from PR #142's unrelated scope were carried over: the TUI
cockpit specs, `orun-tui-cockpit.md`, `agents/orchestrator.md`,
historical task prompts, stale `ai/waiting_for_input.md`, and the
`examples/apps/api-edge/component.yaml` dummy trigger are all absent
from this PR's diff.

## PR Number and URL

- PR: #144
- URL: https://github.com/sourceplane/orun/pull/144

## Branch / base

- Head: `impl/task-0145-github-cli-pr142-supersede`
- Base: `main` (at `960e2a5` chore(orchestrator): scope task 0145…)

## Files Changed

```
cmd/orun/command_github.go
cmd/orun/command_github_test.go
docs/github-log-pull-ux-review.md
website/docs/cli/orun-github.md
ai/reports/task-0145-implementer.md
```

Commits on branch:

1. `e9df9bf fix(github): consistent --orun-dir semantics; register
   status flags` — cherry-picked from PR #142 commit `ddbec4c` (sole
   useful commit on `happy-patch-113`).
2. `e4cf360 test(github): cover --orun-dir normalization and status
   selectors` — refactor `--orun-dir` resolution into
   `normalizeOrunDir()` (zero behavioral change) plus five new focused
   tests.
3. `<report commit>` — this report.

## PR #142 Disposition

PR #142 (`happy-patch-113`, title `chore: update happy-patch-113`)
was closed as superseded by the successor PR. See "CI Status /
Re-run Evidence" below for the exact `gh pr close` invocation and the
resulting `state` returned by `gh pr view 142`.

## Scope Excluded From PR #142

The following PR #142 content was deliberately NOT carried into the
successor PR, per Task 0145 scope and the orchestrator's blocker list:

- `.kiro/specs/orun-tui-cockpit/**` — already landed on `main` via
  Task 0144.
- `orun-tui-cockpit.md` — duplicate of merged content on `main`.
- `agents/orchestrator.md` — process doc unrelated to CLI UX.
- `ai/tasks/task-0139-verifier.md`, `ai/tasks/task-0140.md`,
  `ai/tasks/task-0140-verifier.md`, `ai/tasks/task-0141-verifier.md`
  — historical orchestration prompts.
- `ai/waiting_for_input.md` — stale orchestrator state.
- `examples/apps/api-edge/component.yaml` `trigger:
  pr-142-dummy-change` — dummy CI trigger label.

## Checks Run

All commands run from repo root on `impl/task-0145-github-cli-pr142-supersede`.

```
$ go test ./cmd/orun/ -run 'TestGithub(Status|Pull|Logs|Runs)|TestGithubCommand|TestNormalizeOrunDir' -v -count=1
=== RUN   TestGithubCommandRegistered                     --- PASS
=== RUN   TestGithubCommandHasSubcommands                 --- PASS
=== RUN   TestGithubRunsFlagsRegistered                   --- PASS
=== RUN   TestGithubPullFlagsRegistered                   --- PASS
=== RUN   TestGithubLogsFlagsRegistered                   --- PASS
=== RUN   TestGithubStatusCommandRegistered               --- PASS
=== RUN   TestGithubCommandRunsHelp                       --- PASS
=== RUN   TestGithubPullOrunDirDefaultResolvesToDotOrun   --- PASS
=== RUN   TestGithubPullOrunDirWithIntentRoot             --- PASS
=== RUN   TestGithubLogsPrintsLogContent                  --- PASS
=== RUN   TestGithubLogsSkipsNonLogEntries                --- PASS
=== RUN   TestGithubLogsWarnsOnUnreadableFile             --- PASS
=== RUN   TestGithubLogsMultipleSteps                     --- PASS
=== RUN   TestGithubLogsPathTraversalBlocked              --- PASS
=== RUN   TestGithubRunsDetailsFlag                       --- PASS
=== RUN   TestGithubRunsLevel1NoDownload                  --- PASS
=== RUN   TestGithubLogsJobFilterNoMatch                  --- PASS
=== RUN   TestNormalizeOrunDirParentBecomesDotOrun        --- PASS
=== RUN   TestNormalizeOrunDirAlreadyDotOrunUnchanged     --- PASS
=== RUN   TestNormalizeOrunDirEmptyDefaultsToDotOrun      --- PASS
=== RUN   TestGithubStatusSelectorFlagsRegistered         --- PASS
=== RUN   TestGithubStatusAcceptsSelectorFlagsAtParseTime --- PASS
PASS
ok  github.com/sourceplane/orun/cmd/orun  1.089s

$ go test ./internal/artifactstore/github/... -count=1
ok  github.com/sourceplane/orun/internal/artifactstore/github  20.671s

$ go test ./internal/runbundle/... -count=1
ok  github.com/sourceplane/orun/internal/runbundle  1.436s

$ go test ./cmd/orun/... -count=1
ok  github.com/sourceplane/orun/cmd/orun  8.746s

$ go build ./cmd/orun/
(no output; exit 0)
```

Diff-scope guard:

```
$ git diff --name-only main
cmd/orun/command_github.go
cmd/orun/command_github_test.go
docs/github-log-pull-ux-review.md
website/docs/cli/orun-github.md

$ git diff --name-only main | grep -E '^(\.kiro/specs/orun-tui-cockpit/|orun-tui-cockpit\.md|agents/orchestrator\.md|ai/tasks/task-0139-verifier\.md|ai/tasks/task-0140\.md|ai/tasks/task-0140-verifier\.md|ai/tasks/task-0141-verifier\.md|ai/waiting_for_input\.md|examples/apps/api-edge/component\.yaml)$' && echo BLOCKER || echo "no blockers"
no blockers
```

Dummy-trigger guard (matches only in historical orchestrator state /
verifier reports / task prompts, not in product code):

```
$ git grep -n 'pr-142-dummy-change' -- . ':!ai/reports/task-0142-verifier.md' ':!ai/tasks/task-0143.md' ':!ai/tasks/task-0145.md'
ai/context/task-ledger.md:74:  (historical entry from Task 0142)
ai/context/task-ledger.md:84:  (historical entry from Task 0143)
ai/context/task-ledger.md:108: (historical entry from Task 0145 scoping)
ai/state.json:23: (acceptance criterion text only)
ai/tasks/task-0142-verifier.md:130: (historical task prompt)
```

Orun validation: not applicable — there is no root `intent.yaml` in
this repo checkout (this repo is the Orun CLI source, not an Orun
consumer). The acceptance-criteria block already documents this
fallback ("No root intent.yaml; Orun validation not applicable").

## CI Status / Re-run Evidence

PR #144 — `mergeStateStatus: CLEAN` after Orun Plan completed.

```
$ gh pr view 144 --json mergeStateStatus,statusCheckRollup
mergeStateStatus: CLEAN
  CI/Orun Plan:                                          status=COMPLETED conclusion=SUCCESS
  orun remote-state conformance/Harness dry-run guard:   status=COMPLETED conclusion=SUCCESS
  CI/${{ matrix.component }}/${{ matrix.env }}:          status=COMPLETED conclusion=SKIPPED
  orun remote-state conformance/Compile plan:            status=COMPLETED conclusion=SKIPPED
  orun remote-state conformance/Run: matrix.job:         status=COMPLETED conclusion=SKIPPED
  orun remote-state conformance/Env fanout:              status=COMPLETED conclusion=SKIPPED
  orun remote-state conformance/Verify remote status:    status=COMPLETED conclusion=SKIPPED
```

The two non-skipped checks (`CI/Orun Plan` and `orun remote-state
conformance/Harness dry-run guard`) both PASS. The remaining matrix
checks are SKIPPED because no Orun component subscribes to the PR
trigger's activated environments — expected for a CLI/docs-only PR
that touches no Terraform components. No check is queued or unknown.

PR #142 disposition confirmed:

```
$ gh pr view 142 --json state,title,url
state: CLOSED
title: chore: update happy-patch-113
url:   https://github.com/sourceplane/orun/pull/142
```

Closure comment posted on #142 links #144 as the supersession target.

## Assumptions

1. The orchestrator's instruction to "create a successor branch from
   current `main` instead of force-repairing `happy-patch-113`" makes
   `happy-patch-113` itself out of scope for direct edits; we
   selectively cherry-pick `ddbec4c` only and let PR #142 be closed.
2. Factoring `--orun-dir` resolution into `normalizeOrunDir()` is
   acceptable as a tiny, behavior-preserving refactor because it is
   the only way to give the two Task-0142-flagged cases direct unit
   coverage at the helper level (the previous tests only exercised
   the default-resolves-to-.orun path via `filepath.Join(storeDir(),
   state.OrunDir)`).
3. `docs/github-log-pull-ux-review.md` qualifies under the "optional"
   clause in Task 0145 scope: it is short (176 lines), directly
   explains the UX motivation for the exact `--orun-dir` / status /
   logs / pull fix in this PR, and contains no roadmap promises or
   cross-cutting orchestrator/TUI scope (one passing reference to the
   existing TUI renderer in a UX comparison table is unavoidable and
   not a roadmap claim).

## Risks / Follow-ups

- The cherry-picked commit `ddbec4c` retains the original author
  attribution from PR #142, which is desirable for credit but means
  the successor PR has a mixed-author commit history. This is normal
  for cherry-picks and not a merge blocker.
- The `normalizeOrunDir()` helper centralizes resolution; if future
  work introduces additional `--orun-dir`-accepting subcommands (e.g.
  a hypothetical `orun github hydrate` standalone), they should call
  the same helper rather than re-implementing the parent-vs-`.orun`
  branching.
- No follow-up tasks are required for the `github status` selector
  flag fix beyond standard verifier signoff.
