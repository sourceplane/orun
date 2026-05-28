# Implementation Plan: orun-tui-cockpit

## Overview

Implement the `orun tui` cockpit in four MVP phases. The Charm stack (Bubble Tea, Bubbles, Lip Gloss) is added as pinned direct dependencies. All TUI logic lives under `internal/tui/`; the service layer calls Orun internal packages directly and never shells out. Property-based tests cover the root model and Plan Studio state machines using `github.com/flyingmutant/rapid`.

## Tasks

---

### Phase 1 — Foundation and Read-Only Cockpit

- [ ] 1. Add Charm stack dependencies to go.mod
  - Add `github.com/charmbracelet/bubbletea v1.3.5`, `github.com/charmbracelet/bubbles v0.21.0`, `github.com/charmbracelet/lipgloss v1.1.0`, and `github.com/flyingmutant/rapid v1.1.0` to `go.mod` as pinned direct dependencies
  - Run `go mod tidy` to populate `go.sum` and pull in transitive Charm dependencies (`charmbracelet/x/ansi`, `charmbracelet/x/term`, `muesli/termenv`, `lucasb-eyer/go-colorful`)
  - Verify `go build ./...` still passes after the additions
  - _Requirements: 13.1, 13.2_

- [ ] 2. Scaffold `internal/tui/` package structure
  - [ ] 2.1 Create stub files for the top-level package: `internal/tui/app.go`, `internal/tui/model.go`, `internal/tui/keymap.go`, `internal/tui/theme.go`
    - `app.go`: `NewProgram(svc OrunService) *tea.Program` — constructs `tea.Program` with `tea.WithAltScreen()` and `tea.WithMouseCellMotion()`
    - `model.go`: `Model` struct with `width`, `height`, `activeMode Mode`, `activePanel Panel`, panel model fields, overlay fields, `svc services.OrunService`, `workspace *services.WorkspaceSnapshot`, `lastErr error`; stub `Init`, `Update`, `View`
    - `keymap.go`: `GlobalKeyMap` struct using `github.com/charmbracelet/bubbles/key`; per-mode key map stubs; `help.KeyMap` implementation
    - `theme.go`: all `lipgloss.Style` constants (panel borders, widths, colours, error banner style); no inline style literals in view files
    - _Requirements: 2.1, 2.2, 2.3, 9.1_
  - [ ] 2.2 Create stub files for `internal/tui/views/`: `browse.go`, `navigator.go`, `inspector.go`, `plan_studio.go`, `run_view.go`, `log_explorer.go`, `history.go`, `command_palette.go`
    - Each file: struct definition + stub `Init`, `Update`, `View` satisfying `tea.Model`
    - _Requirements: 2.1, 3.1, 4.1, 6.1, 7.1, 8.1, 9.3_
  - [ ] 2.3 Create stub files for `internal/tui/events/`: `run_events.go`, `log_tail.go`
    - `run_events.go`: `WaitForRunEvent(ch <-chan services.RunEvent) tea.Cmd`
    - `log_tail.go`: `WaitForLogEvent(ch <-chan services.LogEvent) tea.Cmd`
    - _Requirements: 6.4, 7.7, 13.5, 13.6_

- [ ] 3. Define `OrunService` interface and all data types
  - [ ] 3.1 Write `internal/tui/services/orun_service.go`
    - Define `OrunService` interface with all six methods: `LoadWorkspace`, `GeneratePlan`, `RunPlan`, `ListRuns`, `Describe`, `TailLogs`
    - Define all request types: `WorkspaceRequest`, `PlanRequest`, `RunRequest`, `ListRunsRequest`, `LogRequest`, `ResourceRef`
    - Define all response/data types: `WorkspaceSnapshot`, `ComponentSummary`, `PlanSummary`, `PlanResult`, `RunEvent`, `RunEventKind` constants, `LogEvent`, `RunSummary`, `ResourceDescription`, `DescField`
    - Define all `tea.Msg` types: `WorkspaceLoadedMsg`, `PlanGeneratedMsg`, `RunEventMsg`, `LogEventMsg`, `RunsListedMsg`, `DescribeResultMsg`, `ErrMsg`, `TickMsg`
    - _Requirements: 13.1, 13.4, 13.5, 13.6_
  - [ ] 3.2 Write `internal/tui/services/mock_service.go` — `MockOrunService` struct satisfying `OrunService`
    - Configurable return values for each method via function fields (e.g. `LoadWorkspaceFn func(ctx, req) (*WorkspaceSnapshot, error)`)
    - Default implementations return zero values with no error
    - _Requirements: 13.3_

