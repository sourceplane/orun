# Task 0002 — Verifier

Agent: Verifier

## Current Repo Context

- Task 0002 (Milestone M1 — `internal/triggerctx`) was implemented on branch
  `impl/task-0002-m1-triggerctx` and opened as PR **#153**
  ("Task 0002: M1 — internal/triggerctx").
- Implementer report at `ai/reports/task-0002.md`. Summary: introduces the
  `internal/triggerctx` package (`context.go`, `ids.go`, `system.go`,
  `declared.go`, `resolve.go` + 5 test files); deletes the M0 shim
  `internal/testfx/statefs/tools.go`; first real production import of
  `github.com/oklog/ulid/v2` lands here and remains in the **direct** require
  block after `go mod tidy`. Inline rapid-path spec edits applied to
  `specs/orun-state-redesign/test-plan.md` §1 and `design.md` §10 alongside
  the proposal at `ai/proposals/task-0002-spec-update.md`. Reported coverage
  91.6% on `internal/triggerctx` (gate ≥ 90 %).
- PR status at scoping time: `mergeable=MERGEABLE`, `mergeStateStatus=CLEAN`,
  all CI checks `SUCCESS` (matrix legs legitimately `SKIPPED` because the
  example intent compiles to zero jobs). Diff: 17 files, +2,097 / −17.
- Outstanding proposal: `ai/proposals/task-0002-spec-update.md` (rapid import
  path clarification). It is a non-behavioral clarification; the implementer
  folded the spec edits into this PR per the Spec Change Proposals rules.
  Verifier confirms the edits exist and match the proposal; no separate
  Orchestrator decision is needed.
- Local environment quirk (documented in `ai/state.json` notes and
  `ai/context/current.md`): `kiox -- orun plan --changed --intent
  examples/intent.yaml` fails on the composition-cache (`stack.yaml has no
  spec.compositions`) on both `main` and this branch; CI passes the same
  invocation. Pre-existing — record but do not block.

## Objective

Verify PR #153 against Task 0002's acceptance criteria, the Verifier Standard
in `agents/orchestrator.md`, and the M1 "done when" criteria in
`specs/orun-state-redesign/implementation-plan.md`. PASS and merge, or FAIL
with explicit blockers.

## PR Boundary

Verify exactly the scope of PR #153. The PR legitimately bundles, on one
branch:

1. New package `internal/triggerctx/` with 5 production files and 5 test
   files implementing the trigger context model + resolver.
