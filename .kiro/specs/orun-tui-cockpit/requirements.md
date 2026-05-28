# Requirements Document

## Introduction

`orun tui` is a component-native TUI control plane for Orun — a full-window interactive cockpit that makes the intent → component → composition → profile → plan DAG → execution record model visible, navigable, and actionable from the terminal. It is built on the Charm stack (Bubble Tea, Bubbles, Lip Gloss) and calls Orun internal packages directly rather than shelling out to subcommands.

The cockpit ships in four MVP phases:
- **Phase 1** — read-only browse (workspace discovery, component/environment/plan/run listing, describe, logs)
- **Phase 2** — Plan Studio (plan generation, DAG review, dry-run, named plan save)
- **Phase 3** — Execution Dashboard, Log Explorer, History/Replay (live run, log streaming, history)
- **Phase 4** — advanced features (plan diff, failure workbench, explain mode, remote cockpit)

*Design reference: `design.md` §Overview, §Architecture §1–10, §Correctness Properties*

---

## Glossary

- **TUI**: The `orun tui` terminal user interface application.
- **Cockpit**: The full-window interactive TUI launched by `orun tui`.
- **Navigator**: The left panel (~20 cols) showing the resource-type tree.
- **Main**: The flexible centre panel showing mode-specific primary content.
- **Inspector**: The right panel (~28 cols) showing detail for the item focused in Main.
- **OrunService**: The Go interface that is the single boundary between TUI views and Orun internal packages.
- **WorkspaceSnapshot**: The immutable read-only snapshot of the current intent root, components, environments, and saved plans returned by `OrunService.LoadWorkspace`.
- **PlanResult**: The output of `OrunService.GeneratePlan`, containing the compiled plan, checksum, job count, component list, and warnings.
- **RunEvent**: A single streaming event from an executing plan (job started/completed/failed, step started/completed, run done).
- **LogEvent**: A single log line from a job/step log tail.
- **RunSummary**: One row in the History view representing a completed or in-progress execution.
- **ResourceDescription**: Structured detail for any resource ref (component, job, plan, run) returned by `OrunService.Describe`.
- **ConfirmationPane**: The modal overlay shown before plan execution that requires explicit `enter` confirmation.
- **Mode**: The active primary view — one of Browse, Plan Studio, Run Dashboard, Log Explorer, History.
- **Panel**: The focused panel — one of Navigator, Main, Inspector.
- **Phase**: The sub-state within Plan Studio — one of FormFill, Generating, Review, Confirming, Running.
- **ErrMsg**: The Bubble Tea message type used to surface all async errors into the event loop.
- **FileStateBackend**: The local file-based state backend (`statebackend.FileStateBackend`).
- **RemoteStateBackend**: The remote HTTP state backend (`statebackend.RemoteStateBackend`).
- **DAG**: Directed acyclic graph of plan jobs and their dependency edges.
- **Intent root**: The directory containing `intent.yaml`, discovered from the current working directory.

---

## Requirements

### Requirement 1: TUI Entry Point and Workspace Discovery

**User Story:** As a developer, I want to launch `orun tui` from any directory in my Orun workspace, so that I can immediately see the components, environments, and plans relevant to my current context.

*Design reference: `design.md` §Architecture §1 (System Architecture), §8 (Cobra Registration), §Components §1 (Package Structure)*

#### Acceptance Criteria

1. WHEN the user runs `orun tui`, THE TUI SHALL register as a Cobra subcommand and launch the Bubble Tea program with alt-screen and mouse cell motion enabled.
2. WHEN the TUI starts, THE TUI SHALL call `OrunService.LoadWorkspace` asynchronously and display a loading indicator until the `WorkspaceLoadedMsg` is received.
3. WHEN `LoadWorkspace` succeeds, THE TUI SHALL populate the Navigator with the resource-type tree (Components, Environments, Plans, Runs, Jobs, Logs, History) and enter Browse mode; any existing error banners SHALL remain displayed until dismissed by a key press.
4. WHEN `LoadWorkspace` fails, THE TUI SHALL display an error banner describing the failure and render an empty Navigator state.
5. WHEN the user presses `ctrl+r`, THE TUI SHALL re-invoke `LoadWorkspace` and refresh the workspace snapshot.
6. WHERE the current working directory is inside a component directory, THE TUI SHALL scope the initial Browse view to that component and its transitive dependencies.
7. THE TUI SHALL accept a `--remote-state` flag and a `--backend-url` flag (or `ORUN_BACKEND_URL` environment variable) on the `orun tui` command.
8. IF `--remote-state` is specified without a resolvable backend URL, THEN THE TUI SHALL completely abort and return an error to the shell without launching the Bubble Tea program.