- [ ] 4. Implement `LiveOrunService` skeleton and `WorkspaceService`
  - [ ] 4.1 Write `internal/tui/services/live_service.go` — `LiveOrunService` struct and constructor
    - `LiveServiceConfig` struct: `IntentFile`, `IntentRoot`, `ConfigDir string`, `Store *state.Store`, `Backend statebackend.Backend`, `Version string`
    - `NewLiveOrunService(cfg LiveServiceConfig) *LiveOrunService`
    - Stub all six `OrunService` methods returning `errors.New("not implemented")`
    - _Requirements: 13.2_
  - [ ] 4.2 Implement `LoadWorkspace` in `live_service.go`
    - Call `internal/discovery.FindIntentFile` when `req.IntentFile` is empty
    - Call `internal/loader` to load the intent and component tree
    - Call `internal/normalize` to normalise component summaries
    - Populate and return `*WorkspaceSnapshot`; respect `ctx` cancellation
    - _Requirements: 1.2, 1.3, 1.6, 13.2, 13.4_

- [ ] 5. Implement `HistoryService` — `ListRuns`
  - Write `internal/tui/services/history_service.go`
  - Implement `ListRuns` on `LiveOrunService`: call `state.Store.ListExecutions` and `LoadMetadata` for each execution; map to `[]RunSummary`
  - When `req.RemoteState == true`, delegate to `backend.ListExecutions` instead
  - Respect `req.Limit` and `ctx` cancellation
  - _Requirements: 8.1, 11.5, 13.2, 13.4, 14.10_

- [ ] 6. Implement `LogService` — `TailLogs`
  - Write `internal/tui/services/log_service.go`
  - Implement `TailLogs` on `LiveOrunService`: read `.orun/executions/{execID}/logs/{jobID}/` log files line by line; emit `LogEvent` values on the returned channel
  - When `req.Follow == true`, tail the file until `ctx` is cancelled; when `req.Follow == false`, close the channel after the last line
  - When `req.RemoteState == true`, fetch logs from the remote backend
  - _Requirements: 7.1, 7.7, 7.8, 13.4, 13.6, 14.9_

- [ ] 7. Implement `NavigatorModel`
  - Implement `internal/tui/views/navigator.go` fully
  - Render a fixed-width (~20 cols) resource-type tree: Components, Environments, Plans, Runs, Jobs, Logs, History
  - Arrow keys move cursor; `enter` emits a `NavSelectedMsg` consumed by the root model to switch mode
  - Dimmed style when `focused == false`
  - _Requirements: 1.3, 2.1, 2.6_

- [ ] 8. Implement `BrowseModel`
  - Implement `internal/tui/views/browse.go` fully
  - Use `bubbles/list` to render the component table with columns: name, type, env, profile, path, changed, dep count, last run status
  - Implement `/` fuzzy search, `e` env filter, `t` type filter, `c` changed-only toggle — each filter hides non-matching rows in real time
  - `d` key: render dependency tree for selected component in Main
  - `enter`: emit `DescribeRequestMsg` for selected component
  - `p`: emit `SwitchToPlanStudioMsg` with selected component pre-filled
  - `l`: emit `SwitchToLogExplorerMsg` for most recent run of selected component
  - Render failed-status rows with the failure indicator colour from `theme.go`
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10_

- [ ] 9. Implement `InspectorModel`
  - Implement `internal/tui/views/inspector.go` fully
  - Use `bubbles/viewport` to render `ResourceDescription` fields: kind, name, summary, labelled fields, available actions
  - Empty state with placeholder when `desc == nil`
  - Arrow-key scrolling when Inspector panel has focus
  - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5_

