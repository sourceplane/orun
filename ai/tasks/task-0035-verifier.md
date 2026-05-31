# Task 0035 — C4 PR-3 verifier (Resolver + RebuildIndexes, PR #175)

Agent: Verifier

## Current Repo Context

- Phase 2, Milestone **C4 — `internal/catalogstore`** Writer + Resolver. This
  is the **closing** PR of C4. Prior C4 PRs:
  - PR #173 (Task 0030, squash `c2d7b9d`) — paths/errors/writer Steps A & B.
  - PR #174 (Task 0032 + 0033, squash `73c6e8e`) — refs/indexes/events Steps
    C & D. Closed at 90.1 % on `internal/catalogstore` after the Task 0033
    verifier attached 28 coverage tests (implementer had landed at 85.3 %).
- **PR #175** (Task 0034 implementer, branch
  `task-0034-catalogstore-c4-pr3-resolver`) is OPEN. It ships the read side:
  `resolver.go` (five Resolver methods), `rebuild.go` (`RebuildIndexes`,
  added to the `Resolver` interface), and the T-STORE-3 byte-identical
  rebuild test. Implementer report: `ai/reports/task-0034-implementer.md`.
- **CI status:** all PR #175 checks GREEN, mergeStateStatus CLEAN. NOTE: the
  `test` job first failed once on a `executionstate` coverage flap (89.6 %
  measured vs 90.0 % floor — a Phase-1 package PR-3 does NOT touch, identical
  to main); a rerun went green. The flap is an environment/threshold
  brittleness on a zero-margin floor, NOT a PR-3 defect. See "Carry-forward
  risk" below.
- **The central adjudication this task owns:** the implementer self-reports
  `internal/catalogstore` coverage at **88.9 %** — BELOW the 90 % floor and
  the 91 % target the Task 0034 prompt set. `internal/catalogstore` has **no
  CI coverage gate**, so green CI does NOT enforce the floor. The verifier
  MUST re-measure and adjudicate per the three-branch policy below. Do not
  treat green CI as sufficient evidence of coverage compliance.

## Objective

Verify PR #175 against Task 0034's acceptance criteria and the Verifier
Standard, with primary focus on (1) the `internal/catalogstore` coverage
shortfall to 88.9 %, (2) the §4 reader fallback ladder correctness, and (3)
the T-STORE-3 byte-identical rebuild proof. Decide PASS / PASS-with-attached-
fix / FAIL and execute the merge protocol. PASS requires the PR MERGED.

## PR Boundary (must match implementer scope exactly — no expansion)

In scope for verification:
1. `internal/catalogstore/resolver.go` — five Resolver methods + helpers.
2. `internal/catalogstore/rebuild.go` — `RebuildIndexes` body.
3. `internal/catalogstore/store.go` — `Resolver` interface gains
   `RebuildIndexes(ctx) error`; compile-time `var _ Resolver = (*store)(nil)`.
4. `internal/catalogstore/{resolver,rebuild,store,writer}_test.go` — new and
   extended tests.
5. `internal/catalogstore/errors.go` — any sentinel additions for the
   resolver (`ErrCatalogNotFound` / `ErrComponentNotFound`).

Out of scope (flag as overreach if present): any change to
`internal/statestore`, `revision`, `executionstate`, `catalogmodel`,
`sourcectx`, `catalogresolve`; any CLI (`orun catalog *` is C5); any
`internal/catalogsync` activity; any Writer behavioural change.

## Read First

- `ai/tasks/task-0034.md` — original implementer scope and acceptance criteria.
- `ai/reports/task-0034-implementer.md` — what was delivered; note the three
  "Open questions for verifier" (test-side `makeRebuildCGI` duplication;
  no stale-index scrub; silent corrupt-manifest skip) and the self-reported
  88.9 % coverage.
- `agents/orchestrator.md` — Verifier Standard + Verifier Merge Protocol.
- `specs/orun-component-catalog/catalog-store.md` §4 (reader fallback —
  primary), §6 (error taxonomy), §8 (`RebuildIndexes` byte-identical
  contract, T-STORE-3), §9 (concurrency).
- `specs/orun-component-catalog/data-model.md` §8–§9 (Ref + ComponentGlobalIndex
  shapes).