---

### Requirement 2: Three-Panel Layout and Panel Navigation

**User Story:** As a developer, I want a persistent three-panel layout with Navigator, Main, and Inspector panels, so that I can browse resources, view primary content, and inspect details simultaneously.

*Design reference: `design.md` §Architecture §2 (Three-Panel Layout), §7 (Key Binding Map)*

#### Acceptance Criteria

1. THE TUI SHALL render a three-panel layout: Navigator (~20 cols, fixed), Main (flexible), and Inspector (~28 cols, fixed), separated by vertical borders.
2. THE TUI SHALL render a status bar at the top showing the repo name, current plan checksum, and run status.
3. THE TUI SHALL render a key-hint bar at the bottom showing the active mode's most common bindings.
4. WHEN the user presses `tab`, THE TUI SHALL advance panel focus in the cycle Navigator → Main → Inspector → Navigator.
5. WHEN the user presses `shift+tab`, THE TUI SHALL advance panel focus in the reverse cycle Inspector → Main → Navigator → Inspector.
6. WHILE a panel does not have focus, THE TUI SHALL render that individual panel in a dimmed style and ignore mode-specific key input directed at that panel; each unfocused panel is dimmed independently of the others.
7. WHEN the terminal is resized, THE TUI SHALL reflow all three panels to fit the new dimensions without corrupting layout.

---

### Requirement 3: Browse Mode — Component and Resource Listing

**User Story:** As a developer, I want to browse all components, environments, plans, and runs in my workspace, so that I can understand the current state of my Orun repository at a glance.

*Design reference: `design.md` §Architecture §3 (Five View Modes — Browse), §6 (TUI Data Models — ComponentSummary)*

#### Acceptance Criteria

1. WHEN Browse mode is active, THE TUI SHALL display a table of components in the Main panel with columns: name, type, environment, profile, path, changed status, dependency count, and last run status.
2. WHEN the user presses `/` in Browse mode, THE TUI SHALL open a fuzzy search filter that narrows the component list in real time; WHILE the search filter is active, THE TUI SHALL hide the full component table and show only the filtered results.
3. WHEN the user presses `e` in Browse mode, THE TUI SHALL filter the component list to show only components subscribed to the selected environment; WHILE the environment filter is active, THE TUI SHALL hide the full component table and show only the filtered results.
4. WHEN the user presses `t` in Browse mode, THE TUI SHALL filter the component list to show only components of the selected composition type; WHILE the type filter is active, THE TUI SHALL hide the full component table and show only the filtered results.
5. WHEN the user presses `c` in Browse mode, THE TUI SHALL toggle a changed-only filter; WHILE the changed-only filter is active, THE TUI SHALL hide the full component table and show only components where `ComponentSummary.Changed == true`.
6. WHEN the user presses `d` in Browse mode, THE TUI SHALL display the dependency tree for the selected component in the Main panel.
7. WHEN the user presses `enter` in Browse mode, THE TUI SHALL load a `ResourceDescription` for the selected component and display it in the Inspector panel.
8. WHEN the user presses `p` in Browse mode, THE TUI SHALL transition to Plan Studio mode with the selected component pre-filled as the plan scope.
9. WHEN the user presses `l` in Browse mode, THE TUI SHALL transition to Log Explorer mode for the most recent run of the selected component.
10. IF a component has `LastRunStatus == "failed"`, THEN THE TUI SHALL render that row with a distinct failure indicator colour.

---

### Requirement 4: Plan Studio — Plan Generation and DAG Review

**User Story:** As a developer, I want to compose and review a plan visually before executing it, so that I can understand exactly what will run, in what order, and with what profiles before committing to execution.

*Design reference: `design.md` §Architecture §3 (Five View Modes — Plan Studio), §6 (State Machine for Plan-First Safety Flow), §Components §4 (PlanStudioModel)*

#### Acceptance Criteria

