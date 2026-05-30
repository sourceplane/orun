# Task 0006 — Verifier (closes Milestone M2)

Agent: Verifier

## Current Repo Context

- Active spec: `specs/orun-state-redesign/` (Phase 1, local-only). Active milestone: **M2 — `internal/statestore`**. PR-A (#154 → `9b0a39c`) and PR-B (#155 → `0fa2111`) landed and were verified PASS on 2026-05-29 / 2026-05-30.
- Implementer Task 0005 (M2 PR-C — typed `refs.go` + `indexes.go` + `RebuildIndexes()` stub) is complete. PR **#156** (`impl/task-0005-m2-statestore-prc`) is OPEN, MERGEABLE, mergeStateStatus CLEAN, with both required CI checks SUCCESS:
  - `CI / Orun Plan` — run `26671612378`
  - `orun remote-state conformance / Harness dry-run guard` — run `26671612360`
- Implementer report at `ai/reports/task-0005-implementer.md` (PR #156, coverage 96.1 % on `internal/statestore`, leaf-clean confirmed, no production-caller wiring, no spec proposals).
- After PR #156 merges and is verified, **Milestone M2 closes** and M3 (`internal/revision`) becomes the next implementer milestone.

## Objective

Validate Task 0005 / PR #156 against the Verifier Standard in `agents/orchestrator.md` and the **M2 "Done when"** criteria in `specs/orun-state-redesign/implementation-plan.md`. Confirm typed refs/indexes match `data-model.md` §6 / §7 and `state-store.md` §1 / §2.1 / §3.3 byte-for-byte, that all M2 PR-A / PR-B contracts remain unchanged, and that PR #156 carries no overreach. On PASS, merge PR #156, sync local `main`, and leave the working tree clean.

## PR Boundary

- Verification only. Verifier may commit:
  - `ai/reports/task-0006-verifier.md` (this report) to the PR branch.
  - Any tiny verification-only fix that is strictly necessary to make the PR mergeable (e.g. typo, stray TODO removal). Anything beyond that → FAIL with blockers, do not edit.
- Out of scope: spec edits, `internal/revision` (M3) work, production-caller wiring, retroactive changes to PR-A / PR-B surface, refactor of refs/indexes.

## Read First

- `agents/orchestrator.md` — Verifier Standard + Verifier Merge Protocol (sections covering verifier responsibilities and merge handling).
- `specs/orun-state-redesign/README.md` — index + read order.
- `specs/orun-state-redesign/implementation-plan.md` — Milestone **M2** goal, suggested PR scope, and "Done when" checklist.
- `specs/orun-state-redesign/data-model.md` — §6 (LatestRevisionRef, LatestExecutionRef, TriggerRef latest + scope, NamedRef) and §7 (RevisionIndexEntry, ExecutionIndexEntry).
- `specs/orun-state-redesign/state-store.md` — §1 (frozen interface), §2.1 (path alphabet + helpers), §3.3 (CAS semantics), §6 (loser-retry contract: callers retry, not the store).
- `specs/orun-state-redesign/test-plan.md` — coverage targets and JSON-byte-stability requirement.
- `ai/tasks/task-0005.md` — implementer prompt (scope + constraints).
- `ai/reports/task-0005-implementer.md` — implementer report.
- PR **#156** diff and commits on branch `impl/task-0005-m2-statestore-prc`.

## Required Outcomes

- [ ] Verifier report at `ai/reports/task-0006-verifier.md` with sections: Result, Checks, Issues, CI Log Review, Risk Notes, Spec Proposals, Recommended Next Move.
- [ ] PR #156 either squash-merged into `main` (PASS) or left OPEN with explicit blockers (FAIL).
- [ ] If PASS: local `main` fast-forwarded to `origin/main`; PR branch deleted; `git status --short` clean.
- [ ] State.json `task_agent` updated to `/ai/tasks/task-0006-verifier.md` while this verifier task is in flight (the orchestrator already set it on emission).

## Constraints

1. **No production-code edits.** A verifier-only typo fix is permissible only if mergeability requires it.
2. **No spec edits.** If drift is found, file a proposal under `ai/proposals/task-0005-spec-update.md` instead of editing the spec.
3. **PR-A / PR-B surface unchanged.** Confirm the PR-C diff does not touch `paths.go`, `errors.go`, `store.go`, `local.go`, the rapid suite, the four error sentinels, or any file outside `internal/statestore/{refs,indexes}{,_test}.go`. If anything else changed, justify or FAIL.
4. **Leaf-clean.** `internal/statestore` must continue to import zero `internal/*` packages (`go list -deps ./internal/statestore | grep "/orun/internal/"` returns only the package itself).
5. **Coverage gate ≥ 95 % on `internal/statestore`** (M2 stretch ≥ 96 %). PR-C reports 96.1 %; re-measure locally.
6. **No production-caller wiring.** `cmd/orun`, `internal/state`, `internal/runner`, `internal/runbundle` must be byte-identical to `origin/main`.
7. **CI is authoritative for `orun plan --changed`.** The local composition-cache quirk (`stack.yaml at ~/.orun/cache/compositions/c41fc08… has no spec.compositions`) is a known environment artifact carried since Task 0001. Reproducing it is not a blocker; failing CI is.
8. **Merge gate**: never merge unless BOTH local quality gates AND the two required CI checks (`CI / Orun Plan`, `Harness dry-run guard`) are SUCCESS at log level on the latest commit on the PR branch.

## Verification Steps

Run, in order:

### 1. Repo State

```bash
git fetch origin
git status --short
git log --oneline -5 origin/main
gh pr view 156 --json number,state,mergeable,mergeStateStatus,headRefName,headRefOid,statusCheckRollup
```

Confirm: PR is OPEN, MERGEABLE, CLEAN, head SHA matches the local PR branch tip you'll inspect.

### 2. Diff Audit

```bash
git fetch origin pull/156/head:pr-156
git diff --stat origin/main...pr-156
git diff origin/main...pr-156 -- cmd/orun internal/state internal/runner internal/runbundle
git diff origin/main...pr-156 -- internal/statestore/paths.go internal/statestore/errors.go internal/statestore/store.go internal/statestore/local.go
```

The first wider stat call is for inspection; the last two narrow calls MUST be empty. If non-empty, FAIL with the exact lines.

### 3. Spec Conformance (code-path inspection)

Open `internal/statestore/refs.go` and `internal/statestore/indexes.go` side-by-side with `data-model.md` §6 / §7 and `state-store.md` §1 / §2.1 / §3.3. Check:

- Every ref/index struct field name + JSON tag matches the spec table byte-for-byte.
- All paths come from `paths.go` helpers — no `"refs/"`, `"indexes/"`, or string concatenation literals in `refs.go` / `indexes.go`. Run the `TestRefs_NoStringConcatenationInPaths` source-guard test.
- CAS helpers take `prev ObjectMeta` and forward `prev.Revision` directly to `StateStore.CompareAndSwap` — they do NOT re-read inside the helper (per `state-store.md` §6 caller-owns-retry).
- Index writers use `CreateIfAbsent` — duplicate writes return `ErrExists`.
- `RebuildIndexes(ctx, store)` is a stub returning `fmt.Errorf("%w: …deferred…", ErrInvalid)`.
- JSON marshalling: `SetIndent("", "  ")`, `SetEscapeHTML(false)`, trailing `\n`. Confirm a representative `internal/testfx/statefs.AssertJSONFile` byte-stability test exists for at least one ref shape and one index shape.
- TriggerRef helper covers both `latest.json` and `<scope>.json` shapes (single struct with `Latest bool` + `Scope string` is acceptable, both forms must reach the right path).
- No new error sentinels introduced; all errors wrap one of the four existing sentinels via `fmt.Errorf("%w: …", ErrX, …)`.
- No new path helpers added to `paths.go` (check via diff in step 2; if any were added without a proposal → FAIL).

### 4. Local Quality Gates

```bash
go build ./...
go vet ./...
go test -race -count=1 ./internal/statestore/...
make test-state-redesign        # confirm coverage gate prints "measured: <≥95.0>%"
go test -cover ./internal/statestore/...   # second-source coverage measurement
go list -deps ./internal/statestore | grep "/orun/internal/" || echo "leaf-clean"
kiox exec -- orun validate --intent examples/intent.yaml
kiox exec -- orun plan --changed --intent examples/intent.yaml --output /tmp/plan-0006.json || \
  echo "EXPECTED: composition-cache quirk on local; CI is authoritative"
kiox exec -- orun run --plan /tmp/plan-0006.json --dry-run --runner github-actions || \
  echo "skipped because plan not produced; record no-op"
```

All non-quirk steps must exit 0. Coverage on `internal/statestore` must be ≥ 95 % (target ≥ 96 %).

### 5. CI Log Review

Inspect both required CI runs at log level (not just summary):

```bash
gh run view 26671612378 --log-failed | head -200    # CI / Orun Plan
gh run view 26671612378 --log | rg -n "orun plan" | head -20
gh run view 26671612360 --log | rg -n "guard\] PASS:" | head -50
```

Confirm:

- `CI / Orun Plan` (run `26671612378`) actually invoked `orun plan --from-ci github …` against `examples/intent.yaml` and recorded the legitimate empty-matrix shape (`0 components × 3 envs → 0 jobs`). Plan artifact uploaded.
- `Harness dry-run guard` (run `26671612360`) emitted the full `[guard] PASS:` battery (bash syntax, command-count thresholds, duplicate-claim helper PASS+FAIL, status helper PASS+FAIL, exported env asserts).
- No SKIPPED required-check leg slipped through that should have RUN.

### 6. Secret Hygiene & Production-Grade Basics

```bash
rg -n -i "(token|password|secret|key=)" -- internal/statestore/refs.go internal/statestore/indexes.go internal/statestore/refs_test.go internal/statestore/indexes_test.go || echo "clean"
```

Confirm: no plaintext tokens, no logging of sensitive material, deterministic JSON, all exported symbols have doc comments (M2 "Done when" requires public docs on every exported symbol).

### 7. Decision and Merge

If every step is green:

```bash
gh pr review 156 --approve --body "Verified PASS — Task 0006 (M2 PR-C). See ai/reports/task-0006-verifier.md."
gh pr merge 156 --squash --delete-branch
git checkout main
git pull --ff-only origin main
git status --short
```

If any blocker exists, leave PR OPEN and write the verifier report with `Result: FAIL` and explicit blockers. Do NOT merge.

## Acceptance Criteria

- ✅ PR #156 corresponds 1:1 to Task 0005 / M2 PR-C; no overreach (only `internal/statestore/refs.go|indexes.go|refs_test.go|indexes_test.go` changed).
- ✅ All required CI checks on PR #156 are SUCCESS at log level (`CI / Orun Plan`, `Harness dry-run guard`).
- ✅ `internal/statestore` coverage ≥ 95 % locally (M2 stretch ≥ 96 % target).
- ✅ Leaf-clean (`go list -deps` returns only the package itself).
- ✅ Refs/indexes structs and paths byte-match `data-model.md` §6 / §7 and `state-store.md` §1 / §2.1.
- ✅ CAS helpers forward `prev.Revision` without re-reading; index writers use `CreateIfAbsent`.
- ✅ JSON byte-stability enforced by at least one `internal/testfx/statefs.AssertJSONFile` test per file.
- ✅ All exported symbols carry doc comments (M2 "Done when" requires this cross-cutting).
- ✅ No new error sentinels; no new path helpers without a proposal; no string concatenation for paths.
- ✅ No production-caller wiring (`cmd/orun`, `internal/state`, `internal/runner`, `internal/runbundle` byte-identical to `origin/main`).
- ✅ On PASS: PR #156 squash-merged, branch deleted, local `main` fast-forwarded, `git status --short` clean.
- ✅ Verifier report `ai/reports/task-0006-verifier.md` filed and committed (to `main` post-merge or to PR branch pre-merge per Verifier Merge Protocol).

## When Done Report

Save to: `/ai/reports/task-0006-verifier.md`

Sections:

- Result: PASS | FAIL
- Checks (every command from steps 1–6 with exit code / outcome)
- CI Log Review (run IDs, expected commands actually executed, evidence quoted)
- Issues (blocking or non-blocking, ranked)
- Risk Notes (residual risk after merge — e.g. `RebuildIndexes` stub, in-process CAS mutex, PR-C surface deferred to M3 callers)
- Spec Proposals (links + one-line reason; expected to be **None** per implementer)
- Recommended Next Move (M3 PR-A scoping if PASS; remediation steps if FAIL)
- PR Number (156) and merge commit SHA (post-merge) or "OPEN — blocked" (FAIL)
