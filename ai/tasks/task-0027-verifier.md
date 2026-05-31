# Task 0027 — Verifier pass for PR #171 (C2 PR-2)

Agent: Verifier

## Current Repo Context
- Task 0025 closed C2 PR-1 (`DiscoverAndLoad`) via PR #170 squash `723be32`. `internal/catalogresolve` shipped to `main` with deterministic 90.0% coverage.
- Task 0026 implementer added the second `internal/catalogresolve` PR — top-level `Resolve(ctx, opts) (*ResolvedCatalog, []ValidationIssue, error)` covering resolution-pipeline stages 4/5/6/7/8/9/10 plus `manifestHash`. PR #171 OPEN on branch `task-0026-catalogresolve-c2-pr2` @ `9c65e7c`. Implementer report at `ai/reports/task-0026-implementer.md`.
- All required CI checks SUCCESS at orchestrator handoff: `Orun Plan`, `Harness dry-run guard`, `test`. Matrix legs SKIPPED legitimately. mergeable=MERGEABLE, mergeStateStatus=CLEAN.
- C2 closes with this PR; C3 (`CatalogSnapshot` + graph builder + `catalogHash`) opens on PASS.

## Objective
Validate PR #171 against the Verifier Standard in `agents/orchestrator.md` and the Milestone C2 "done when" criteria in `specs/orun-component-catalog/implementation-plan.md` §C2. On PASS, merge per the Verifier Merge Protocol and close Milestone C2. On FAIL, leave PR open with documented blockers.

## PR Boundary (must match exactly)
1. New files in `internal/catalogresolve/`: `assemble.go`, `clock.go`, `dependencies.go`, `errors.go`, `hash.go`, `infer.go`, `resolve_full.go`, `validate.go`, plus `resolve_full_test.go` and `testdata/resolve_e2e/`, `testdata/resolve_cycle/` fixtures.
2. Additive edits to `internal/catalogresolve/intent.go` (intentInference pointer-mirror so optional inference toggles are distinguishable from zero-values) and `internal/catalogresolve/types.go` (add `ResolvedCatalog`; add `Options.{Strict, Repo, Namespace, Clock}`).
3. Implementer report at `ai/reports/task-0026-implementer.md` and task prompt `ai/tasks/task-0026.md`.

**Non-goals / explicit overreach guards:**
- NO edits outside `internal/catalogresolve/` (per the C2 PR-1 verifier-adopted convention: "one additive file per cross-package contract surface in `internal/catalogmodel/`, no edits to existing source files"; Task 0026 must not need any new file in `catalogmodel/` or `sourcectx/`).
- NO CLI wiring, NO `catalogstore/`, NO graph builder (deferred to C3 / C4).
- NO Phase 1 surface changes (`internal/statestore`, `internal/revision`, `internal/executionstate`, `internal/triggerctx` byte-identical to `origin/main`).

## Read First
- `ai/tasks/task-0026.md` — implementer prompt (original scope contract).
- `ai/reports/task-0026-implementer.md` — implementer self-report (decisions, coverage, files changed).
- `specs/orun-component-catalog/implementation-plan.md` §C2 — milestone "done when".
- `specs/orun-component-catalog/resolution-pipeline.md` — stages 4 / 5 / 6 / 7 / 8 / 9 / 10 normative behaviour.
- `specs/orun-component-catalog/identity-and-keys.md` §10 — `manifestHash` definition (provenance excluded).
- `specs/orun-component-catalog/test-plan.md` — T-RES-1, T-RES-2, T-RES-3, T-RES-4 expectations.
- `agents/orchestrator.md` — Verifier Standard + Verifier Merge Protocol.
- `ai/context/current.md` and `ai/context/task-ledger.md` (Task 0025 + Task 0026 entries).
- `ai/proposals/task-0025-spec-update.md` — adopted convention for additive sibling files in `catalogmodel/` (relevant if Task 0026 introduced any).

## Required Outcomes
- [ ] Verifier report at `ai/reports/task-0027-verifier.md` with: Result (PASS/FAIL), Checks, Issues, Risk Notes, Spec Proposals, Recommended Next Move.
- [ ] PR #171 merged via squash on PASS (Verifier Merge Protocol); branch deleted; main fast-forwarded locally.
- [ ] If PASS, post-merge state-file updates pushed on `main`: `ai/state.json` (current_task → next, completed += "0026", task_agent updated), `ai/context/current.md` (mark Task 0026 ✅, mark C2 ✅, update Repo Checkpoint, scope C3 next), `ai/context/task-ledger.md` (Task 0026 entry updated to "verified PASS and merged"), `ai/waiting_for_input.md` (cleared).
- [ ] On FAIL, leave PR #171 open, file blocker list in verifier report, do not touch state files beyond appending the FAIL note to `ai/context/task-ledger.md` Task 0026 entry.

## Verification Checklist

### A. PR boundary fidelity
- `gh pr diff 171 --name-only` returns ONLY files inside `internal/catalogresolve/` plus `ai/reports/task-0026-implementer.md` and `ai/tasks/task-0026.md`.
- `git diff origin/main...origin/task-0026-catalogresolve-c2-pr2 -- internal/catalogmodel/ internal/sourcectx/ internal/statestore/ internal/revision/ internal/executionstate/ internal/triggerctx/ cmd/orun/ examples/` is EMPTY.
- Confirm `intent.go` and `types.go` edits are additive only (no removed exports, no signature changes that would break C2 PR-1 callers).

