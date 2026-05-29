# Current Orchestration Context

Last updated: 2026-05-29 (Task 0146.1 verified PASS, PR #145 merged)

## Repo Reality

- Local `main` synced with `origin/main` at `5beb334` (`feat(tui): plan studio with real GeneratePlan (#145)`). Working tree clean after orchestration cleanup commit.
- Completed checkpoint: Task 0146.1 verified PASS and merged PR #145. TUI Cockpit Phase 2 (Plan Studio + real `LiveOrunService.GeneratePlan`) is now durable on `main`.
- Open PRs: none.
- Repo health: **green**. TUI Cockpit Phase 3 (`RunPlan`/`Describe`/`TailLogs`) is now unblocked.

## Last Completed Task (0146.1)

Task 0146.1 verified PASS and merged PR #145 at merge commit `5beb334` (2026-05-29). Verifier report: `ai/reports/task-0146-verifier.md`. Implementer report: `ai/reports/task-0146-implementer.md`.

Durable outcomes now on `main`:

- `internal/tui/services/plan_service.go`: `LiveOrunService.GeneratePlan(ctx, PlanRequest) (*PlanResult, error)` mirrors `cmd/orun/main.go:generatePlan` via internal packages only (`loader`/`composition`/`trigger`/`expand`/`planner`/`render`). Honours `ctx.Err()` at every stage boundary. No `exec.Command`, no `os/exec`, no `"orun"` literal under `internal/tui/`.
- `internal/tui/views/plan_studio.go`: rewritten as a state machine (`Idle → Configuring → Generating → Review → Saved → Error`) with cursor nav, local keymap (`g`=generate, `s`=save, `c`=clear, `j/k`=cursor), deterministic `View()`, and indirect service dispatch via `views.GeneratePlanCmd`.
- `internal/tui/model.go`: routes `services.PlanGeneratedMsg` and `views.PlanStudioSaveRequestedMsg`; mode-switches `p`/`b`/`h`; seeds `PlanRequest` from the workspace snapshot. Save reuses `GeneratePlan` with `NamedPlan` set → byte-identical persisted plan.
- `NamedPlan` nil-store path emits a clear warning instead of panicking. `Warnings` also surfaces the ChangedOnly safe-subset disclaimer.
- Tests: `plan_service_test.go` (4) covers cancellation, missing intent, malformed YAML, request/config precedence; `plan_studio_test.go` (10 + 2 `pgregory.net/rapid` property tests) covers state transitions, cursor clamping, save dispatch, deterministic rendering, and state-set invariants.
- `go.mod`: `bubbletea v1.3.5`, `bubbles v0.21.0`, `lipgloss v1.1.0`, `pgregory.net/rapid v1.1.0` promoted to direct deps. Stale `github.com/flyingmutant/rapid` NOT reintroduced (still present only in `.kiro/specs/orun-tui-cockpit/tasks.md` — accepted non-blocking follow-up).
- Phase 3 surfaces (`RunPlan`, `Describe`, `TailLogs(Follow=true)`, `RemoteState.ListRuns`) remain explicit stubs — intentionally out of scope.

## Prior Checkpoint (Task 0145.1)

Task 0145.1 verified PASS and merged PR #144 at merge commit `300a436` (2026-05-29T00:14:11Z). Verifier report: `ai/reports/task-0145-verifier.md`. Implementer report: `ai/reports/task-0145-implementer.md`.

Durable outcomes now on `main`:

- `cmd/orun/command_github.go`: `normalizeOrunDir()` helper centralizes `--orun-dir` resolution. Empty input → `./.orun`; parent input → `<parent>/.orun`; already-`.orun` input unchanged. `runGithubPull` calls the helper once instead of bifurcated logic.
- `orun github status` registers the same six selector flags as `pull`/`logs`: `--run-id`, `--exec-id`, `--sha`, `--branch`, `--latest`, `--failed`. Status, logs, and pull now share a uniform vocabulary.
- `cmd/orun/command_github_test.go`: five new focused tests cover the three normalization branches plus selector flag registration and parse-time acceptance.
- `website/docs/cli/orun-github.md`: public docs match the final CLI behavior, including the full-SHA caveat and `--job` substring-match caveat.
- `docs/github-log-pull-ux-review.md`: short UX-review note with reproducer commands, code-level root cause for the two fixed bugs, and a prioritized list of three open friction items (short-SHA support, `--job` logical-id matching, `--latest` branch disclosure).
- PR #142 (`happy-patch-113`) is CLOSED as superseded. No remaining open-dirty PRs.

## Current Task

None scoped. Repo is green; orchestrator is ready to scope the next cycle (TUI Cockpit Phase 3).

## Current Roadmap Position

1. ✅ GitHub Artifacts Level 1/2 CLI gaps through Task 0141.1.
2. ✅ Orun Cockpit TUI Phase 1 foundation through Task 0144.1.
3. ✅ Task 0145 / 0145.1: dirty PR #142 resolved, GitHub CLI UX cleanup merged via PR #144.
4. ✅ Task 0146 / 0146.1: TUI Cockpit Phase 2 Plan Studio + real `LiveOrunService.GeneratePlan` merged via PR #145.
5. ⏭️ Task 0147: TUI Cockpit Phase 3 — wire `RunPlan` (dry-run first), live Run Dashboard skeleton, `Describe`, follow-mode `TailLogs`. Currently `errNotImplemented`.
6. ⏭️ Optional: pick up `docs/github-log-pull-ux-review.md` section 3 follow-ups (short-SHA support, `--job` logical-id matching, `--latest` branch disclosure) as a small focused CLI UX task in parallel.

## Next Task

Task 0147 (Implementer): scope TUI Cockpit Phase 3 — start with `LiveOrunService.RunPlan` against `internal/runner` in dry-run mode, plus a Run Dashboard view (`r`) that subscribes to run events via `services.RunEventMsg`. Defer real apply/destroy execution and remote-state log follow until 0148+.

## Open Risks

- None blocking. Spec drift in `.kiro/specs/orun-tui-cockpit/tasks.md` (stale `flyingmutant/rapid` reference) remains non-blocking; address opportunistically in the next spec edit. Open follow-ups in `docs/github-log-pull-ux-review.md` section 3 are non-blocking UX polish.