- `ai/reports/task-0033-verifier.md` — the PR-2 coverage-adjudication
  precedent and the documented "Branches Left Unexercised" list (defensive
  returns gated by upstream validation; do not demand white-box hooks to
  cover those).
- `ai/context/orchestrator-brief.md` — cycle-8 mental model + next-cycle
  hypothesis (this task is path (a)).

## Required Outcomes (verifier report)

Produce `ai/reports/task-0035-verifier.md` with: `Result: PASS|FAIL`,
`Checks`, `Issues`, `Coverage Adjudication`, `CI Log Review`, `Code-Path
Inspection`, `Secret Handling Review`, `Spec Proposals`, `Risk Notes`,
`Recommended Next Move`.

## Verification Steps

1. **Repo/PR state.** `git fetch`; confirm branch is CLEAN/MERGEABLE against
   main (`gh pr view 175 --json mergeable,mergeStateStatus,state`). Confirm
   `ai/reports/task-0034-implementer.md` is committed on the PR branch
   (`git ls-tree origin/task-0034-catalogstore-c4-pr3-resolver --name-only
   ai/reports/task-0034-implementer.md`); if missing, commit+push it to the
   branch (recurring gap).

2. **Re-run tests under CI mode.** `go vet ./...`, `go build ./...`,
   `go test -race -count=1 ./internal/catalogstore/...`, full
   `go test -race -count=1 ./...`, `make verify-generated`. All must pass.

3. **Coverage re-measurement + ADJUDICATION (load-bearing).**
   - Re-measure `internal/catalogstore`:
     `go test -count=1 -coverprofile=/tmp/cs.cov ./internal/catalogstore/... &&
     go tool cover -func=/tmp/cs.cov | tail -1`. Confirm the 88.9 % figure.
   - Identify the low-coverage functions (current low band is in `rebuild.go`:
     `rebuildSourceGlobalIndex` 72.7 %, `rebuildCatalogGlobalIndex` 72.7 %,
     `writeComponentGlobalIndexPlain` 72.7 %, `listAllSources` 78.9 %,
     `collectAllCatalogs` 77.8 %; plus `WriteRefs` 79.1 %).
   - Re-measure every adjacent floor and confirm held byte-for-byte:
     statestore ≥95.7, revision ≥90.3, executionstate ≥90.0, catalogmodel
     ≥91.1, sourcectx ≥91.1, catalogresolve ≥90.9.
   - **Three-branch adjudication (mirror Task 0033):**
     - **(a) PASS-with-attached-fix (preferred when the gap is real
       reachable branches):** the uncovered branches in `rebuild.go` are
       NEW production code (walk error paths, multi-source merge ordering,
       per-index-kind write-error surfacing). If they are reachable through
       the public `Resolver`/`Writer` API, attach focused tests (the
       implementer report names 6 rebuild test cases; the error-surface and
       multi-source-merge arms are the likely gaps) to lift
       `internal/catalogstore` to **≥ 90 %** (target 91 %). Commit the
       attached tests to PR #175 with a clear `test(catalogstore):` message,
       wait for CI green, THEN merge. Document exactly which branches the
       new tests cover in the report.
     - **(b) PASS-with-note (only if the residual gap is genuinely defensive
       returns gated by upstream validation, à la the PR-2 "Branches Left
       Unexercised" list):** document each uncovered branch, why it is
       unreachable through the public interface, and why a white-box hook is
       not worth it. Permitted ONLY if the re-measured number is ≥ 90 % after
       any cheap top-ups. **88.9 % as-is is below the floor and is NOT an
       acceptable PASS-with-note** unless you can prove the entire 1.1 %
       gap to 90 % is unreachable-defensive — which is unlikely given the
       low functions are new walk/merge/write logic, not validated-input
       guards.
     - **(c) FAIL:** if the gap reflects untested *reachable* logic that you
       cannot cheaply cover in-PR and the implementer must redo, leave PR
       open with explicit blockers and recommend Task 0035.1 implementer-fix.
   - State the chosen branch and the post-adjudication coverage number
     explicitly in the report.

4. **§4 reader fallback ladder code-path inspection.** Read `resolver.go`
   directly. Confirm for each method: refs-first → walk-fallback → typed
   sentinel on miss; `ResolveComponent` returns `ErrComponentNotFound` (NOT
   `ErrCatalogNotFound`) when the catalog is present but the manifest is
   missing; `RefSelector.Snapshot` is rejected with a "not yet implemented
   (C8)" envelope; the `errors.Is` chain threads
   `ErrCatalogNotFound`/`ErrComponentNotFound` → `statestore.ErrNotFound`;
   ctx-cancellation is honoured in every walk (tests inject a cancelled ctx).
   Confirm ZERO `ErrNotImplemented` surfaces remain in the package
   (`search_files pattern="ErrNotImplemented" path=internal/catalogstore`
   should show only the sentinel definition + stub-pin test history, no live
   returns from Resolver methods).

5. **T-STORE-3 byte-identity inspection.** Read `rebuild.go` and
   `rebuild_test.go`. Confirm the scrub-then-rebuild test asserts byte-for-byte
   equality across ALL THREE index kinds (`indexes/sources/*`,
   `indexes/catalogs/*`, `indexes/components/*`), that rebuild reuses
   `catalogmodel.PrettyEncode` and the `mergeComponentGlobalIndex` ordering
   from `indexes.go`, that ascending key sort is applied before write, and
   that empty-store rebuild is a documented no-op. Verify the multi-source
   previews-union case (`TestRebuildIndexes_MultiSourceUnionsPreviews`)
   actually exercises the main-vs-latest freshness pick and the Previews
   union+sort.

6. **No-raw-FS guard.** `search_files(target='content',
   pattern='^\s*"(os|io/ioutil|path/filepath)"',
   path='internal/catalogstore')` MUST be empty.

7. **Adjudicate the implementer's 3 open questions** (test-side CGI helper
   duplication; no stale-index scrub on rebuild; silent corrupt-manifest
   skip). For each: accept-and-document, OR file
   `ai/proposals/task-0035-spec-update.md` if it changes a spec contract.
   The stale-index-scrub question likely belongs to C8
   (`orun catalog validate --rebuild-indexes`) — record as a C8 risk, do
   not block C4 on it.