- [ ] 10. Implement root `Model` — three-panel layout and global key handling
  - Implement `internal/tui/model.go` fully
  - `Init()`: return `loadWorkspaceCmd(m.svc)` — calls `LoadWorkspace` asynchronously
  - `Update()`: dispatch `WorkspaceLoadedMsg`, `ErrMsg`, `tea.WindowSizeMsg`, `tea.KeyMsg` (global bindings: `tab`, `shift+tab`, `?`, `:`, `/`, `q`, `ctrl+c`, `esc`, `ctrl+r`); delegate remaining messages to the focused panel's `Update`
  - `View()`: render error banner when `lastErr != nil`; render three-panel layout using Lip Gloss joins; render status bar (repo, plan checksum, run status) and key-hint bar
  - Panel focus cycles Navigator → Main → Inspector → Navigator on `tab`; reverse on `shift+tab`
  - Unfocused panels rendered in dimmed style; only focused panel receives mode-specific key input
  - `tea.WindowSizeMsg` propagated to all panels
  - _Requirements: 1.2, 1.3, 1.4, 1.5, 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 9.1, 12.1, 12.2, 12.3, 12.6_

- [ ] 11. Register `cmd/orun/command_tui.go` Cobra command
  - Create `cmd/orun/command_tui.go` with `tuiCmd`, `registerTuiCommand`, and `runTUI` following the pattern in `design.md §8`
  - `--remote-state` flag: if set without a resolvable backend URL, return error before launching Bubble Tea
  - `--backend-url` flag (falls back to `ORUN_BACKEND_URL` env var)
  - Call `registerTuiCommand(rootCmd)` in `commands_root.go init()`
  - _Requirements: 1.1, 1.7, 1.8, 11.1, 11.2_

- [ ] 12. Checkpoint — Phase 1 builds and smoke-tests pass
  - Ensure `go build ./...` succeeds
  - Ensure `go test ./internal/tui/... -count=1` passes (stubs + mock service)
  - Ask the user if questions arise before proceeding to Phase 2.

- [ ] 13. Add `MockOrunService` property-based tests for root `Model` state machine
  - [ ] 13.1 Write `internal/tui/model_pbt_test.go`
    - Import `github.com/flyingmutant/rapid`
    - **Property 1: Mode validity** — after any sequence of key messages, `ActiveMode` is in `[ModeBrowse, ModeHistory]`
    - **Validates: Requirements 9.10, 14.1**
    - _Requirements: 9.10, 14.1_
  - [ ]* 13.2 Write property test for panel cycle (Property 2)
    - **Property 2: Panel cycle** — after `n` tab presses from initial state, `ActivePanel == n % 3`
    - **Validates: Requirements 2.4, 14.2**
    - _Requirements: 2.4, 14.2_
  - [ ]* 13.3 Write property test for overlay exclusivity (Property 3)
    - **Property 3: Overlay exclusivity** — at most one overlay (command palette, help, confirmation) is visible at any time
    - **Validates: Requirements 9.7, 14.3**
    - _Requirements: 9.7, 14.3_
  - [ ]* 13.4 Write property test for workspace immutability (Property 7)
    - **Property 7: Workspace immutability** — `WorkspaceSnapshot` pointer is never replaced by view-level key messages; only `WorkspaceLoadedMsg` replaces it
    - **Validates: Requirements 14.7**
    - _Requirements: 14.7_
  - [ ]* 13.5 Write property test for esc idempotence (Property 6)
    - **Property 6: esc idempotence** — pressing `esc` at `PhaseFormFill` does not change the active mode or produce an error
    - **Validates: Requirements 9.9, 14.6**
    - _Requirements: 9.9, 14.6_

---

### Phase 2 — Plan Studio

