# Task Ledger — orun-state-redesign lineage

Task numbering restarted at 0001 with the 2026-05-30 pivot from
`.kiro/specs/orun-tui-cockpit/` (paused at PR #146 merged) to
`specs/orun-state-redesign/` (Phase 1 local-only trigger-first revision-first
local state model).

Prior TUI cockpit tasks (0139–0147 with sub-tasks) are preserved in git
history; reports/prompts are no longer present in the working tree.

---

## Task 0001

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0001.md`
|- Status: **implemented and merged via PR #152** (2026-05-29T19:11:11Z, main commit `4ea1980e`)
|- Milestone: M0 — Foundation
|- Objective: pin `github.com/oklog/ulid/v2`, scaffold `internal/testfx/statefs`
   helper package with `NewWorkspace`, `AssertJSONFile`, and `ReadJSON[T]`,
   and add `make test-state-redesign` (initially a no-op).
|- Scope boundary: `go.mod`/`go.sum`, `internal/testfx/statefs/`, `Makefile`.
   No production-code or CLI-surface changes. Implementer may split into two
   PRs (dependency pin vs. harness) if cleaner; both must land before M1.
|- Acceptance: `go build ./...` and `go test ./...` green; unit tests for all
   three helpers; `internal/testfx/statefs` imports no other `internal/` package;
   `make test-state-redesign` target exists and runs.
|- Expected outcome: M1 (`internal/triggerctx`) and M2 (`internal/statestore`)
   are unblocked.
|- Implementer outcome (2026-05-29): PR **#152** opened on branch
   `impl/task-0001-m0-foundation` @ `628c212`. Status `OPEN`, `MERGEABLE`,
   `mergeStateStatus=CLEAN`, all required CI checks `SUCCESS`. Implementer
   report: `ai/reports/task-0001-implementer.md`. Awaiting verifier.

## Task 0001 (Verifier pass)

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0001-verifier.md`
|- Status: **verified PASS and merged** (2026-05-29T19:11:11Z)
|- Verifying: PR #152 (`impl/task-0001-m0-foundation` @ `628c212`)
|- Implementation: PR **#152** squash-merged → main commit
   `4ea1980e8822d21908b09ab70316fe3953bb75ce`. Branch deleted.
|- PR CI: `CI / Orun Plan` SUCCESS (run `26656333958`, real plan output
   `0 components × 3 envs → 0 jobs`, plan artifact uploaded);
   `orun remote-state conformance / Harness dry-run guard` SUCCESS
   (run `26656333939`, all `[guard] PASS:` assertions ran). Matrix legs
   SKIPPED legitimately (empty matrix at M0).
|- Local checks: `go build ./...`, `go vet ./...`, `go test ./...`,
   `go test -count=1 ./internal/testfx/statefs/...`, `make test-state-redesign`,
   `go list -m github.com/oklog/ulid/v2` (→ `v2.1.1`),
   `go list -deps ./internal/testfx/statefs` (leaf-clean) all exit 0.
   `kiox -- orun validate` passes; `kiox -- orun plan --changed` fails locally
   on a pre-existing composition-cache env quirk that reproduces on main
   `d2ab48e` and is exercised successfully in CI.
|- Reports: implementer at `ai/reports/task-0001-implementer.md`; verifier at
   `ai/reports/task-0001-verifier.md`.
|- Objective: validate Task 0001 against the Verifier Standard in
   `agents/orchestrator.md` and the M0 "done when" criteria in
   `specs/orun-state-redesign/implementation-plan.md`; enforce non-goals;
   resolve the `flyingmutant/rapid` → `pgregory.net/rapid` spec drift.
|- Scope boundary: verification only (plus the verifier report committed to
   the PR branch as a verifier-only artifact); no production-code edits.
|- Durable outcome on main: `github.com/oklog/ulid/v2 v2.1.1` direct require,
   `internal/testfx/statefs` test harness with happy + fakeT-driven failure-
   path coverage, `make test-state-redesign` target, full
   `specs/orun-state-redesign/` spec pack, `agents/orchestrator.md` updates,
   rebuilt `ai/` lineage, TUI-era `task-014*.md` / `report task-014*.md`
   deleted. `flyingmutant/rapid` drift formally deferred to Task 0002 with
   rationale (forcing function: first real `rapid` import will fail to compile
   under the wrong path).

## Historical Notes

- 2026-05-30: roadmap pivoted from TUI cockpit (Phase 3) to orun-state-redesign
  Phase 1. `agents/orchestrator.md` and `specs/orun-state-redesign/` set the
  new authoritative spec pack. `ai/` directory rebuilt by orchestrator under
  the new lineage.