2. Deletion of the M0 shim `internal/testfx/statefs/tools.go` (intentional —
   M1's "first real production import of `oklog/ulid/v2`" replaces the shim).
3. Two non-behavioral spec edits — `specs/orun-state-redesign/test-plan.md`
   §1 and `specs/orun-state-redesign/design.md` §10 — correcting the rapid
   library import path from `github.com/flyingmutant/rapid` to
   `pgregory.net/rapid`. The companion proposal lives at
   `ai/proposals/task-0002-spec-update.md`.
4. The implementer report and task prompt are checked in to `ai/`.
5. `Makefile` updated so `make test-state-redesign` exercises
   `internal/triggerctx/...` alongside `internal/testfx/statefs/...`.

Non-goals to enforce (reject the PR if any of these appear):

- No `StateStore` interface (that is M2).
- No `.orun/revisions/` writes, no manifests, no executions.
- No CLI surface changes (`cmd/orun/...`, `internal/cli/...` must be
  untouched).
- No production-code changes outside `internal/triggerctx/`,
  `internal/testfx/statefs/tools.go` (deletion), and the `Makefile` /
  `go.mod` / `go.sum` companion updates.
- No unrelated refactors, formatting churn, or opportunistic changes to other
  `internal/` packages.

## Read First

- `ai/tasks/task-0002.md` — original implementer prompt and acceptance.
- `ai/reports/task-0002.md` — implementer's report (claims to verify).
- `ai/proposals/task-0002-spec-update.md` — rapid path clarification.
- `agents/orchestrator.md` — Verifier Standard and Verifier Merge Protocol.
- `specs/orun-state-redesign/README.md` — entry/read-order for the active
  spec pack.
- `specs/orun-state-redesign/implementation-plan.md` — M1 "done when".
- `specs/orun-state-redesign/data-model.md` §2 (`TriggerOccurrence`,
  `TriggerSource`, `PlanScope` schemas) and §10 (ID family rules: ULID
  monotonicity, `trg_` prefix, `TriggerKey` format).
- `specs/orun-state-redesign/design.md` §5.1 (resolver flow / dispatcher
  responsibility) and §11 (error taxonomy — `ErrNoMatchingBinding` /
  `*NoMatchingBindingError`).
- `specs/orun-state-redesign/test-plan.md` §1 (coverage targets) and §3
  (property tests).
- PR diff: `gh pr diff 153`.
- PR metadata + checks: `gh pr view 153 --json ...`.

## Required Outcomes

- [ ] Verifier report `ai/reports/task-0002-verifier.md` filed with explicit
      `Result: PASS` or `Result: FAIL`.
- [ ] If PASS: PR #153 merged (squash), local `main` fast-forwarded to
      `origin/main`, task branch not left checked out, `git status --short`
      clean.
- [ ] If PASS: `ai/state.json`, `ai/context/current.md`,
      `ai/context/task-ledger.md` updated to reflect M1 complete and the
      next task pointer (see "After Verification" below — orchestrator
      handoff).
- [ ] If FAIL: PR left OPEN with each blocker recorded in the report; no
      state-file mutation that would mark Task 0002 as completed.
- [ ] The rapid import-path proposal is recorded as **applied** (spec edits
      present in this PR) in the verifier report.

## Non-Goals

- Do not start M2 (`internal/statestore`). That is the next implementer
  task only after this verifier passes.
- Do not modify the spec pack beyond clarifications already in scope.
- Do not edit production code. Verifier-only artifacts (the verifier report
  itself, optional minor proposal-file tweaks) may be pushed to the PR
  branch and must be CI-re-greened before merge.

## Constraints

1. Inspect the actual PR diff with `gh pr diff 153` — do not trust the
   report alone.
2. Re-run all "Validation Results" entries the implementer listed and flag
   any divergence.
3. Confirm `internal/triggerctx/` imports only stdlib, `oklog/ulid/v2`,
   `pgregory.net/rapid` (test-only), and `internal/trigger`. No
   `internal/cli`, no `internal/runbundle`, no `internal/state*`, no
   `internal/testfx/statefs`. Spot-check with
   `go list -deps ./internal/triggerctx`.
4. Confirm `go.mod` still carries `github.com/oklog/ulid/v2` in the
   **direct** require block after `go mod tidy` (no shim left behind).
   Verify with `go list -m github.com/oklog/ulid/v2` (expect `v2.1.1`).
5. Confirm coverage gate: `go test -count=1 -cover ./internal/triggerctx/...`
   reports ≥ 90 %.
6. Confirm `TriggerKey` rapid property test exists and runs — search
   `internal/triggerctx/ids_test.go` for `rapid.Check` or equivalent; run
   it explicitly with a non-trivial count (e.g. `go test -count=1 -run
   PropertyStability ./internal/triggerctx -rapid.checks=200`).
7. Confirm all four `ResolveTriggerContext` branches are exercised in
   `resolve_test.go`: system manual, system manual-changed, declared
   (push/PR), declared `--from-ci` no-match. The no-match path MUST return
   an error satisfying `errors.Is(err, ErrNoMatchingBinding)` AND be type-
   assertable to `*NoMatchingBindingError`.
8. Confirm all five system constructors from `implementation-plan.md` M1
   exist: `NewSystemManual`, `NewSystemManualChanged`, `NewSystemReplay`,
   `NewSystemAPI`, `NewSystemMigrated`. (The implementer report mentions
   the first four explicitly but the spec lists five — verify
   `NewSystemMigrated` is present and tested.)
9. Confirm `internal/testfx/statefs/tools.go` is deleted in the PR diff
   AND that `internal/testfx/statefs` is still leaf-clean (depends only on
   stdlib + `testing`). Re-run `go list -deps ./internal/testfx/statefs`.
10. Confirm the rapid path edit replaces every `flyingmutant/rapid`
    occurrence in `specs/orun-state-redesign/`. `rg "flyingmutant" specs/`
    should return zero matches after the PR.
11. Confirm `Makefile` target `make test-state-redesign` now exercises
    `./internal/triggerctx/...` and still exits 0.
12. No secrets, tokens, or credentials anywhere in the diff or CI logs.
13. Use `/Users/irinelinson/.local/bin/kiox` when `kiox` is not on `PATH`.

## Integration Notes

- PR is on branch `impl/task-0002-m1-triggerctx` at HEAD `270eb75`. Base
  `main` at `4ea1980e`.
- CI workflows that ran on this PR (per `gh pr view 153`): `CI / Orun Plan`
  (SUCCESS, run inspectable via `gh run view`) and `orun remote-state
  conformance / Harness dry-run guard` (SUCCESS). Matrix legs are
  legitimately `SKIPPED` because the example intent compiles to 0 jobs at
  M1 (no production wiring of `triggerctx` yet). Confirm by reading the
  job logs, not just the rollup.
- `GitSource` is an injected interface (not a concrete VCS dependency) so
  `triggerctx` stays free of git wiring. Acceptable — a `fakeGit` adapter
  in `resolve_test.go` exercises it.
- The implementer opted to ship M1 as a single PR (under the spec's
  suggested 1–2 PR latitude). That is within scope; do not penalize.

## Acceptance Criteria

✅ PR #153 corresponds exactly to Task 0002 (no extra modules, no CLI
   changes, no unrelated refactors, no M2 surface).