- [ ] 14. Implement `PlanService` — `GeneratePlan`
  - Write `internal/tui/services/plan_service.go`
  - Implement `GeneratePlan` on `LiveOrunService`: call `internal/planner.Generate` with the resolved intent and scope from `PlanRequest`; map result to `*PlanResult`
  - Respect `ctx` cancellation; wrap planner errors in `ErrMsg`-compatible error values
  - _Requirements: 4.2, 4.3, 4.4, 13.2, 13.4_

- [ ] 15. Implement `PlanStudioModel` with phase state machine
  - [ ] 15.1 Implement `internal/tui/views/plan_studio.go` fully
    - `ScopeForm`: `bubbles/textinput` fields for scope, environment, trigger, base/head refs, dry-run toggle
    - `DAGRenderer`: ASCII DAG from `PlanResult.Plan` jobs and `DependsOn` edges; show per-component profiles, gate names, destructive capabilities, composition source digests, plan checksum
    - Phase state machine: `PhaseFormFill → PhaseGenerating → PhaseReview → PhaseConfirming → PhaseRunning`
    - `g` in `PhaseFormFill`: call `GeneratePlan` asynchronously; show spinner in `PhaseGenerating`
    - `PlanGeneratedMsg{Err != nil}`: transition to `PhaseFormFill` with error banner
    - `PlanGeneratedMsg{Result, nil}`: transition to `PhaseReview`
    - `d` in `PhaseReview`: call `RunPlan` with `DryRun: true`; transition to Run Dashboard
    - `r` in `PhaseReview`: transition to `PhaseConfirming`; populate `ConfirmationPane`
    - `s` in `PhaseReview`: prompt for plan name; save to state store
    - `enter` on job in `PhaseReview`: emit `DescribeRequestMsg` for that job
    - `esc` at any phase: return to `PhaseFormFill`
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9_
  - [ ] 15.2 Implement `ConfirmationPane` in `internal/tui/model.go`
    - Render modal overlay with: scope, environments, affected components, job count, plan checksum, destructive capabilities, gate names, runner backend, concurrency, dry-run flag
    - `enter`: call `RunPlan`; transition to Run Dashboard
    - `esc`: return to `PhaseFormFill`; clear pane
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

- [ ] 16. Add property-based tests for `PlanStudioModel` phase transitions
  - [ ] 16.1 Write `internal/tui/views/plan_studio_pbt_test.go`
    - **Property 4: Confirmation gate** — `PhaseRunning` is only reachable immediately after `PhaseConfirming` with an `enter` key message
    - **Validates: Requirements 5.1, 14.4**
    - _Requirements: 5.1, 14.4_
  - [ ]* 16.2 Write property test for error recovery (Property 5)
    - **Property 5: Error recovery** — `PlanGeneratedMsg` with a non-nil error always transitions to `PhaseFormFill`
    - **Validates: Requirements 4.4, 12.4, 14.5**
    - _Requirements: 4.4, 12.4, 14.5_
  - [ ]* 16.3 Write unit tests for `PlanStudioModel` phase transitions
    - Test each valid phase transition with concrete messages
    - Test `esc` at each phase returns to `PhaseFormFill`
    - Test `ConfirmationPane` populated correctly from `PlanResult`
    - _Requirements: 4.1–4.9, 5.1–5.5_

- [ ] 17. Checkpoint — Phase 2 tests pass
  - Ensure `go test ./internal/tui/... -count=1` passes including PBT tests
  - Ask the user if questions arise before proceeding to Phase 3.

---

### Phase 3 — Execution Dashboard, Log Explorer, History

- [ ] 18. Implement `RunService` — `RunPlan` with streaming channel bridge
  - Write `internal/tui/services/run_service.go`
  - Implement `RunPlan` on `LiveOrunService`: construct `runner.NewRunner`; attach `runner.RunnerHooks` to emit `RunEvent` values (`JobStarted`, `JobCompleted`, `JobFailed`, `StepStarted`, `StepCompleted`) on a buffered channel (size 64); run in a goroutine; close channel and emit `RunEventRunDone` when done
  - Respect `req.DryRun`, `req.JobID` (single-job retry), `req.Concurrency`
  - Respect `ctx` cancellation: cancel the runner context when `ctx` is done
  - _Requirements: 6.1, 6.7, 13.2, 13.4, 13.5_