1. WHEN Plan Studio mode is entered, THE TUI SHALL display a scope form with fields: scope (full / changed / component / env), environment, trigger, base/head refs, and mode (dry-run first).
2. WHEN the user fills the scope form and presses `g`, THE TUI SHALL call `OrunService.GeneratePlan` asynchronously and display a spinner while the plan is being generated (PhaseGenerating).
3. WHEN `GeneratePlan` returns a successful `PlanResult`, THE TUI SHALL transition to PhaseReview and display the job DAG with dependency edges, per-component profiles, gate names, destructive capabilities, composition source digests, and the plan checksum.
4. WHEN `GeneratePlan` returns an error, THE TUI SHALL display an error banner and return Plan Studio to `PhaseFormFill`; both the error banner display and the phase transition SHALL occur on any plan generation error.
5. WHEN the user presses `d` in PhaseReview, THE TUI SHALL initiate a dry-run by calling `OrunService.RunPlan` with `DryRun: true` and transition to Run Dashboard mode.
6. WHEN the user presses `r` in PhaseReview, THE TUI SHALL transition to PhaseConfirming and display the ConfirmationPane.
7. WHEN the user presses `s` in PhaseReview, THE TUI SHALL prompt for a plan name and save the plan to the state store.
8. WHEN the user presses `enter` on a job in PhaseReview, THE TUI SHALL load a `ResourceDescription` for that job and display it in the Inspector.
9. WHEN the user presses `esc` at any Plan Studio phase, THE TUI SHALL return to PhaseFormFill without executing any run.

---

### Requirement 5: Plan-First Safety — Confirmation Gate

**User Story:** As a developer, I want an explicit confirmation step before any plan is executed, so that I cannot accidentally trigger a destructive run with a single keypress.

*Design reference: `design.md` §Architecture §6 (State Machine for Plan-First Safety Flow), §7 (Confirmation Pane Data Structure), §Correctness Properties — Property 4*

#### Acceptance Criteria

1. THE TUI SHALL enforce that `PhaseRunning` in Plan Studio is only reachable immediately after `PhaseConfirming` with an `enter` key message; no other message sequence SHALL transition directly to `PhaseRunning`.
2. WHEN the ConfirmationPane is displayed, THE TUI SHALL show: scope, environments, affected components, job count, plan checksum, destructive capabilities, gate names, runner backend, concurrency, and dry-run flag.
3. WHEN the user presses `enter` in the ConfirmationPane, THE TUI SHALL call `OrunService.RunPlan`; WHEN `RunPlan` is successfully called, THE TUI SHALL transition to Run Dashboard mode.
4. WHEN the user presses `esc` in the ConfirmationPane, THE TUI SHALL return to PhaseFormFill and clear the ConfirmationPane without executing any run.
5. IF `RunPlan` returns an error at run start, THEN THE TUI SHALL display an error banner and return to Plan Studio; THE TUI SHALL NOT enter Run Dashboard mode unless `RunPlan` was successfully called.

---

### Requirement 6: Execution Dashboard — Live Run Monitoring

**User Story:** As a developer, I want to watch a running plan in real time, so that I can see which jobs are running, which have succeeded or failed, and how long each is taking.

*Design reference: `design.md` §Architecture §3 (Five View Modes — Execution Dashboard), §5 (Event Channel Patterns), §Components §4 (RunViewModel)*

#### Acceptance Criteria

1. WHEN Run Dashboard mode is active, THE TUI SHALL display a live job timeline in the Main panel, grouped by component, with a status icon (running / success / failed / pending), step progress indicator, and elapsed duration for each job.
2. WHEN a `RunEventMsg` is received, THE TUI SHALL update the corresponding job row's status and duration immediately; WHEN the `RunEventMsg` carries error information, THE TUI SHALL display the error text in the Inspector panel.
3. WHEN a `RunEventJobFailed` event is received, THE TUI SHALL mark the job row with a failure indicator; the failure indicator and error text in the Inspector are independent — the error text SHALL be displayed regardless of whether the failure indicator renders successfully.
4. WHEN a `RunEventRunDone` message is received, THE TUI SHALL stop issuing `WaitForRunEvent` commands and mark the run as complete.
5. WHEN the user presses `enter` on a job in Run Dashboard, THE TUI SHALL load a `ResourceDescription` for that job and display it in the Inspector.
6. WHEN the user presses `l` on a job in Run Dashboard, THE TUI SHALL transition to Log Explorer mode for that job.
7. WHEN the user presses `r` on a failed job in Run Dashboard, THE TUI SHALL retry that job by calling `OrunService.RunPlan` with the specific `JobID`.
8. WHEN the user presses `s` in Run Dashboard, THE TUI SHALL display the step timeline for the selected job in the Main panel.
9. WHILE a run is in progress, THE TUI SHALL refresh the job timeline on each `TickMsg` (1-second interval).

---