### B. Spec conformance (resolution-pipeline.md)
- Stage 4 (infer): `infer.go` runs after inheritance, gated by `intent.catalog.inference.*`, `recover()`-safe, emits warn-severity `ErrInferenceFailed` on panic instead of bubbling.
- Stages 5/6 (validate pre-deps): typed issues collected; strict-mode promotes warn → error.
- Stage 7 (assemble): output `[]ComponentManifest` sorted by `componentKey`.
- Stage 8 (deps): cross-component refs resolved; missing ref ⇒ `ErrDependencyMissing` carrying both endpoints (from + to).
- Stage 9 (validate post-deps): `deploy-after` cycle = error always; `calls` cycle = warn (default) / error (strict mode).
- Stage 10 (`manifestHash`): computed via `catalogmodel.CanonicalEncode`; provenance (`resolution.inheritedFrom`) automatically excluded per `identity-and-keys.md` §10. Self-reference (`source.manifestHash`) set after computation.

### C. C2 "done when" (implementation-plan.md §C2)
- `internal/catalogresolve` coverage ≥ 90%.
- T-RES-1: `Resolve` called twice on same fixture produces byte-identical `[]ComponentManifest` (asserted in `resolve_full_test.go`).
- T-RES-2: provenance populated for every inherited / inferred field.
- Broken dependency reports `ErrDependencyMissing` with both endpoints.
- `deploy-after` cycle aborts; `calls` cycle warns by default.
- Duplicate `componentKey` is a resolver error before persistence (`ErrDuplicateComponent` or equivalent).

### D. Local quality gates (must all exit 0)
```
go build ./...
go vet ./...
make verify-generated
go test -race -count=1 ./...
go test -race -count=3 ./internal/catalogresolve/...
go test -cover ./internal/catalogresolve/... ./internal/catalogmodel/... ./internal/sourcectx/... ./internal/statestore/... ./internal/revision/... ./internal/executionstate/...
make test-state-redesign
kiox -- orun validate --intent intent.yaml
```
Confirm coverage floors:
- `internal/catalogresolve` ≥ 90% (implementer reports 90.2%).
- `internal/catalogmodel` ≥ 91.1% (Phase 2 floor — must not regress).
- `internal/sourcectx` ≥ 91.1%.
- `internal/statestore` ≥ 95.7%, `internal/revision` ≥ 90.3%, `internal/executionstate` ≥ 90.0% (Phase 1 floors — byte-for-byte hold).

### E. CI evidence at log level
- `gh pr view 171 --json statusCheckRollup,mergeable,mergeStateStatus` — all required checks SUCCESS, MERGEABLE, CLEAN.
- `gh run view <orun-plan-run-id>` confirms real `orun plan` invocation (not a stub) on PR head.
- `Harness dry-run guard` shows full `[guard] PASS:` battery.

### F. Determinism stress
- `go test -count=10 -race ./internal/catalogresolve/...` — zero failures (no rapid-driven variance, fixture hashes byte-stable).

### G. Errors and typed surface
- `errors.Is(err, ErrDependencyMissing)`, `errors.Is(err, ErrCycle)`, `errors.Is(err, ErrDuplicateComponent)`, `errors.Is(err, ErrInferenceFailed)` — all sentinel paths exercised in tests.
- No raw `fmt.Errorf` paths that bypass the sentinel taxonomy for the four documented failure classes.

### H. Secret / overreach audit
- `search_files(target='content', pattern='(?i)(API_KEY|TOKEN|SECRET|PASSWORD)=', file_glob='*.go,*.yaml,*.yml')` — no committed credentials.
- No new exported symbols outside the documented `Resolve`, `ResolvedCatalog`, `Options`, `ValidationIssue`, error sentinels, and `Clock` seam surface.

## Verifier Merge Protocol
On PASS AND CI green:
1. `gh pr review 171 --approve --body "<one-line-summary>"`
2. `gh pr merge 171 --squash --delete-branch`
3. `git checkout main && git pull --ff-only origin main`
4. Verify squash commit SHA: `git log --oneline -1`
5. Update state files (state.json, current.md, task-ledger.md, waiting_for_input.md) per Required Outcomes above.
6. `git add ai/state.json ai/context/current.md ai/context/task-ledger.md ai/waiting_for_input.md ai/reports/task-0027-verifier.md && git commit -m "Task 0027 verifier PASS: C2 closed (PR #171 merged); scope C3" && git push origin main`
7. Confirm post-merge `main` CI green.

On FAIL: do NOT merge. File the blocker list in the verifier report. Append a FAIL note to the Task 0026 ledger entry. `waiting_for_input.md` may flip to true if a human decision is needed; otherwise orchestrator scopes a corrective task.

## When Done Report
Save `ai/reports/task-0027-verifier.md` with sections:
- Result: PASS or FAIL
- Checks (A–H above, each with the command run + observed output summary)
- Issues (severity-tagged)
- CI Log Review (run IDs + key log lines)
- Local Resource Evidence (coverage numbers, test counts, determinism stress result)
- Spec Proposals (file `ai/proposals/task-0026-spec-update.md` only if a normative gap is found)
- Risk Notes (residual risks carried into C3+)
- Recommended Next Move (Task 0028 = C3 implementer, or remediation task if FAIL)