- [ ] 19. Implement `events/run_events.go` and `events/log_tail.go` channel bridges
  - Implement `WaitForRunEvent(ch <-chan services.RunEvent) tea.Cmd` — blocks on channel read; returns `RunEventMsg`; returns `RunEventMsg{RunEventRunDone}` when channel closes
  - Implement `WaitForLogEvent(ch <-chan services.LogEvent) tea.Cmd` — blocks on channel read; returns `LogEventMsg`; returns sentinel `LogEventMsg` when channel closes
  - _Requirements: 6.4, 7.7, 13.5, 13.6, 14.8, 14.9_

- [ ] 20. Implement `RunViewModel` — live job timeline
  - Implement `internal/tui/views/run_view.go` fully
  - Accumulate `RunEvent` values in `m.events`; derive `[]JobRow` (status icon, step progress, elapsed duration, component grouping) via `rebuildJobRows`
  - On `RunEventMsg`: update job row; re-arm `WaitForRunEvent`; on `RunEventJobFailed` mark row with failure indicator and emit `DescribeRequestMsg` with error text
  - On `RunEventRunDone`: set `phase = RunPhaseDone`; stop issuing `WaitForRunEvent`
  - On `TickMsg`: refresh elapsed durations for running jobs
  - `enter` on job: emit `DescribeRequestMsg`
  - `l` on job: emit `SwitchToLogExplorerMsg`
  - `r` on failed job: call `RunPlan` with specific `JobID`
  - `s`: display step timeline for selected job
  - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7, 6.8, 6.9_

- [ ] 21. Implement `LogExplorerModel` — structured log streaming
  - Implement `internal/tui/views/log_explorer.go` fully
  - Left: `bubbles/list` job/step tree (rendered in Navigator panel when Log Explorer is active)
  - Main: `bubbles/viewport` scrollable log stream; append lines on `LogEventMsg`; re-arm `WaitForLogEvent`
  - `f`: toggle follow mode; auto-scroll viewport to latest line while active
  - `e`: toggle errors-only filter (`LogEvent.IsError == true`)
  - `j`: scroll viewport to first error line
  - `c`: copy failed command for selected step to system clipboard
  - On channel close with error: append error line to viewport; disable follow mode
  - On channel close without error (`Follow == false`): stop issuing `WaitForLogEvent`
  - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8_

- [ ] 22. Implement `HistoryModel` — run list and replay
  - Implement `internal/tui/views/history.go` fully
  - Use `bubbles/list` to render run rows: exec-id, status, plan checksum, total/done/failed jobs, duration, trigger, git ref, dry-run flag
  - On init: call `ListRuns` asynchronously; populate list on `RunsListedMsg`
  - `enter`: emit `DescribeRequestMsg` for selected run
  - `l`: emit `SwitchToLogExplorerMsg` for selected run
  - `r`: call `RunPlan` with stored plan; transition to Run Dashboard
  - `d`: emit `ShowPlanDiffMsg` for selected run vs previous run
  - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

- [ ] 23. Implement `CommandPaletteModel`
  - Implement `internal/tui/views/command_palette.go` fully
  - `bubbles/textinput` for command input; suggestion list filtered to match typed prefix
  - Support all 13 commands from `design.md §7`: `:plan changed`, `:plan component <name>`, `:plan env <env>`, `:run latest`, `:run latest dry-run`, `:logs failed`, `:logs <exec-id>`, `:describe component <name>`, `:describe job <job-id>`, `:filter type <type>`, `:filter env <env>`, `:history`, `:remote-state`
  - `enter` on selected suggestion: dispatch corresponding action to root model
  - `esc`: close palette without changing mode
  - _Requirements: 9.3, 9.4, 9.5, 9.6, 9.7, 9.8_

