# Task 0001 — Verifier

Agent: Verifier

## Current Repo Context
- Task 0001 (Milestone M0 — Foundation for `specs/orun-state-redesign/`) was implemented on branch `impl/task-0001-m0-foundation` and opened as PR **#152** ("Task 0001: Milestone M0 — state-redesign foundation (deps, testfx/statefs, Makefile)").
- Implementer report filed at `ai/reports/task-0001-implementer.md`. Summary: pinned `github.com/oklog/ulid/v2 v2.1.1` (kept live via a `//go:build tools` blank import in `internal/testfx/statefs/tools.go`), scaffolded `internal/testfx/statefs` with `NewWorkspace`, `AssertJSONFile`, `ReadJSON[T]` plus happy + failure unit tests, added `make test-state-redesign`. Also bundled the roadmap-pivot artifacts: new spec pack under `specs/orun-state-redesign/`, edits to `agents/orchestrator.md`, rebuilt `ai/` tree, and deletions of the TUI-era `ai/tasks/task-014*.md` and `ai/reports/task-014*.md`.
- PR status at scoping time: `mergeable=MERGEABLE`, `mergeStateStatus=CLEAN`, all CI checks `SUCCESS` (or `SKIPPED` for matrix legs with zero rows), no failing checks. Branch is up to date with `main` at `d2ab48e`.
- No outstanding `/ai/proposals/`. The known `flyingmutant/rapid` → `pgregory.net/rapid` spec drift in `specs/orun-state-redesign/test-plan.md §3` is documented in `ai/context/current.md`; the implementer chose to defer the proposal filing rather than fold it into this PR — this is acceptable, but the verifier should record whether it stays deferred or should be filed as a follow-up.

## Objective
Verify PR #152 against Task 0001's acceptance criteria, the Verifier Standard in `agents/orchestrator.md`, and the M0 "done when" criteria in `specs/orun-state-redesign/implementation-plan.md`. PASS and merge, or FAIL with explicit blockers.

## PR Boundary
Verify exactly the scope of PR #152. The PR legitimately bundles three things on one branch because they are inseparable for a coherent `main`:
1. Dependency pin: `go.mod`/`go.sum` add `github.com/oklog/ulid/v2 v2.1.1` as a direct require.
2. Test harness: new package `internal/testfx/statefs/` (`statefs.go`, `statefs_test.go`, transitional `tools.go`).
3. Makefile target: `make test-state-redesign` (initially exercising only `internal/testfx/statefs/...`).
4. Pivot artifacts: spec pack under `specs/orun-state-redesign/`, source design doc at `orun-state-redesign.md`, `agents/orchestrator.md` edits, rebuilt `ai/` tree, deletions of TUI-era `ai/tasks/task-014*.md` and `ai/reports/task-014*.md`.

Non-goals to enforce (reject the PR if any of these appear):
- No production-code changes outside `internal/testfx/statefs/`.
- No CLI surface changes (`cmd/orun/...`, `internal/cli/...` must be untouched).
- No `.orun/revisions/`, `TriggerOccurrence`, or `StateStore` introduction (those belong to M1/M2).
- No unrelated refactors, formatting churn, or opportunistic changes to other `internal/` packages.

## Read First
- `ai/tasks/task-0001.md` — original implementer prompt and acceptance criteria.
- `ai/reports/task-0001-implementer.md` — implementer's report (claims to verify).
- `agents/orchestrator.md` — Verifier Standard and Verifier Merge Protocol.
- `specs/orun-state-redesign/README.md` — entry/read-order for the active spec pack.
- `specs/orun-state-redesign/implementation-plan.md` — M0 "done when" criteria.
- `specs/orun-state-redesign/design.md` §9 Correctness Properties, §13 Dependency additions.
- `specs/orun-state-redesign/test-plan.md` §1 Coverage targets, §3 (the `rapid` import-path drift), §8 CI integration.
- PR diff: `gh pr diff 152`.
- PR metadata + checks: `gh pr view 152 --json ...`.

## Required Outcomes
- [ ] Verifier report `ai/reports/task-0001-verifier.md` filed with explicit `Result: PASS` or `Result: FAIL`.
- [ ] If PASS: PR #152 merged, local `main` fast-forwarded to `origin/main`, task branch not left checked out, `git status --short` clean.
- [ ] If PASS: `ai/state.json`, `ai/context/current.md`, `ai/context/task-ledger.md` updated to reflect M0 complete and the next task pointer (see "After Verification" below).
- [ ] If FAIL: PR left OPEN with each blocker recorded in the report; no state-file mutation that would mark Task 0001 as completed.
- [ ] One-line decision recorded for the `flyingmutant/rapid` → `pgregory.net/rapid` drift: either filed as `/ai/proposals/task-0001-spec-update.md` now, or formally deferred with rationale (not silently dropped).

