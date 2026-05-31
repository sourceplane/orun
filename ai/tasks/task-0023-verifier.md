# Task 0023 — Verifier

Agent: Verifier

## Current Repo Context
- Implementer landed Phase 2 / Milestone C0 code half on branch
  `impl/task-0023-c0-catalogmodel` (commit `155d2f2`, head of PR #168).
  Implementer report at `ai/reports/task-0023-implementer.md`.
- New packages: `internal/catalogmodel/` (pure data, canonical encoder,
  JSON Schema, golden fixtures, property tests) and `internal/sourcectx/`
  (skeleton — model/keys/hash with stubs only).
- Build/CI: `Makefile` extends `test-state-redesign` to gate the new
  packages and adds `verify-generated`. The
  `.github/workflows/state-redesign-tests.yml` workflow gates both targets
  on every PR + push to main.
- PR CI on `155d2f2`: `state-redesign-tests / test` SUCCESS,
  `CI / Orun Plan` SUCCESS, `Harness dry-run guard` SUCCESS, matrix legs
  legitimately SKIPPED.

## Objective
Verify PR #168 against the Task 0023 Implementer prompt
(`ai/tasks/task-0023.md`) and the Phase 2 spec set
(`specs/orun-component-catalog/`). Confirm the C0 "done when"
checklist holds, Phase 1 invariants are preserved, and the package is
leaf-clean. PASS → merge, sync main, leave repo clean. FAIL → leave PR
open with blockers.

## PR Boundary (must match implementer scope exactly)
- New `internal/catalogmodel/` package — pure data models, canonical
  encoder, ID/key helpers, sanitizers, JSON Schema generator + committed
  artifact, golden fixtures + roundtrip tests + property tests
  (T-IDK-1, T-IDK-3, T-IDK-5).
- New `internal/sourcectx/` skeleton — types-only (`model.go`,
  `keys.go`, `hash.go`) + at least one `_test.go`.
- `Makefile`: `test-state-redesign` extended; new `verify-generated`
  target.
- `.github/workflows/state-redesign-tests.yml`: gates the new targets.
- Implementer report at `ai/reports/task-0023-implementer.md`.

Out of scope for this PR (verifier flags as scope creep if found):
resolver logic, `internal/catalogstore`, `internal/catalogresolve`,
`internal/catalogsync`, any `orun catalog *` CLI surface, any plan/run
flag wiring, any FS writes under `.orun/sources/` or `.orun/catalogs/`,
any Phase 1 field renames, any coverage-floor reductions on
`internal/statestore`, `internal/revision`, `internal/executionstate`,
`internal/triggerctx`.

## Read First
- `ai/tasks/task-0023.md` — implementer prompt (acceptance criteria).
- `ai/reports/task-0023-implementer.md` — implementer self-report.
- `specs/orun-component-catalog/README.md`,
  `data-model.md` (§§1–11), `identity-and-keys.md` (§§1–6, §11, §12),
  `implementation-plan.md` (Milestone C0), `test-plan.md` (§1, §2 for
  T-IDK-1 / T-IDK-3 / T-IDK-5).
- `agents/orchestrator.md` — Verifier Standard, Verifier Merge Protocol.

## Verification Checklist
1. **Branch / PR shape**
   - `gh pr view 168 --json …` confirms one branch, one PR, mapped to
     Task 0023.
   - `git diff --stat origin/main...impl/task-0023-c0-catalogmodel`
     touches only the file inventory above. No edits to Phase 1
     packages, no edits to `agents/orchestrator.md`, no changes to
     `specs/`.
2. **Build / vet / test**
   - `go build ./...`
   - `go vet ./...`
   - `go test ./... -count=1 -race -timeout 600s`
3. **State-redesign gate**
   - `make test-state-redesign` — Phase 1 floors held byte-for-byte
     (`internal/statestore` ≥ 95.7 %, `internal/revision` ≥ 90.3 %,
     `internal/executionstate` ≥ 90.0 %, `internal/triggerctx` passes).
     New `internal/catalogmodel` ≥ 90 % coverage. `Sanitize*` == 100 %.
     `internal/sourcectx` builds and tests green.
4. **Generator drift**
   - `go generate ./internal/catalogmodel`
   - `git diff --exit-code internal/catalogmodel/schema/`
   - `make verify-generated`
5. **Leaf-clean invariant**
   - `go list -deps ./internal/catalogmodel/...` — no `internal/*`
     imports other than the package itself + stdlib + third-party.
6. **Spec drift**
   - JSON tags lowerCamelCase per `data-model.md`.
   - ULID prefixes `src_/cat_/cmp_` per `identity-and-keys.md` §6.
   - Sanitizers per `identity-and-keys.md` §12 (total, panic-free).
   - Canonical encoder is the only path for hashed payloads
     (no bare `encoding/json` for hash inputs in code paths).
7. **Property gates (T-IDK-1, T-IDK-3, T-IDK-5)**
   - `internal/catalogmodel/property_test.go` runs and asserts
     order-invariant canonical encode (T-IDK-1), `manifestHash`
     provenance invariant (T-IDK-3), sanitizer totality (T-IDK-5).
8. **Secrets / fixtures**
   - No plaintext tokens, API keys, passwords, or full credentials in
     fixtures or test output.
9. **PR CI logs**
   - `gh run view 26704256666` — `state-redesign-tests / test` SUCCESS.
     Inspect logs to confirm the make targets actually ran (not just
     the workflow listing them).
   - `Orun Plan` and `Harness dry-run guard` SUCCESS.
10. **Local Orun checks**
    - `kiox -- orun validate --intent intent.yaml` if `intent.yaml`
      exists; otherwise record no-op.
    - `kiox -- orun plan --changed --intent intent.yaml --output
      plan.json` if scaffolded; otherwise record no-op.
    - `kiox -- orun run --plan plan.json --dry-run --runner
      github-actions` if a plan was produced; otherwise record no-op.

## Acceptance Criteria
✅ PR #168 maps exactly to Task 0023 with no scope creep.
✅ All build / vet / test commands above pass locally.
✅ `make test-state-redesign` green; Phase 1 floors held; new floors hit.
✅ `make verify-generated` green; no schema drift.
✅ `go list -deps ./internal/catalogmodel/...` shows leaf-clean.
✅ Property tests cover T-IDK-1, T-IDK-3, T-IDK-5.
✅ No secret material in fixtures or logs.
✅ PR CI green; logs show the make targets actually executed.
✅ Phase 1 invariants preserved (no field renames, no coverage
   regressions, no removed CLI workflows).

## Verifier Merge Protocol
- If PASS: merge PR #168 with `gh pr merge 168 --squash --admin`
  (this repo's standard pattern), checkout `main`, fast-forward pull
  from `origin/main`, delete the local task branch, run `git status
  --short` to confirm clean.
- If FAIL: leave PR open with clear blockers. Do not merge with
  unresolved verification blockers.

## When Done Report
Write `/ai/reports/task-0023-verifier.md` with:
- `Result: PASS|FAIL`
- `Checks` — each verification step with command + outcome
- `CI Log Review` — `state-redesign-tests` run inspection
- `Issues` — blockers + non-blocking concerns
- `Spec Proposals` — any drift surfaced
- `Risk Notes` — residual risk (e.g. bespoke schema generator,
  `genfixtures` main package shipping in module)
- `Recommended Next Move` — Task 0024 = Milestone C1
  (`internal/sourcectx` resolver — Git probe, `treeHash`, `dirtyHash`,
  `catalogInputHash` populating `WorkspaceState`)