### Requirement 7: Log Explorer — Structured Log Streaming

**User Story:** As a developer, I want to explore logs for any job or step in a structured way, so that I can quickly find errors, follow live output, and copy failed commands without leaving the TUI.

*Design reference: `design.md` §Architecture §3 (Five View Modes — Log Explorer), §Components §4 (LogExplorerModel), §Correctness Properties — Property 9*

#### Acceptance Criteria

1. WHEN Log Explorer mode is active, THE TUI SHALL display a job/step tree in the Navigator panel and a scrollable log stream in the Main panel.
2. WHEN a `LogEventMsg` is received, THE TUI SHALL append the log line to the log viewport.
3. WHEN the user presses `f` in Log Explorer, THE TUI SHALL toggle follow mode; WHILE follow mode is active, THE TUI SHALL auto-scroll the viewport to the latest log line.
4. WHEN the user presses `e` in Log Explorer, THE TUI SHALL toggle errors-only mode, filtering the log stream to show only lines where `LogEvent.IsError == true`.
5. WHEN the user presses `j` in Log Explorer, THE TUI SHALL scroll the viewport to the first log line where `LogEvent.IsError == true`.
6. WHEN the user presses `c` in Log Explorer, THE TUI SHALL copy the failed command associated with the selected step to the system clipboard.
7. WHEN `LogRequest.Follow == false` and the log file is fully read, THE TUI SHALL close the log tail channel and stop issuing `WaitForLogEvent` commands without blocking.
8. IF a log tail channel closes with an error, THEN THE TUI SHALL display an error line in the log viewport and disable follow mode.

---

### Requirement 8: History and Run Replay

**User Story:** As a developer, I want to browse past runs and replay or diff them, so that I can understand what changed between runs and reproduce past execution states.

*Design reference: `design.md` §Architecture §3 (Five View Modes — History/Replay), §Components §4 (HistoryModel), §Data Models — RunSummary*

#### Acceptance Criteria

1. WHEN History mode is active, THE TUI SHALL display a list of runs in the Main panel with columns: exec-id, status, plan checksum, total jobs, completed jobs, failed jobs, duration, trigger, git ref, and dry-run flag.
2. WHEN the user presses `enter` on a run in History, THE TUI SHALL load a `ResourceDescription` for that run and display it in the Inspector.
3. WHEN the user presses `l` on a run in History, THE TUI SHALL transition to Log Explorer mode for that run.
4. WHEN the user presses `r` on a run in History, THE TUI SHALL replay that run by calling `OrunService.RunPlan` with the stored plan and transition to Run Dashboard mode.
5. WHEN the user presses `d` on a run in History, THE TUI SHALL display a diff of that run's plan against the immediately preceding run's plan.
6. WHEN `--remote-state` is active, THE TUI SHALL display remote runs in the History list alongside local runs, using the same `RunSummary` shape.

---

### Requirement 9: Keyboard Navigation and Command Palette

**User Story:** As a developer, I want consistent global key bindings and a command palette, so that I can navigate the TUI efficiently without memorising every mode-specific shortcut.

*Design reference: `design.md` §Architecture §7 (Key Binding Map and Command Palette), §Correctness Properties — Properties 1, 2, 3, 6*

#### Acceptance Criteria

1. THE TUI SHALL support the following global key bindings at all times: `tab`/`shift+tab` (cycle panel focus), `?` (toggle help overlay), `:` (open command palette), `/` (open fuzzy search), `q`/`ctrl+c` (quit), `esc` (close overlay / cancel / back), `ctrl+r` (reload workspace).
2. WHEN the user presses `?`, THE TUI SHALL toggle a help overlay listing all active key bindings for the current mode; pressing `?` again or `esc` SHALL dismiss the overlay.
3. WHEN the user presses `:`, THE TUI SHALL open the command palette with a text input and a suggestion list.
4. WHEN the user types in the command palette, THE TUI SHALL filter suggestions to match the typed prefix.
5. WHEN the user selects a command palette suggestion and presses `enter`, THE TUI SHALL execute the corresponding action.
6. THE TUI SHALL support the following command palette commands: `:plan changed`, `:plan component <name>`, `:plan env <env>`, `:run latest`, `:run latest dry-run`, `:logs failed`, `:logs <exec-id>`, `:describe component <name>`, `:describe job <job-id>`, `:filter type <type>`, `:filter env <env>`, `:history`, `:remote-state`.
7. AT MOST one overlay (command palette, help, confirmation) SHALL be visible at any time; opening a second overlay SHALL close the first.
8. WHEN the user presses `esc` with an overlay open, THE TUI SHALL close the overlay without changing the active mode.
9. WHEN the user presses `esc` at `PhaseFormFill` in Plan Studio, THE TUI SHALL NOT change the active mode or produce an error; other inputs at `PhaseFormFill` may change mode or cause errors.
10. THE TUI SHALL ensure `ActiveMode` is always a valid Mode constant (one of Browse, Plan Studio, Run Dashboard, Log Explorer, History) after any sequence of key messages.