8. **Secret review.** Confirm no credentials/tokens in changed files or test
   fixtures (implementer used literal `src-…`/`cat-…` keys).

## Carry-forward risk to record (do NOT fix in this PR)

- **executionstate zero-margin coverage floor flap.** `internal/executionstate`
  sits at exactly 90.0 % locally and flapped to 89.6 % on one CI run of this
  PR (Linux + `-race`), then passed on rerun. It is byte-identical to main
  and untouched by C4. This is a latent CI-stability risk: any future PR can
  randomly red on it. Record it in the verifier report's Risk Notes and in
  `ai/context/open-risks.md` as a new entry (suggest **R-008**). Recommended
  follow-up (NOT part of this PR, orchestrator will scope separately if it
  recurs): add a 2–4 test buffer to `internal/executionstate` to lift it
  off the exact floor, OR widen the Makefile gate tolerance. Do not expand
  PR #175 to touch executionstate — that violates the PR boundary.

## Merge Protocol

- **PASS (after any path-(a) attached fix lands and CI is green):** squash-merge
  PR #175, checkout main, `git pull --ff-only`, delete the branch. Confirm
  C4 is now CLOSED (zero `ErrNotImplemented` Resolver surfaces; T-STORE-3
  green on main).
- **FAIL:** leave PR open, document blockers, recommend Task 0035.1.
- NEVER merge with verification blockers unresolved or CI red.

## After Merge — State Finalization (verifier updates these)

- `ai/context/task-ledger.md` — mark Task 0034 + 0035 verified/merged (PASS),
  record final coverage number and adjudication branch.
- `ai/state.json` — add `0034` to `completed`; set `current_task` to the next
  slot; `active_milestone` → `C5` (C4 closed); `repo_health` green;
  `last_verified` date; append a C4-closed note.
- `ai/context/current.md` — update milestone position to C5, note C4 closure.
- `ai/context/open-risks.md` — add R-008 (executionstate floor flap).
- `ai/context/orchestrator-brief.md` — the orchestrator rewrites this at
  cycle end (cycle 9); verifier need not.
- Commit verification artifacts to main.

## When Done Report

Save to `/ai/reports/task-0035-verifier.md` with the sections named under
"Required Outcomes". Be explicit about the coverage adjudication branch
chosen and the final measured number.
