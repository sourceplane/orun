# Task 0142 — Verifier Report

## Result: FAIL

PR #142 has correct, well-tested code changes for the GitHub CLI UX follow-up, but it
cannot be merged in its current form. Three independent blockers apply:

1. CI is not green. `Orun Plan` has been QUEUED since 2026-05-28T22:41 UTC and
   `mergeStateStatus = UNSTABLE`. The verifier task explicitly forbids merging on
   queued/unknown CI.
2. The PR ships a deliberate dummy label
   (`examples/apps/api-edge/component.yaml: trigger: pr-142-dummy-change`) that exists
   only to force a component re-evaluation in CI. It is not part of the GitHub Artifacts
   UX fix and must be removed (or the PR otherwise justified) before merge.
3. The PR has massive unrelated scope (3,760 additions across 15 files) — an entire
   TUI cockpit spec pack, an `agents/orchestrator.md` rewrite, four historical task
   prompts, and a stale `ai/waiting_for_input.md` — landing under a meaningless title
   (`chore: update happy-patch-113`) with no implementer report. This is precisely
   the "unplanned scope" the verifier task asked us to gate against.

## Checks

| Check                                                        | Result |
|--------------------------------------------------------------|--------|
| `git status` / branch identification                         | PASS — happy-patch-113 @ ddbec4c, clean of unrelated edits |
| `gh pr view 142` metadata                                    | OPEN, isDraft=false, mergeStateStatus=UNSTABLE |
| `go build ./cmd/orun/`                                       | PASS |
| `go test ./cmd/orun/ -run 'TestGithub(Status\|Pull\|Logs\|Runs)\|TestGithubCommand' -v` | PASS — 17/17 |
| `go test ./internal/artifactstore/github/...`                | PASS (14.9s) |
| `go test ./internal/runbundle/...`                           | PASS (cached) |
| `go test ./cmd/orun/...`                                     | PASS (9.5s) |
| `--orun-dir` normalization code review                       | PASS — matches documented semantics |
| `orun github status` flag registration code review           | PASS — all 6 flags wired to existing resolver inputs |
| `orun validate` (no root intent.yaml)                        | N/A — no root intent in repo |
| PR CI required checks all green                              | FAIL — `Orun Plan` QUEUED ~24h; rollup UNSTABLE |
| Scope contained to GitHub Artifacts UX fix                   | FAIL — TUI cockpit spec + orchestrator + dummy label included |
| Dummy/temporary CI triggers removed                          | FAIL — `trigger: pr-142-dummy-change` still present |
| Implementer report present on PR branch                      | FAIL — `ai/reports/task-0142-implementer.md` missing |
| Secret safety                                                | PASS — no credentials, signed URLs, or tokens in diff |

## PR/CI Evidence

PR: https://github.com/sourceplane/orun/pull/142
- Branch: `happy-patch-113` → `main`
- HEAD: `ddbec4c fix(github): consistent --orun-dir semantics; register status flags`
- Commits on branch (3):
  - `ddbec4c` fix(github): consistent --orun-dir semantics; register status flags
  - `061fa5d` test: trigger api-edge component change to verify CI matrix jobs
  - `85cd7a8` chore: update happy-patch-113  ← brings in TUI cockpit + orchestrator + history
- Diff size: 15 files, +3,760 / −11
- mergeStateStatus: UNSTABLE

CI rollup (latest commit):
| Check                              | Status    | Conclusion |
|------------------------------------|-----------|------------|
| Orun Plan                          | QUEUED    | (none)     |
| Harness dry-run guard              | COMPLETED | SUCCESS    |
| Compile plan                       | COMPLETED | SKIPPED    |
| Run: ${{ matrix.job }}             | COMPLETED | SKIPPED    |
| Env fanout: ${{ matrix.env_name }} | COMPLETED | SKIPPED    |
| Verify remote status and logs      | COMPLETED | SKIPPED    |

`Orun Plan` (workflow `CI`, run `26606559596`, job `78402930910`) started 2026-05-28T22:41:48Z
and is still `queued` ~24h later. Per task constraint: "Do not merge if CI is queued,
failing, cancelled, or unknown." The downstream `orun remote-state conformance` jobs are
SKIPPED because their compile/matrix gating did not produce work — `Harness dry-run guard`
is the only check that actually exercised code, and it passed.

## Code Review Notes

### `cmd/orun/command_github.go` — `--orun-dir` normalization