---

### Requirement 10: Inspector Panel and Resource Description

**User Story:** As a developer, I want a persistent Inspector panel that shows structured detail for whatever resource I have selected, so that I can see inputs, environment variables, dependencies, gates, and available actions without switching views.

*Design reference: `design.md` §Architecture §2 (Three-Panel Layout), §Components §4 (InspectorModel), §Data Models — ResourceDescription*

#### Acceptance Criteria

1. WHEN a resource is selected in any mode, THE TUI SHALL call `OrunService.Describe` asynchronously regardless of the Inspector panel's current state, and display the resulting `ResourceDescription` in the Inspector panel when the `DescribeResultMsg` is received.
2. WHEN a `DescribeResultMsg` is received, THE TUI SHALL render the resource's kind, name, summary, labelled fields, and available actions in the Inspector panel.
3. WHEN no resource is selected, THE TUI SHALL render the Inspector panel in an empty state with a placeholder message.
4. WHEN the Inspector panel has focus, THE TUI SHALL allow the user to scroll through the description using arrow keys.
5. THE TUI SHALL support `Describe` for the following resource kinds: `component`, `job`, `plan`, `run`.

---

### Requirement 11: Remote State Integration

**User Story:** As a developer, I want to connect the TUI to a remote Orun backend, so that I can monitor and manage runs that execute on remote runners from the same cockpit interface.

*Design reference: `design.md` §Architecture §8 (Remote State Integration), §Correctness Properties — Property 10*

#### Acceptance Criteria

1. WHEN `--remote-state` is passed to `orun tui`, THE TUI SHALL construct `LiveOrunService` with a `RemoteStateBackend` instead of `FileStateBackend`.
2. WHEN using `RemoteStateBackend`, THE TUI SHALL resolve the authentication token via `remotestate.ResolveTokenSource` before constructing the backend client.
3. WHEN watching a remote execution in Run Dashboard, THE TUI SHALL poll `backend.LoadRunState` on each `TickMsg` (1-second interval) to refresh job statuses.
4. WHEN the remote state backend is completely unavailable, THE TUI SHALL display an error banner and fall back to showing cached local runs in the History view; authentication failures and network timeouts that do not result in complete unavailability SHALL be surfaced as error banners without triggering the fallback.
5. THE `OrunService.ListRuns` method SHALL return `RunSummary` values with the same field shape regardless of whether the backend is `FileStateBackend` or `RemoteStateBackend`.

---

### Requirement 12: Error Handling and Recovery

**User Story:** As a developer, I want the TUI to surface errors clearly and let me recover without restarting, so that transient failures in plan generation, execution, or log tailing do not force me to quit and relaunch.

*Design reference: `design.md` §Error Handling, §Correctness Properties — Properties 5, 8, 9*

#### Acceptance Criteria

1. WHEN any async operation returns an error, THE TUI SHALL wrap it in an `ErrMsg` and display a styled error banner at the top of the screen showing the error message.
2. WHEN an error banner is displayed, THE TUI SHALL clear it on the next key press.
3. WHEN `WorkspaceLoadedMsg` carries a non-nil error, THE TUI SHALL display the error banner and render the Navigator in an empty state; pressing `ctrl+r` SHALL retry `LoadWorkspace`.
4. WHEN `PlanGeneratedMsg` carries a non-nil error, THE TUI SHALL display the error banner and return Plan Studio to `PhaseFormFill`.
5. WHEN a `RunEventMsg` carries `Kind == RunEventJobFailed`, THE TUI SHALL mark the job row as failed and display the error text in the Inspector; the run SHALL continue for other jobs.
6. WHEN the context is cancelled (e.g. user presses `q`), THE TUI SHALL cancel all in-flight `OrunService` calls, close all streaming channels, and exit cleanly via `tea.Quit`.
7. WHEN a log tail channel closes unexpectedly, THE TUI SHALL display an error line in the log viewport and disable follow mode without crashing.

---

