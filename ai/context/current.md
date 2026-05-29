# Current Roadmap Position

## Active Spec
`specs/orun-state-redesign/` (Phase 1, local-only) — trigger-first revision-first
local state model. See `specs/orun-state-redesign/README.md` for the index and
read order.

## Active Milestone
**M1 — `internal/triggerctx`.** Build the trigger context model that every
downstream milestone (statestore, revision, executionstate, CLI) consumes.

## Current Task (0002 — Implementer, queued for prompt emission)
- Agent: Implementer
- Prompt: `ai/tasks/task-0002.md` (to be authored by the orchestrator next).
- Scope (per `specs/orun-state-redesign/implementation-plan.md` M1): model
  `TriggerOccurrence`, `TriggerSource`, `PlanScope`; ULID-prefixed ID generator
  (`trg_`); system trigger constructors; `FromDeclaredTrigger` +
  `ResolveProviderEvent` wrapping `internal/trigger`; top-level
  `ResolveTriggerContext` dispatcher. ≥90 % coverage. Property test on
  `TriggerKey` stability + format.
- M1 task scope must also include the two M0→M1 hand-off chores called out in
  Task 0001's verifier report (`ai/reports/task-0001-verifier.md`):
  1. Delete `internal/testfx/statefs/tools.go` once the first real production
     import of `github.com/oklog/ulid/v2` lands and confirm `go mod tidy` keeps
     the require in the **direct** block.
  2. Use the correct `rapid` import path empirically (the spec text says
     `github.com/flyingmutant/rapid` but `go.mod` already pins
     `pgregory.net/rapid v1.1.0` — same module, new home). File
     `ai/proposals/task-0002-spec-update.md` to correct
     `specs/orun-state-redesign/test-plan.md §3` to match what compiles.

## Last Completed Task (0001 — Verifier PASS)
- Verifier prompt: `ai/tasks/task-0001-verifier.md`
- Verifier report: `ai/reports/task-0001-verifier.md`
- PR **#152** (`impl/task-0001-m0-foundation` @ `628c212`) verified and merged
  via squash on 2026-05-29T19:11:11Z → main commit `4ea1980e`.
- Durable outcome on main: `github.com/oklog/ulid/v2 v2.1.1` direct require,
  `internal/testfx/statefs` test harness (`NewWorkspace`, `AssertJSONFile`,
  `ReadJSON[T]`, fakeT-driven failure-path coverage), `make test-state-redesign`
  target, full `specs/orun-state-redesign/` spec pack, `agents/orchestrator.md`
  updates, rebuilt `ai/` tree under the new lineage, deletion of all TUI-era
  `ai/tasks/task-014*.md` and `ai/reports/task-014*.md` files.

## Repo Checkpoint

| Attribute | Value |
|---|---|
| Branch | main (synced with origin/main) |
| Last commit on main | `4ea1980` — Task 0001: Milestone M0 — state-redesign foundation (deps, testfx/statefs, Makefile) (#152) |
| Open PRs | none for the state-redesign lineage |
| Repo health | 🟢 Green |
| Last verified | 2026-05-29 (Task 0001, PR #152) |
| Active milestone | M1 (`internal/triggerctx`) — implementer queued |

## Roadmap (M0 → M6)
1. ✅ **M0 Foundation** — landed on main at `4ea1980`.
2. **M1 `internal/triggerctx`** ← current
3. M2 `internal/statestore` (local driver) — contract frozen here
4. M3 `internal/revision`
5. M4 `internal/executionstate` + runner bridge
6. M5 CLI rewire (`orun plan/run/status/logs/describe/get plans` + hidden `state migrate`)
7. M6 End-to-end + property gates

## Known Spec Drift / Open Questions
- `specs/orun-state-redesign/test-plan.md` §3 still names
  `github.com/flyingmutant/rapid`; `go.mod` pins `pgregory.net/rapid v1.1.0`
  (same module, current import path). Formally deferred to Task 0002 — see
  the verifier report's "Spec Proposals" section for the rationale and forcing
  function.
- Local-only `kiox -- orun plan --changed --intent examples/intent.yaml` failure
  on the composition cache (`stack.yaml has no spec.compositions`). Reproduces
  on `main` HEAD `4ea1980` and on the prior `d2ab48e`. CI passes the same
  invocation. Not a regression; revisit only if it surfaces during M1+
  verification.

## Secondary Specs (not driving new tasks this phase)
- `.kiro/specs/orun-tui-cockpit/` — paused. Resumes after M5 lands.
- `.kiro/specs/github-artifacts/` — cross-check only; new revision/execution
  keys must remain compatible with the existing
  `gh-{run_id}-{attempt}-{sha}` ExecID shape produced by `internal/runbundle`.