## Non-Goals
- Do not start M1 (`internal/triggerctx`). That is the next implementer task only after this verifier passes.
- Do not modify the spec pack beyond a clarification proposal file under `/ai/proposals/`.
- Do not edit production code; verifier-only fixes to the PR branch are allowed (report tweaks, deferred-proposal file) but must be pushed and re-run through CI before merge.
- Do not delete the transitional `internal/testfx/statefs/tools.go`. It is M0's intentional artefact; M1 removes it when the first real production import of `oklog/ulid/v2` lands.

## Constraints
1. Inspect the actual PR diff with `gh pr diff 152` — do not trust the report alone.
2. Re-run all "Checks Run" the implementer listed; flag any divergence.
3. Confirm `internal/testfx/statefs/statefs.go` imports only stdlib + `testing` (no `internal/*` paths). Spot-check `go list -deps ./internal/testfx/statefs` if uncertain.
4. Confirm failure-path tests actually exercise failure (e.g., wrong JSON, missing file) and assert via a fakeT — not via real `t.Fatal` that would mask a missed assertion.
5. Confirm `go.mod` carries `github.com/oklog/ulid/v2 v2.1.1` in the **direct** require block (not indirect). Verify with `go list -m github.com/oklog/ulid/v2`.
6. Confirm `make test-state-redesign` runs and exits 0; confirm the target's `.PHONY` entry exists in `Makefile`.
7. Confirm the deletions of `ai/tasks/task-014*.md` and `ai/reports/task-014*.md` are present in the diff and do not collaterally drop any non-TUI file.
8. Confirm `agents/orchestrator.md` and `specs/orun-state-redesign/` references stay internally consistent (e.g., `active_spec` in `ai/state.json` matches the spec path on disk).
9. No secrets, tokens, or credentials anywhere in the diff or CI logs.
10. Use `/Users/irinelinson/.local/bin/kiox` when `kiox` is not on `PATH`.

## Integration Notes
- PR is on branch `impl/task-0001-m0-foundation` at SHA `628c2127b0dbd07a4797494e349eafe5a315c782`. Base `main` at `d2ab48e`.
- CI workflows that ran: `CI` (Orun Plan), `orun remote-state conformance` (Harness dry-run guard). Matrix legs are `SKIPPED` because the example intent compiles to 0 jobs — expected at M0; confirm by reading the job logs, not just the status summary.
- The implementer chose `testing.TB` over `*testing.T` in helper signatures. That is a deliberate superset; do **not** treat it as a deviation. It enables failure-path unit testing via a fakeT.

## Acceptance Criteria
✅ PR #152 corresponds exactly to Task 0001 (no extra modules, no CLI changes, no unrelated refactors).
✅ `gh pr view 152` shows `state=OPEN` going into verification, `mergeable=MERGEABLE`, `mergeStateStatus=CLEAN`, and every required check `SUCCESS` (or legitimately `SKIPPED` with a captured reason from job logs).
✅ Local `go build ./...`, `go vet ./...`, `go test ./...` all exit 0 against the PR branch.
✅ Local `go test -count=1 ./internal/testfx/statefs/...` exits 0 and exercises both happy and failure paths for `NewWorkspace`, `AssertJSONFile`, `ReadJSON[T]`.
✅ `make test-state-redesign` exits 0.
✅ `go list -m github.com/oklog/ulid/v2` reports `v2.1.1`; the require is in the direct block.
✅ `grep -R "\"<repo>/internal/" internal/testfx/statefs` (or equivalent) returns no matches — package is leaf-clean.
✅ `kiox -- orun validate --intent examples/intent.yaml`, `kiox -- orun plan --changed --intent examples/intent.yaml --output /tmp/plan.json`, and `kiox -- orun run --plan /tmp/plan.json --dry-run --runner github-actions` all exit 0 (record the no-op result for empty plans).
✅ GitHub Actions logs (not just rollup) inspected for the two completed workflows; commands that were supposed to run actually ran.
✅ No plaintext secrets/tokens in diff or CI logs.
✅ M0 "done when" criteria in `specs/orun-state-redesign/implementation-plan.md` are all satisfied.
✅ `flyingmutant/rapid` drift decision recorded (filed as proposal OR formally deferred with rationale).
✅ If PASS: PR merged, `main` fast-forwarded, `git status --short` clean, branch not checked out.