- [ ] 24. Wire remote state backend — `--remote-state` flag integration
  - In `runTUI()` (`cmd/orun/command_tui.go`): when `--remote-state` is set, call `newRemoteBackend(backendURL)` to construct `RemoteStateBackend`; pass to `LiveServiceConfig.Backend`
  - In `RunViewModel.Update`: on `TickMsg` while run is in progress and `RemoteState == true`, poll `backend.LoadRunState` to refresh job statuses
  - In `HistoryModel`: when `RemoteState == true`, `ListRuns` uses `backend.ListExecutions`; merge with local runs using same `RunSummary` shape
  - _Requirements: 1.7, 1.8, 11.1, 11.2, 11.3, 11.4, 11.5_

- [ ] 25. Checkpoint — Phase 3 tests pass
  - Ensure `go test ./internal/tui/... -count=1` passes
  - Ensure `go build ./cmd/orun/...` succeeds
  - Ask the user if questions arise before proceeding to Phase 4.

---

### Phase 4 — Advanced Features

- [ ] 26. Implement plan diff view
  - Add `ShowPlanDiffMsg` handling in root `Model.Update`
  - Implement diff rendering in `internal/tui/views/history.go` (or a dedicated `plan_diff.go`): show added jobs, removed jobs, changed jobs, added/removed gates, changes to composition source digests between two `PlanResult` values
  - Wire `d` key in `HistoryModel` to emit `ShowPlanDiffMsg` with current and previous run's plan checksums
  - _Requirements: 8.5, 15.1_

- [ ] 27. Implement failure workbench
  - Add failure workbench overlay/panel to `internal/tui/views/run_view.go` or a dedicated `failure_workbench.go`
  - When a job has failed, display: failed step, exit code, likely issue (parsed from log), available actions (retry, copy command, open logs)
  - Render whatever failure details are available; do not suppress the workbench if some details are missing
  - Wire to `RunEventJobFailed` handling in `RunViewModel`
  - _Requirements: 15.2_

- [ ] 28. Implement explain mode
  - Add `ExplainMsg` and explain overlay to `internal/tui/model.go`
  - When activated on a component or job node, display: why the node is in the plan, which profile was selected and why, which environment it targets, which dependency is blocking it (if any), which composition source produced it
  - Populate from `ResourceDescription` fields returned by `OrunService.Describe`
  - _Requirements: 15.3_

- [ ] 29. Final checkpoint — all tests pass
  - Ensure `go test ./internal/tui/... -count=1` passes including all PBT tests
  - Ensure `go build ./cmd/orun/...` succeeds
  - Ask the user if questions arise.

---

## Notes

- Tasks marked with `*` are optional and can be skipped for a faster MVP; they are property-based or unit tests that validate correctness properties from the design.
- Each task references specific requirements for traceability.
- Checkpoints (tasks 12, 17, 25, 29) ensure incremental validation between phases.
- Property tests use `github.com/flyingmutant/rapid` and live in `model_pbt_test.go` and `plan_studio_pbt_test.go`; run with `go test ./internal/tui/... -count=1`.
- The `LiveOrunService` must never use `exec.Command` to invoke `orun` subprocesses — all calls go through internal packages directly.
- All `OrunService` methods accept `context.Context` and must respect cancellation; the root model passes a cancellable context derived from the program lifecycle.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1"] },
    { "id": 1, "tasks": ["2.1", "2.2", "2.3"] },
    { "id": 2, "tasks": ["3.1", "3.2"] },
    { "id": 3, "tasks": ["4.1"] },
    { "id": 4, "tasks": ["4.2", "5", "6"] },
    { "id": 5, "tasks": ["7", "8", "9"] },
    { "id": 6, "tasks": ["10", "11"] },
    { "id": 7, "tasks": ["13.1", "13.2", "13.3", "13.4", "13.5"] },
    { "id": 8, "tasks": ["14"] },
    { "id": 9, "tasks": ["15.1", "15.2"] },
    { "id": 10, "tasks": ["16.1", "16.2", "16.3"] },
    { "id": 11, "tasks": ["18"] },
    { "id": 12, "tasks": ["19"] },
    { "id": 13, "tasks": ["20", "21", "22", "23"] },
    { "id": 14, "tasks": ["24"] },
    { "id": 15, "tasks": ["26", "27", "28"] }
  ]
}
```