### Requirement 13: Service Layer — OrunService Interface

**User Story:** As a developer, I want the TUI to call Orun internal packages directly through a well-defined service interface, so that the TUI never shells out to `orun` subprocesses and remains testable with a mock service.

*Design reference: `design.md` §Architecture §4 (Service Layer Architecture), §Components §3 (OrunService Interface Definition)*

#### Acceptance Criteria

1. THE TUI SHALL define an `OrunService` interface with the following methods: `LoadWorkspace`, `GeneratePlan`, `RunPlan`, `ListRuns`, `Describe`, `TailLogs`.
2. THE `LiveOrunService` implementation SHALL call internal packages directly: `internal/discovery`, `internal/loader`, `internal/planner`, `internal/runner`, `internal/state`, `internal/statebackend`, `internal/remotestate`; it SHALL NOT use `exec.Command` to invoke `orun` subprocesses.
3. THE TUI SHALL provide a `MockOrunService` implementation that satisfies the `OrunService` interface for use in unit tests and property-based tests.
4. ALL `OrunService` methods SHALL accept a `context.Context` as their first argument and respect context cancellation.
5. THE `RunPlan` method SHALL return a `<-chan RunEvent` channel that is closed when the run completes or the context is cancelled.
6. THE `TailLogs` method SHALL return a `<-chan LogEvent` channel that is closed when the log file ends (non-follow) or the context is cancelled (follow mode).

---

### Requirement 14: State Machine Correctness

**User Story:** As a developer, I want the TUI state machines to be provably correct, so that no key sequence can put the cockpit into an invalid or inconsistent state.

*Design reference: `design.md` §Architecture §6 (State Machine for Plan-First Safety Flow), §Testing Strategy, §Correctness Properties — Properties 1–10*

#### Acceptance Criteria

1. THE TUI SHALL ensure `ActiveMode` is always one of the five defined Mode constants after any sequence of `tea.Msg` values.
2. THE TUI SHALL ensure `ActivePanel` cycles Navigator → Main → Inspector → Navigator after successive `tab` key messages; after `n` tab presses from the initial state, `ActivePanel == n % 3`.
3. THE TUI SHALL ensure at most one overlay (command palette, help, confirmation) is visible at any time.
4. THE TUI SHALL ensure `PhaseRunning` in Plan Studio is only reachable immediately after `PhaseConfirming` with an `enter` key message.
5. THE TUI SHALL ensure a `PlanGeneratedMsg` with a non-nil error always transitions Plan Studio to `PhaseFormFill`.
6. THE TUI SHALL ensure pressing `esc` at `PhaseFormFill` does not change the active mode or produce an error; other inputs at `PhaseFormFill` may change mode or cause errors.
7. THE TUI SHALL ensure the `WorkspaceSnapshot` pointer stored in `Model` is never replaced by view-level key messages; only a `WorkspaceLoadedMsg` may replace it, and `WorkspaceLoadedMsg` SHALL always be allowed to update the pointer.
8. THE TUI SHALL ensure that after a `RunEventRunDone` message, no further `WaitForRunEvent` commands are issued.
9. THE TUI SHALL ensure that when `LogRequest.Follow == false`, the log tail channel is closed after the last log line is read and the TUI does not block.
10. THE TUI SHALL ensure `OrunService.ListRuns` returns `RunSummary` values with the same field shape for both `FileStateBackend` and `RemoteStateBackend`.

---

### Requirement 15: Phase 4 Advanced Features

**User Story:** As a developer, I want advanced analysis tools — plan diff, failure workbench, and explain mode — so that I can deeply understand why the plan looks the way it does and diagnose failures without leaving the TUI.

*Design reference: `design.md` §Overview (Phase 4), `orun-tui-cockpit.md` §Features that make it "next level"*

#### Acceptance Criteria

1. WHERE plan diff is enabled (Phase 4), THE TUI SHALL display a diff between two selected plans showing added jobs, removed jobs, changed jobs, added/removed gates, and changes to composition source digests.
2. WHERE failure workbench is enabled (Phase 4) and a job has failed, THE TUI SHALL display the failure workbench; the workbench SHALL display whatever failure details are available (failed step, exit code, likely issue, available actions) and SHALL NOT be suppressed if some details are unavailable.
3. WHERE explain mode is enabled (Phase 4) and the user activates explain on a component or job node, THE TUI SHALL display: why the node is present in the plan, which profile was selected and why, which environment it targets, which dependency is blocking it (if any), and which composition source produced it.