## Verification
Execute, in order:
1. `cd /Users/irinelinson/sourceplane/orun && git fetch origin && gh pr checkout 152`.
2. `git status --short` (should be clean before any local edits).
3. `gh pr view 152 --json number,state,mergeable,mergeStateStatus,statusCheckRollup,headRefOid,baseRefName`.
4. `gh pr diff 152 | head -400` (then scroll for full review).
5. `go build ./...`, `go vet ./...`, `go test ./...`, `go test -count=1 ./internal/testfx/statefs/...`.
6. `make test-state-redesign`.
7. `go list -m github.com/oklog/ulid/v2`.
8. `/Users/irinelinson/.local/bin/kiox -- orun validate --intent examples/intent.yaml`.
9. `/Users/irinelinson/.local/bin/kiox -- orun plan --changed --intent examples/intent.yaml --output /tmp/orun-task0001-plan.json`.
10. `/Users/irinelinson/.local/bin/kiox -- orun run --plan /tmp/orun-task0001-plan.json --dry-run --runner github-actions` (no-op acceptable; record explicitly).
11. `gh run view 26656333958 --log | tail -200` (CI workflow) and `gh run view 26656333939 --log | tail -200` (remote-state conformance) — confirm expected commands ran; do not trust the rollup alone.
12. Search the diff for secret-shaped tokens (`AKIA`, `xoxb-`, `ghp_`, long base64-looking strings); fail if any appear.
13. Decide on the `flyingmutant/rapid` drift: if filing, write `ai/proposals/task-0001-spec-update.md` and push to the PR branch as a verifier-only commit; if deferring, record rationale in the verifier report.
14. If any verifier-only artifact was added (proposal file, report tweak), commit and push to the PR branch, then re-check `gh pr view 152` and wait for CI to re-go-green before merging.

### Verifier Merge Protocol (from `agents/orchestrator.md`)
- If PASS and CI green: `gh pr merge 152 --squash --delete-branch` (or `--merge` per repo convention), then `git checkout main && git pull --ff-only origin main`, confirm `git status --short` is clean, do not leave `impl/task-0001-m0-foundation` checked out.
- If PASS but any required check is failing: do NOT merge; treat as FAIL.
- If FAIL: leave PR open, list blockers in the report, do not mutate state files to mark Task 0001 complete.

## PR Creation Requirement
The implementer has already created PR #152. The verifier does not create a new PR. Verifier-only commits (proposal file, report) are pushed to the existing branch `impl/task-0001-m0-foundation` before merge.

## When Done Report
Save report to `ai/reports/task-0001-verifier.md` with these sections (per Verifier Standard):

- `Result: PASS | FAIL`
- `Checks` — every command from the Verification list with its exit code.
- `Issues` — blockers (FAIL only) or minor non-blocking concerns (PASS).
- `CI Log Review` — what the workflow logs actually showed (commands that ran, any SKIPPED legs and why).
- `Live Resource Evidence` — N/A for M0 (no live resources); state so explicitly.
- `Secret Handling Review` — confirmation no credentials appear in diff or CI logs.
- `Spec Proposals` — `flyingmutant/rapid` drift decision; link to proposal file if filed.
- `Risk Notes` — residual risk (e.g., `tools.go` lifecycle, harness coverage gaps to revisit in M1+).
- `Recommended Next Move` — on PASS: "Orchestrator advances `active_milestone` to M1; next implementer task is Task 0002 (`internal/triggerctx`)." On FAIL: specific remediation list.

### After Verification (Orchestrator hands off if PASS)
On a PASS merge, the orchestrator will:
- Set `ai/state.json` → `current_task: "0002"`, append `"0001"` to `completed`, bump `active_milestone` to `"M1"`, update `task_agent` to the new `task-0002.md`.
- Update `ai/context/current.md` to point at Task 0002 (Milestone M1 — `internal/triggerctx`).
- Append a verified-and-merged ledger entry for Task 0001 in `ai/context/task-ledger.md`.
- Generate `ai/tasks/task-0002.md` (M1 implementer prompt) and the next verifier task afterwards.