```
orunDir := githubPullOrunDir
if orunDir == "" {
    orunDir = "."
}
// Normalize: --orun-dir is a working directory; .orun/ lives inside it.
// Accept either the working dir or a path that already ends in .orun for back-compat.
if filepath.Base(orunDir) != state.OrunDir {
    orunDir = filepath.Join(orunDir, state.OrunDir)
}
```

Behavior matches the acceptance criteria:
- Default `--orun-dir .` → `filepath.Join(".", ".orun")` = `.orun` ✅
- `--orun-dir /tmp/foo` → `filepath.Base = "foo"` ≠ `.orun` → `/tmp/foo/.orun` ✅
- `--orun-dir /tmp/foo/.orun` → `filepath.Base = ".orun"` → unchanged ✅
- The value handed to `runbundle.Hydrate` therefore always ends in `.orun`, matching docs.

`TestGithubPullOrunDirDefaultResolvesToDotOrun` and `TestGithubPullOrunDirWithIntentRoot`
cover the join behaviour. Direct table-driven coverage of the new `filepath.Base != .orun`
branch (the `/tmp/foo` and `/tmp/foo/.orun` cases) isn't there — the existing tests still
exercise the legacy `storeDir() + .orun` join — but the logic is small and self-evidently
correct on inspection. Not a blocker; suggest adding two cases in a follow-up.

Subtle behaviour change worth flagging (not blocking): the previous code only joined
`.orun` when `orunDir == "."`. The new code joins for *any* path whose basename isn't
`.orun`. That is the intended semantics per docs, but it changes how absolute paths are
treated: an operator who previously passed `--orun-dir /var/run/orun-store` (assuming the
store itself was the target) now gets `/var/run/orun-store/.orun`. The docs change makes
this explicit, and `.orun`-suffixed paths remain a back-compat escape hatch.

### `cmd/orun/command_github.go` — `orun github status` flags

Six flag registrations added to `githubStatusCmd` (lines 112-117): `--run-id`,
`--exec-id`, `--sha`, `--branch`, `--latest`, `--failed`. They bind to the same package
vars used by `logs`/`pull` (`githubLogsRunID`, `githubLogsExecID`, `githubLogsSHA`,
`githubLogsBranch`, `githubLogsLatest`, `githubLogsFailed`), and `runGithubStatus`
already consumes those vars when constructing its resolver inputs (lines 448-449 and
the surrounding `resolveRun` call). Wiring is consistent with `pull`/`logs`.

Caveat (not blocking, but worth noting for the orchestrator): because `status`, `logs`,
and `pull` share the same global flag-backed variables, registering the same flag name
on multiple sibling subcommands is safe with Cobra but means a user can't run two
subcommands concurrently in-process with different selectors. This is the existing
pattern, so the change doesn't make it worse.

### `website/docs/cli/orun-github.md`

Doc edits accurately describe the new `--orun-dir` semantics, full-SHA caveat, the new
`status` flag set, the consolidated resolution order, and the `--job` substring-matching
limitation. No false claims.

### `docs/github-log-pull-ux-review.md`

Walkthrough + improvements list. Reasonable context doc; no claims to verify mechanically.

## Scope/Overreach Review

Per the verifier task, each non-CLI file must be classified.

| File / Group                                         | Classification | Reason |
|------------------------------------------------------|----------------|--------|
| `cmd/orun/command_github.go`                         | ACCEPTABLE     | Core PR scope |
| `website/docs/cli/orun-github.md`                    | ACCEPTABLE     | Documents the code change |
| `docs/github-log-pull-ux-review.md`                  | ACCEPTABLE     | Provenance for the fix |
| `examples/apps/api-edge/component.yaml` `trigger: pr-142-dummy-change` | **BLOCKER** | Explicit dummy label introduced by commit 061fa5d to force a CI re-trigger. Should not land on `main`. |
| `.kiro/specs/orun-tui-cockpit/**` (1,971 lines)      | **BLOCKER**    | Brand-new TUI cockpit spec pack. Not part of GitHub Artifacts UX. Should ship in its own PR with a proper title/body and review. |
| `orun-tui-cockpit.md` (456 lines)                    | **BLOCKER**    | Same TUI cockpit content at repo root; almost certainly belongs alongside the spec pack. |
| `agents/orchestrator.md` (+475 lines, new file)      | **BLOCKER**    | Verifier Standard / Merge Protocol document. Useful, but landing it inside a "chore: update happy-patch-113" PR with no review hides a foundational process doc behind unrelated noise. Split. |
| `ai/tasks/task-0139-verifier.md`, `task-0140.md`, `task-0140-verifier.md`, `task-0141-verifier.md` | CLEANUP | Historical task prompts that were never committed. Harmless on their own but should land as a dedicated housekeeping commit, not as part of a CLI fix. |
| `ai/waiting_for_input.md`                            | STALE          | References Task 0141.1 ("ready to proceed") which is already merged. Should be regenerated (or set to "No human input currently requested") before this lands. |