✅ `gh pr view 153` shows `state=OPEN` going into verification,
   `mergeable=MERGEABLE`, `mergeStateStatus=CLEAN`, and every required
   check `SUCCESS` (or legitimately `SKIPPED` with reason captured from
   job logs).
✅ Local `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
   against the PR branch.
✅ `go test -count=1 -cover ./internal/triggerctx/...` exits 0 with
   coverage ≥ 90 %.
✅ `make test-state-redesign` exits 0 and the recipe references
   `./internal/triggerctx/...`.
✅ `go list -m github.com/oklog/ulid/v2` reports `v2.1.1` and the require
   is in the direct block of `go.mod`.
✅ `go list -deps ./internal/triggerctx` contains no `internal/*` other
   than `internal/trigger`.
✅ `go list -deps ./internal/testfx/statefs` is still leaf-clean (no
   `internal/*` deps).
✅ `internal/testfx/statefs/tools.go` is absent from the PR-merged tree.
✅ `TriggerKey` rapid property test runs and passes with an explicit
   non-trivial `-rapid.checks` value.
✅ All four `ResolveTriggerContext` branches covered; `--from-ci`
   no-match returns `*NoMatchingBindingError` satisfying
   `errors.Is(err, ErrNoMatchingBinding)`.
✅ All five M1 system constructors present and tested
   (`NewSystemManual`, `NewSystemManualChanged`, `NewSystemReplay`,
   `NewSystemAPI`, `NewSystemMigrated`).
✅ `rg "flyingmutant" specs/` returns zero matches.
✅ `kiox -- orun validate --intent examples/intent.yaml` exits 0.
   `kiox -- orun plan --changed --intent examples/intent.yaml --output
   /tmp/plan.json` may fail locally on the documented composition-cache
   quirk (see Current Repo Context); record the failure mode and confirm
   it reproduces on `main` HEAD `4ea1980e` — if it does, it is not a
   regression. If it does NOT reproduce on `main`, treat as a FAIL.
✅ GitHub Actions logs (not just rollup) inspected for the two completed
   workflows; expected commands actually ran.
✅ No plaintext secrets/tokens in diff or CI logs.
✅ M1 "done when" criteria in `specs/orun-state-redesign/implementation-plan.md`
   are all satisfied.
✅ If PASS: PR merged via squash, `main` fast-forwarded, `git status
   --short` clean, branch not checked out.

## Verification

Execute, in order:

1. `cd /Users/irinelinson/sourceplane/orun && git fetch origin && gh pr
   checkout 153`.
2. `git status --short` (clean before any local edits).
3. `gh pr view 153 --json
   number,state,mergeable,mergeStateStatus,statusCheckRollup,headRefOid,baseRefName`.
4. `gh pr diff 153 | wc -l` then scroll the full diff.
5. `go build ./...`, `go vet ./...`, `go test ./...`.
6. `go test -count=1 -cover ./internal/triggerctx/...`.
7. `go test -count=1 -run Property ./internal/triggerctx -rapid.checks=200`.
8. `make test-state-redesign`.
9. `go list -m github.com/oklog/ulid/v2` (expect `v2.1.1`, direct).
10. `go list -deps ./internal/triggerctx | rg '^orun/' || true` —
    inspect for forbidden `internal/*` neighbours.
11. `go list -deps ./internal/testfx/statefs` — confirm leaf-clean.
12. `rg "flyingmutant" specs/` (expect zero matches).
13. `/Users/irinelinson/.local/bin/kiox -- orun validate --intent
    examples/intent.yaml`.
14. `/Users/irinelinson/.local/bin/kiox -- orun plan --changed --intent
    examples/intent.yaml --output /tmp/orun-task0002-plan.json` — if it
    fails on the composition-cache quirk, re-run on `main` HEAD `4ea1980e`
    to confirm it reproduces there too; record exit codes for both.
15. `/Users/irinelinson/.local/bin/kiox -- orun run --plan
    /tmp/orun-task0002-plan.json --dry-run --runner github-actions`
    (only if step 14 produced a plan; otherwise record no-op).
16. `gh run view <CI Orun Plan run-id> --log | tail -200` and
    `gh run view <conformance run-id> --log | tail -200` — confirm
    expected commands ran; do not trust the rollup.
17. Search the diff for secret-shaped tokens (`AKIA`, `xoxb-`, `ghp_`,
    long base64-looking strings); fail if any appear.
18. Write `ai/reports/task-0002-verifier.md` with the sections listed
    below.
19. If any verifier-only artifact was added (report, optional proposal
    tweak), commit and push to the PR branch, then re-check `gh pr view
    153` and wait for CI to re-go-green before merging.

### Verifier Merge Protocol (from `agents/orchestrator.md`)

- If PASS and CI green: `gh pr merge 153 --squash --delete-branch`, then
  `git checkout main && git pull --ff-only origin main`, confirm `git
  status --short` is clean, do not leave `impl/task-0002-m1-triggerctx`
  checked out.
- If PASS but any required check is failing: do NOT merge; treat as FAIL.
- If FAIL: leave PR open, list blockers in the report, do not mutate
  state files to mark Task 0002 complete.

## PR Creation Requirement

The implementer has already created PR #153. The verifier does not create
a new PR. Verifier-only commits (the verifier report, optional proposal
tweak) are pushed to the existing branch `impl/task-0002-m1-triggerctx`
before merge.

## When Done Report

Save report to `ai/reports/task-0002-verifier.md` with these sections (per
Verifier Standard):

- `Result: PASS | FAIL`
- `Checks` — every command from the Verification list with its exit code
  and a one-line outcome.
- `Issues` — blockers (FAIL only) or minor non-blocking concerns (PASS).
- `CI Log Review` — what the workflow logs actually showed (commands that
  ran, any SKIPPED legs and why).
- `Live Resource Evidence` — N/A for M1 (no live resources, no
  persistence); state explicitly.
- `Secret Handling Review` — confirmation no credentials appear in diff
  or CI logs.
- `Spec Proposals` — record `ai/proposals/task-0002-spec-update.md` as
  **applied** (inline spec edits present). If any new drift is found,
  file a new proposal.
- `Risk Notes` — residual risk (e.g., GitSource interface ergonomics for
  M2 wiring; plan-scope defaults for `system.api`; whether
  `NormalizeScope` needs exporting for M2 consumers).
- `Recommended Next Move` — on PASS: "Orchestrator advances
  `active_milestone` to M2; next implementer task is Task 0003
  (`internal/statestore` local driver)." On FAIL: specific remediation
  list.

### After Verification (Orchestrator hands off if PASS)

On a PASS merge, the orchestrator will:

- Set `ai/state.json` → `current_task: "0003"`, append `"0002"` to
  `completed`, bump `active_milestone` to `"M2"`, update `task_agent` to
  the new `task-0003.md`.
- Update `ai/context/current.md` to point at Task 0003 (Milestone M2 —
  `internal/statestore` local driver), record the M1 closing summary, and
  refresh the Repo Checkpoint table.
- Append a verified-and-merged ledger entry for Task 0002 in
  `ai/context/task-ledger.md`.
- Generate `ai/tasks/task-0003.md` (M2 implementer prompt) and the next
  verifier task afterwards.
