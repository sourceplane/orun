# Current Orchestration Context

Last updated: 2026-05-29 (Task 0147.1 verifier PASS, PR #146 merged)

## Repo Reality

- Local branch `main` at merge commit `8b6f609` (`feat(tui): wire Plan Studio dry-run via LiveOrunService.RunPlan (Task 0147) (#146)`), fast-forwarded from `origin/main`.
- Phase 3 TUI Cockpit slice 1 is durable on `main`: `LiveOrunService.RunPlan` (local dry-run, fail-closed), Plan Studio `d` dispatch, Run Dashboard streaming event timeline.
- Open PRs: none for Task 0147. PR #146 merged at `8b6f609` on 2026-05-29.
- Repo health: **green**. All local checks pass (`go test ./internal/tui/...`, `./internal/runner/...`, `./cmd/orun/...`, `go build ./cmd/orun/...`, `kiox -- orun validate/plan/dry-run`).

## Last Completed Task (0147.1)

Task 0147.1 verified PASS and merged PR #146 at merge commit `8b6f609` (2026-05-29). Verifier report: `ai/reports/task-0147-verifier.md`. Implementer report: `ai/reports/task-0147-implementer.md` (verifier-authored from PR evidence — implementer omitted the report from the PR push; reconstruction committed at `1abb9cb` on the PR branch before merge).

Durable outcomes now on `main`:

- `internal/tui/services/run_service.go`: `LiveOrunService.RunPlan(ctx, RunRequest) (<-chan RunEvent, error)` constructs `internal/runner.Runner` directly (no `exec.Command`). `validateRunRequest` fails closed for nil `Plan`, `DryRun:false`, `RemoteState:true`. Sends are `ctx.Done()`-guarded; final `RunEventRunDone` uses a non-blocking `select` with `default` so runner hooks cannot deadlock. Channel buffered (64). Runner stdout/stderr discarded — TUI surfaces progress only via `RunEvent`.
- `internal/tui/services/live_service.go`: removed the `errNotImplemented` stub for `RunPlan`.
- `internal/tui/views/plan_studio.go`: `d` key from Review with a generated plan emits `PlanStudioDryRunRequestedMsg{Plan}`. Outside Review or with nil plan, `d` is a no-op.
- `internal/tui/model.go`: routes `PlanStudioDryRunRequestedMsg` → `svc.RunPlan(ctx, RunRequest{Plan, DryRun:true})`, installs the channel into `RunViewModel` via `StartStream`, and switches to `ModeRunDashboard` **only** on success. Error path keeps the active mode and stores `lastErr`. Routes `services.RunEventMsg` to the run view.
- `internal/tui/views/run_view.go`: real `RunViewModel` with per-job `rows` map, env grouping in `View()`, status icons, error truncation, `Done()` no-rearm semantics after `RunEventRunDone`.
- Tests: `run_service_test.go` (validate-only path + happy-path stream), `run_view_test.go` (event accumulation + no-rearm), `plan_studio_test.go` (d-key dispatch + no-op guards), `model_test.go` (root dispatch happy/error paths).

Safety confirmed: zero `exec.Command`/`os/exec`/`"orun"` subprocess usage anywhere under `internal/tui/`.

## Current Task

None. Orchestrator should scope the next Phase 3 slice.

## Current Roadmap Position

1. ✅ GitHub Artifacts Level 1/2 CLI gaps through Task 0141.1.
2. ✅ Orun Cockpit TUI Phase 1 foundation through Task 0144.1.
3. ✅ Task 0145 / 0145.1: dirty PR #142 resolved, GitHub CLI UX cleanup merged via PR #144.
4. ✅ Task 0146 / 0146.1: TUI Cockpit Phase 2 Plan Studio + real `LiveOrunService.GeneratePlan` merged via PR #145.
5. ✅ Task 0147 / 0147.1: TUI Cockpit Phase 3 slice 1 — local dry-run `RunPlan` + Plan Studio `d` + Run Dashboard timeline — merged via PR #146.
6. ⏭️ Next implementer task: next coherent Phase 3 slice. Strongest candidates:
   - `OrunService.Describe` + Inspector resource detail wiring (Run Dashboard `enter` and Plan Studio job inspection both depend on this becoming real).
   - Log Explorer / follow-mode `TailLogs` if log navigation is the more urgent dependency for the dry-run dashboard.
7. ⏭️ Later Phase 3 follow-ups: full Log Explorer/follow-mode, History/Replay, remote-state polling/integration, true cancellation via `Run(ctx)`.
8. ⏭️ Optional later UX polish: `docs/github-log-pull-ux-review.md` section 3 follow-ups (short-SHA support, `--job` logical-id matching, `--latest` branch disclosure).

## Next Task

Orchestrator should evaluate merged Phase 3 state and pick the next implementer slice — most likely `Describe` + Inspector wiring, unless the user surfaces log navigation as the more urgent gap.

## Open Risks

- **Recurring missing implementer report**: PR #146 again lacked `ai/reports/task-0147-implementer.md` on push. Verifier authored from PR evidence per task allowance. Recommend tightening the implementer skill so the report is always part of the PR push.
- **No `Run(ctx)` plumbing in the runner**: TUI cannot cancel a dry-run mid-step; cancellation only observed at the event boundary. Acceptable for dry-run (fast), must be revisited before real execution lands.
- **`AfterJobTerminal` skip semantics**: if the runner introduces a third terminal state ("skipped"), it will currently render as `failed` in the dashboard. Out of scope for Task 0147; tighten when runner adds the state.
- **Stale spec import path**: `.kiro/specs/orun-tui-cockpit/tasks.md` still mentions `github.com/flyingmutant/rapid`; repo reality uses `pgregory.net/rapid`. Non-blocking. PR #146 did not reintroduce the stale path.
- **Phase 3 real execution / remote-state integration remain deferred** — intentional to keep destructive paths gated until the dry-run dashboard seam is durable.