Verifier conclusion: the PR should be **narrowed to the CLI + docs changes** (≈ the
`ddbec4c` commit minus the dummy-trigger). The TUI cockpit spec pack, the orchestrator
document, the historical task prompts, and the dummy component label should each be
split into their own PRs (or at minimum into clearly-titled commits with descriptive
PR title/body that name them).

## Secret and Safety Review

- Reviewed every changed file's diff for tokens, signed URLs, bearer headers, raw
  credentials, or PII. None found.
- `cmd/orun/command_github.go` adds no new I/O paths beyond what already existed; the
  `--orun-dir` normalization only operates on a user-supplied path.
- `examples/apps/api-edge/component.yaml` adds a non-sensitive label (`trigger:
  pr-142-dummy-change`); not a secret, but is the scope blocker described above.
- No log / output sites print credentials.

## Issues

Blockers (must fix before merge):

1. **CI not green** — `Orun Plan` is QUEUED. Either kick the queue, wait for it to run
   and confirm SUCCESS, or — if it remains stuck — re-trigger CI on the narrowed PR.
2. **Dummy CI trigger** — remove `trigger: pr-142-dummy-change` from
   `examples/apps/api-edge/component.yaml`. The component-change verification it was
   added to perform has happened; the label has no business on `main`.
3. **PR scope** — split `.kiro/specs/orun-tui-cockpit/**`, `orun-tui-cockpit.md`,
   `agents/orchestrator.md`, the four historical `ai/tasks/task-013*/014*` prompts,
   and `ai/waiting_for_input.md` out of this PR. Each is independently reviewable and
   should not ride on a `chore: update happy-patch-113` commit.
4. **PR title and body** — retitle to something like
   `fix(github): normalize --orun-dir and register status resolution flags`, and write
   a real body summarising the CLI behaviour change, the doc updates, and the new
   resolution order. The current title/body cannot be merged as a squash commit on
   `main` without losing the change story.

Process issues (non-blocking for this PR; orchestrator should note):

5. No `ai/reports/task-0142-implementer.md` was produced. Future implementer runs that
   open a PR should commit a report onto the PR branch so verification has a self-
   reported scope statement to validate against.

## Risk Notes

- The `--orun-dir` semantics change is technically a small behaviour shift for callers
  who previously passed an absolute path pointing directly at an `.orun` store without
  the `.orun` suffix. The new docs explicitly tell them what happens, and the
  `.orun`-suffixed back-compat path keeps the old behaviour reachable. Low risk, but
  worth a one-line CHANGELOG entry once the PR is narrowed.
- `agents/orchestrator.md` is process-defining content. Landing it casually (no PR
  review focus) risks future agents treating an unreviewed draft as canonical. Strongly
  recommend a dedicated PR with at least one human reviewer.
- TUI cockpit spec is large (≈ 2.4k lines). Reviewing it inside a CLI-fix PR is
  unrealistic; future verifiers will rubber-stamp it. Split.

## Recommended Next Move

Orchestrator should hand the implementer a narrow follow-up: rebase `happy-patch-113`
(or open a successor branch from `main`) containing only the CLI + docs changes from
`ddbec4c`, retitled and re-described, with `trigger: pr-142-dummy-change` reverted.
Re-trigger CI and confirm `Orun Plan` passes before re-requesting verification.

Separately, the orchestrator should scope:
- A `chore(spec)` PR for `.kiro/specs/orun-tui-cockpit/**` + `orun-tui-cockpit.md`.
- A `docs(agents)` PR for `agents/orchestrator.md`.
- A `chore(history)` PR for the historical `ai/tasks/*` prompts and the refreshed
  `ai/waiting_for_input.md`.

PR #142 remains OPEN. No state files are advanced; `ai/state.json.completed` does not
gain `0142` because nothing was merged.

## PR Number

**#142** — https://github.com/sourceplane/orun/pull/142
