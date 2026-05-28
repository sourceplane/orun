# Design Document: orun-tui-cockpit

## Overview

`orun tui` is a component-native TUI control plane for Orun — not a wrapper around CLI subcommands, but a full-window interactive cockpit that makes the intent → component → composition → profile → plan DAG → execution record model visible, navigable, and actionable from the terminal.

The UI pattern is **k9s + lazygit + GitHub Actions run viewer**, adapted to Orun's component DAG. It is built on the Charm stack: **Bubble Tea** (Elm-style state/update/view), **Bubbles** (list, viewport, spinner, textinput widgets), and **Lip Gloss** (declarative terminal styling). The TUI calls internal packages directly — `internal/loader`, `internal/planner`, `internal/runner`, `internal/state`, `internal/statebackend` — and never shells out to `orun` subprocesses as its primary design.

The cockpit ships in four MVP phases: Phase 1 (read-only browse), Phase 2 (Plan Studio), Phase 3 (Execution Dashboard), Phase 4 (advanced features: plan diff, failure workbench, explain mode, remote cockpit).

---

## Architecture

### 1. System Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          orun CLI (Cobra)                               │
│  cmd/orun/command_tui.go  ──registerTuiCommand(root)──►  tuiCmd.RunE   │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │  tea.NewProgram(tui.NewModel(svc))
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                       internal/tui  (Bubble Tea app)                   │
│                                                                         │
│  app.go ──► model.go (root Model) ──► views/                           │
│                │                       ├── browse.go                   │
│                │                       ├── plan_studio.go              │
│                │                       ├── run_view.go                 │
│                │                       ├── log_explorer.go             │
│                │                       ├── history.go                  │
│                │                       ├── inspector.go                │
│                │                       └── command_palette.go          │
│                │                                                        │
│                └──► services/                                           │
│                       ├── orun_service.go  (OrunService interface)     │
│                       ├── plan_service.go  (GeneratePlan impl)         │
│                       ├── run_service.go   (RunPlan + streaming)       │
│                       ├── log_service.go   (TailLogs + history)        │
│                       └── history_service.go (ListRuns)                │
│                                                                         │
│  events/                                                                │
│    ├── run_events.go   (RunEvent channel bridge)                       │
│    └── log_tail.go     (LogEvent channel bridge)                       │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │  direct package calls (no exec.Command)
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     Orun Internal Packages                              │
│                                                                         │
│  internal/discovery   internal/loader    internal/normalize            │
│  internal/expand      internal/planner   internal/runner               │
│  internal/state       internal/statebackend  internal/remotestate      │
│  internal/model       internal/ui                                      │
└─────────────────────────────────────────────────────────────────────────┘
```

The Cobra command is a thin entry point. All TUI logic lives under `internal/tui/`. The service layer is the only code that touches internal Orun packages; views only call service methods and handle `tea.Msg` values.


### 2. Three-Panel Layout

```
┌─ Orun Cockpit ─────────────────────────────────────────────────────────────┐
│ repo: sourceplane/multi-tenant-saas   plan: a1b2c3d   run: running  [?]   │
├───────────────┬──────────────────────────────────────┬─────────────────────┤
│ NAVIGATOR     │ MAIN                                 │ INSPECTOR           │
│ (20 cols)     │ (flexible)                           │ (28 cols)           │
│               │                                      │                     │
│ ▶ Components  │  [mode-specific content]             │  [selected item]    │
│   Environments│                                      │  inputs / env       │
│   Plans       │  Browse: component table             │  policy / deps      │
│   Runs        │  Plan Studio: DAG + form             │  gates / actions    │
│   Jobs        │  Run Dashboard: live timeline        │  composition source │
│   Logs        │  Log Explorer: step log stream       │  commands           │
│   History     │  History: run list                   │                     │
│               │                                      │                     │
├───────────────┴──────────────────────────────────────┴─────────────────────┤
│ / search   g plan   d dry-run   r run   l logs   e env   tab focus   ? help│
└─────────────────────────────────────────────────────────────────────────────┘
```

**Panel responsibilities:**

| Panel | Width | Owns |
|---|---|---|
| Navigator | ~20 cols, fixed | Resource-type tree; arrow keys select category; `enter` loads into Main |
| Main | flexible, fills remaining | Mode-specific primary content: tables, DAG, live timeline, log stream |
| Inspector | ~28 cols, fixed | Detail pane for the item focused in Main: inputs, env, deps, actions |

Panel focus cycles with `tab`. Each panel renders independently; only the focused panel receives keyboard input beyond global bindings.


### 3. Five View Modes

| Mode | Entry | Primary content | Key actions |
|---|---|---|---|
| **Browse** | Default on launch | Component table: name, type, env, profile, path, changed, dep count, last run status | `/` fuzzy search, `e` env filter, `t` type filter, `c` changed-only, `d` dep tree, `p` generate plan |
| **Plan Studio** | `g` or Navigator → Plans | Plan scope form + job DAG + profile/gate summary | `d` dry-run, `r` run, `s` save named plan, `enter` inspect job |
| **Execution Dashboard** | `r` or Navigator → Runs | Live job timeline grouped by component: status icons, step progress, duration | `enter` inspect job, `l` logs, `r` retry failed, `s` step timeline |
| **Log Explorer** | `l` or Navigator → Logs | Left: job/step tree; Main: log stream; Right: failure summary | `f` follow, `e` errors-only, `c` copy command, `j` jump to first error |
| **History / Replay** | Navigator → History | Run list: exec-id, status, plan checksum, job counts, duration, trigger, git ref | `enter` inspect run, `l` logs, `r` replay, `d` diff vs previous |

Mode transitions are driven by key bindings and Navigator selection. The active mode is stored in `model.ActiveMode`.

### 4. Service Layer Architecture

The `OrunService` interface is the single boundary between TUI views and Orun internals. All implementations call internal packages directly.

```
OrunService (interface)
    │
    ├── WorkspaceService  ──► internal/discovery + internal/loader + internal/normalize
    ├── PlanService       ──► internal/planner + internal/expand + internal/state
    ├── RunService        ──► internal/runner + internal/state + internal/statebackend
    ├── LogService        ──► internal/state (log dir reader) + internal/remotestate
    └── HistoryService    ──► internal/state (ListExecutions + LoadMetadata)
```

The concrete `LiveOrunService` struct embeds all five sub-services and satisfies the full interface. A `MockOrunService` is provided for unit tests and property-based tests.


### 5. Event / Message Flow (Bubble Tea Msg Types)

Bubble Tea's `Update(msg tea.Msg)` function is the single dispatch point. All async operations return `tea.Cmd` values that produce typed `Msg` values when they complete.

```
User keystroke
    │
    ▼
model.Update(tea.KeyMsg)
    │
    ├── synchronous state mutation (focus, filter, cursor)
    │
    └── returns tea.Cmd (async work)
            │
            ▼
        goroutine / channel read
            │
            ▼
        typed Msg sent back to Update:
            WorkspaceLoadedMsg   — workspace snapshot ready
            PlanGeneratedMsg     — plan result ready (or error)
            RunEventMsg          — single RunEvent from streaming channel
            LogEventMsg          — single LogEvent from log tail channel
            RunsListedMsg        — history list ready
            DescribeResultMsg    — inspector detail ready
            TickMsg              — periodic refresh (1s) for live views
            WindowSizeMsg        — terminal resize (tea.WindowSizeMsg)
            ErrMsg               — any async error
```

Streaming channels (run events, log tail) are bridged into Bubble Tea's event loop via a `waitForMsg` command pattern: each `RunEventMsg` handler returns a new `waitForRunEvent(ch)` command, keeping the loop alive until the channel closes.

### 6. TUI Data Models

```go
// WorkspaceSnapshot is the read-only view of the current intent root.
type WorkspaceSnapshot struct {
    IntentRoot   string
    IntentName   string
    Components   []ComponentSummary
    Environments []string
    Plans        []PlanSummary
    LoadedAt     time.Time
}

type ComponentSummary struct {
    Name        string
    Type        string   // composition type
    Domain      string
    Path        string
    Envs        []string // subscribed environments
    Profile     string   // default profile
    DependsOn   []string
    Changed     bool
    LastRunStatus string  // "success" | "failed" | "running" | ""
}

type PlanSummary struct {
    Checksum    string
    Name        string
    GeneratedAt time.Time
    JobCount    int
    Components  []string
}

// PlanResult is returned by GeneratePlan.
type PlanResult struct {
    Plan        *model.Plan
    Checksum    string
    JobCount    int
    Components  []string
    Warnings    []string
    GeneratedAt time.Time
}

// RunEvent is a single event from a streaming run.
type RunEvent struct {
    Kind      RunEventKind  // JobStarted | JobCompleted | JobFailed | StepStarted | StepCompleted | RunDone
    JobID     string
    StepID    string
    Component string
    Env       string
    Status    string
    Error     string
    Timestamp time.Time
}

type RunEventKind string
const (
    RunEventJobStarted    RunEventKind = "job_started"
    RunEventJobCompleted  RunEventKind = "job_completed"
    RunEventJobFailed     RunEventKind = "job_failed"
    RunEventStepStarted   RunEventKind = "step_started"
    RunEventStepCompleted RunEventKind = "step_completed"
    RunEventRunDone       RunEventKind = "run_done"
)

// LogEvent is a single line from a log tail.
type LogEvent struct {
    JobID     string
    StepID    string
    Line      string
    IsError   bool
    Timestamp time.Time
}

// RunSummary is one row in the History view.
type RunSummary struct {
    ExecID      string
    PlanID      string
    PlanName    string
    Status      string
    JobTotal    int
    JobDone     int
    JobFailed   int
    StartedAt   time.Time
    FinishedAt  *time.Time
    Duration    time.Duration
    Trigger     string
    DryRun      bool
}
```


### 7. Key Binding Map and Command Palette

#### Global bindings (always active)

| Key | Action |
|---|---|
| `tab` / `shift+tab` | Cycle panel focus (Navigator → Main → Inspector) |
| `?` | Toggle help overlay |
| `:` | Open command palette |
| `/` | Open fuzzy search in current view |
| `q` / `ctrl+c` | Quit |
| `esc` | Close overlay / cancel / back |
| `ctrl+r` | Reload workspace snapshot |

#### Mode-specific bindings

| Key | Browse | Plan Studio | Run Dashboard | Log Explorer | History |
|---|---|---|---|---|---|
| `g` | Generate plan for selection | — | — | — | — |
| `d` | Show dep tree | Dry-run | — | — | Diff vs prev |
| `r` | — | Run plan | Retry failed job | — | Replay run |
| `l` | Open logs | — | Open logs for job | — | Open logs |
| `e` | Filter by env | — | — | Errors-only | — |
| `t` | Filter by type | — | — | — | — |
| `c` | Changed-only | — | — | Copy command | — |
| `f` | — | — | — | Follow/unfollow | — |
| `j` | — | — | — | Jump to first error | — |
| `s` | — | Save named plan | Step timeline | — | — |
| `enter` | Inspect item | Inspect job | Inspect job | — | Inspect run |
| `p` | Generate plan | — | — | — | — |

#### Command palette (`:` prefix)

```
:plan changed
:plan component <name>
:plan env <env>
:run latest
:run latest dry-run
:logs failed
:logs <exec-id>
:describe component <name>
:describe job <job-id>
:filter type terraform
:filter env prod
:history
:remote-state
```

### 8. Remote State Integration

When `--remote-state` is passed to `orun tui`, the `LiveOrunService` is constructed with a `statebackend.RemoteStateBackend` instead of the default `statebackend.FileStateBackend`. The TUI surface is identical; only the backend changes.

```
orun tui --remote-state --backend-url https://orun.example.com
    │
    ▼
tuiCmd.RunE resolves token via remotestate.ResolveTokenSource
    │
    ▼
LiveOrunService{backend: statebackend.NewRemoteStateBackend(client, runnerID)}
    │
    ▼
History view: backend.LoadRunState / ListExecutions (remote)
Run Dashboard: backend.ClaimJob / Heartbeat / UpdateJob (remote)
Log Explorer: backend.AppendStepLog / log fetch (remote)
```

Remote runs appear in the History view alongside local runs. The Run Dashboard polls `backend.LoadRunState` on a 1-second tick when watching a remote execution.


---

## Components and Interfaces

### 1. Package Structure under `internal/tui/`

```
internal/tui/
├── app.go                  # tea.NewProgram setup; terminal raw mode; resize handling
├── model.go                # Root Model struct, Init/Update/View; mode dispatch
├── keymap.go               # KeyMap struct (bubbletea/key bindings); help.KeyMap impl
├── theme.go                # Lip Gloss style constants (colors, borders, widths)
│
├── services/
│   ├── orun_service.go     # OrunService interface + request/response types
│   ├── live_service.go     # LiveOrunService: concrete impl using internal packages
│   ├── plan_service.go     # GeneratePlan: calls planner.Generate, returns PlanResult
│   ├── run_service.go      # RunPlan: calls runner.NewRunner, bridges to RunEvent channel
│   ├── log_service.go      # TailLogs: reads .orun/executions/{id}/logs/; remote fetch
│   └── history_service.go  # ListRuns: calls state.Store.ListExecutions + LoadMetadata
│
├── views/
│   ├── browse.go           # BrowseView: bubbles/list of ComponentSummary rows
│   ├── plan_studio.go      # PlanStudioView: scope form + DAG renderer + confirmation
│   ├── run_view.go         # RunView: live job timeline; polls RunEvent channel
│   ├── log_explorer.go     # LogExplorerView: step tree + viewport log stream
│   ├── history.go          # HistoryView: bubbles/list of RunSummary rows
│   ├── inspector.go        # InspectorView: detail pane; renders any ResourceDescription
│   ├── navigator.go        # NavigatorView: resource-type tree (Components/Plans/Runs/…)
│   └── command_palette.go  # CommandPaletteView: textinput + suggestion list
│
└── events/
    ├── run_events.go       # waitForRunEvent(ch <-chan RunEvent) tea.Cmd
    └── log_tail.go         # waitForLogEvent(ch <-chan LogEvent) tea.Cmd
```

**File responsibilities in detail:**

- `app.go` — constructs `tea.Program` with `tea.WithAltScreen()` and `tea.WithMouseCellMotion()`. Handles `tea.WindowSizeMsg` propagation to all panels.
- `model.go` — the root `Model` owns panel models, active mode, overlay state, and the `OrunService` reference. Its `Update` dispatches to the focused panel's `Update` first, then handles global keys.
- `keymap.go` — defines `GlobalKeyMap` and per-mode `KeyMap` structs using `github.com/charmbracelet/bubbles/key`. Implements `help.KeyMap` for the `?` overlay.
- `theme.go` — all `lipgloss.Style` values in one place. No inline style literals in view files.
- `services/orun_service.go` — the interface and all request/response types. No implementation here.
- `services/live_service.go` — the `LiveOrunService` struct. Holds `*state.Store`, `statebackend.Backend`, intent path, and config dir. Satisfies `OrunService`.


### 2. Core Bubble Tea Model Structure

```go
// internal/tui/model.go

package tui

import (
    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/bubbles/help"
    "github.com/sourceplane/orun/internal/tui/services"
    "github.com/sourceplane/orun/internal/tui/views"
)

// Mode identifies which primary view is active.
type Mode int

const (
    ModeBrowse    Mode = iota
    ModePlanStudio
    ModeRunDashboard
    ModeLogExplorer
    ModeHistory
)

// Panel identifies which panel has keyboard focus.
type Panel int

const (
    PanelNavigator Panel = iota
    PanelMain
    PanelInspector
)

// Model is the root Bubble Tea model.
type Model struct {
    // Layout
    width, height int
    activeMode    Mode
    activePanel   Panel

    // Panel models
    navigator  views.NavigatorModel
    main       tea.Model  // swapped per mode
    inspector  views.InspectorModel

    // Overlay models
    commandPalette views.CommandPaletteModel
    helpOverlay    help.Model
    confirmation   *ConfirmationPane

    // Overlays active?
    showCommandPalette bool
    showHelp           bool
    showConfirmation   bool

    // Service
    svc services.OrunService

    // Workspace state (loaded async)
    workspace *services.WorkspaceSnapshot

    // Error banner
    lastErr error
}

// Init loads the workspace snapshot on startup.
func (m Model) Init() tea.Cmd {
    return loadWorkspaceCmd(m.svc)
}

// Update is the single dispatch point for all messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)

// View renders the full terminal frame.
func (m Model) View() string
```

**Msg types** (all in `model.go` or `services/orun_service.go`):

```go
type WorkspaceLoadedMsg  struct { Snapshot *services.WorkspaceSnapshot; Err error }
type PlanGeneratedMsg    struct { Result *services.PlanResult; Err error }
type RunEventMsg         struct { Event services.RunEvent }
type LogEventMsg         struct { Event services.LogEvent }
type RunsListedMsg       struct { Runs []services.RunSummary; Err error }
type DescribeResultMsg   struct { Desc *services.ResourceDescription; Err error }
type ErrMsg              struct { Err error }
type TickMsg             struct{}
```


### 3. OrunService Interface Definition

```go
// internal/tui/services/orun_service.go

package services

import (
    "context"
    "time"
    "github.com/sourceplane/orun/internal/model"
)

// OrunService is the single boundary between TUI views and Orun internals.
// All methods are safe to call from a tea.Cmd goroutine.
type OrunService interface {
    // LoadWorkspace discovers the intent root and returns a snapshot of all
    // components, environments, and saved plans. Called on startup and on ctrl+r.
    LoadWorkspace(ctx context.Context, req WorkspaceRequest) (*WorkspaceSnapshot, error)

    // GeneratePlan compiles a plan from the current intent using the planner.
    // Never shells out; calls internal/planner directly.
    GeneratePlan(ctx context.Context, req PlanRequest) (*PlanResult, error)

    // RunPlan executes a compiled plan and streams RunEvents on the returned channel.
    // The channel is closed when the run completes or the context is cancelled.
    RunPlan(ctx context.Context, req RunRequest) (<-chan RunEvent, error)

    // ListRuns returns execution summaries from the local state store (or remote backend).
    ListRuns(ctx context.Context, req ListRunsRequest) ([]RunSummary, error)

    // Describe returns structured detail for any resource ref (component, job, plan, run).
    Describe(ctx context.Context, ref ResourceRef) (*ResourceDescription, error)

    // TailLogs streams log lines for a job/step. The channel is closed when
    // the log file ends (or the context is cancelled for live tailing).
    TailLogs(ctx context.Context, req LogRequest) (<-chan LogEvent, error)
}

// --- Request types ---

type WorkspaceRequest struct {
    IntentFile string // auto-discovered if empty
    ConfigDir  string
    All        bool   // disable CWD scoping
}

type PlanRequest struct {
    IntentFile   string
    ConfigDir    string
    Components   []string
    Environment  string
    ChangedOnly  bool
    BaseBranch   string
    HeadRef      string
    TriggerName  string
    NamedPlan    string
}

type RunRequest struct {
    Plan        *model.Plan
    ExecID      string
    DryRun      bool
    JobID       string   // empty = run all
    Components  []string
    Concurrency int
    WorkDir     string
    RemoteState bool
    BackendURL  string
}

type ListRunsRequest struct {
    Limit       int
    RemoteState bool
    BackendURL  string
}

type LogRequest struct {
    ExecID      string
    JobID       string
    StepID      string  // empty = all steps for the job
    Follow      bool    // tail live logs
    RemoteState bool
    BackendURL  string
}

type ResourceRef struct {
    Kind string // "component" | "job" | "plan" | "run"
    Name string
    Env  string
}

// --- Response types (defined in Section 6 of HLD, repeated here for completeness) ---
// WorkspaceSnapshot, PlanResult, RunEvent, LogEvent, RunSummary, ResourceDescription
// are defined in services/orun_service.go alongside the interface.

type ResourceDescription struct {
    Kind       string
    Name       string
    Summary    string
    Fields     []DescField
    Actions    []string  // available key actions for this resource
}

type DescField struct {
    Label string
    Value string
    Dim   bool
}
```


### 4. Key View Structs and Update/View Signatures

Each view is a self-contained Bubble Tea model. The root `Model` holds the active view as a `tea.Model` interface value and delegates `Update`/`View` calls to it.

```go
// views/browse.go
type BrowseModel struct {
    list      list.Model          // bubbles/list
    filter    BrowseFilter
    workspace *services.WorkspaceSnapshot
    svc       services.OrunService
}
func (m BrowseModel) Init() tea.Cmd
func (m BrowseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m BrowseModel) View() string

// views/plan_studio.go
type PlanStudioModel struct {
    scopeForm   ScopeForm           // textinput fields for scope/env/trigger
    planResult  *services.PlanResult
    dagView     DAGRenderer         // ASCII DAG from plan jobs + DependsOn edges
    phase       PlanStudioPhase     // FormFill | Generating | Review | Confirming
    svc         services.OrunService
}
func (m PlanStudioModel) Init() tea.Cmd
func (m PlanStudioModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m PlanStudioModel) View() string

// views/run_view.go
type RunViewModel struct {
    execID      string
    events      []services.RunEvent  // accumulated for replay
    eventCh     <-chan services.RunEvent
    jobRows     []JobRow             // derived from events; sorted by component
    phase       RunPhase             // Running | Done | Failed
    svc         services.OrunService
}
func (m RunViewModel) Init() tea.Cmd
func (m RunViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m RunViewModel) View() string

// views/log_explorer.go
type LogExplorerModel struct {
    jobTree     list.Model
    logViewport viewport.Model      // bubbles/viewport for scrollable log
    lines       []services.LogEvent
    logCh       <-chan services.LogEvent
    follow      bool
    errorsOnly  bool
    svc         services.OrunService
}
func (m LogExplorerModel) Init() tea.Cmd
func (m LogExplorerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m LogExplorerModel) View() string

// views/history.go
type HistoryModel struct {
    list   list.Model
    runs   []services.RunSummary
    svc    services.OrunService
}
func (m HistoryModel) Init() tea.Cmd
func (m HistoryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m HistoryModel) View() string

// views/inspector.go
type InspectorModel struct {
    desc     *services.ResourceDescription
    viewport viewport.Model
}
func (m InspectorModel) Init() tea.Cmd
func (m InspectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m InspectorModel) View() string

// views/navigator.go
type NavigatorModel struct {
    items    []NavItem
    cursor   int
    focused  bool
}
func (m NavigatorModel) Init() tea.Cmd
func (m NavigatorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m NavigatorModel) View() string

// views/command_palette.go
type CommandPaletteModel struct {
    input       textinput.Model
    suggestions []string
    selected    int
}
func (m CommandPaletteModel) Init() tea.Cmd
func (m CommandPaletteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m CommandPaletteModel) View() string
```


### 5. Event Channel Patterns for Streaming

Bubble Tea's event loop is single-threaded. Streaming channels are bridged using the `waitForMsg` pattern: a `tea.Cmd` blocks on a channel read and returns the next message to the event loop. The handler for that message returns a new `waitForMsg` command, keeping the loop alive.

```go
// events/run_events.go

package events

import (
    tea "github.com/charmbracelet/bubbletea"
    "github.com/sourceplane/orun/internal/tui/services"
)

// WaitForRunEvent returns a Cmd that blocks until the next RunEvent arrives
// on ch, then wraps it in a RunEventMsg. Returns nil when ch is closed.
func WaitForRunEvent(ch <-chan services.RunEvent) tea.Cmd {
    return func() tea.Msg {
        event, ok := <-ch
        if !ok {
            return services.RunEventMsg{Event: services.RunEvent{Kind: services.RunEventRunDone}}
        }
        return services.RunEventMsg{Event: event}
    }
}

// events/log_tail.go

// WaitForLogEvent returns a Cmd that blocks until the next LogEvent arrives.
func WaitForLogEvent(ch <-chan services.LogEvent) tea.Cmd {
    return func() tea.Msg {
        event, ok := <-ch
        if !ok {
            return services.LogEventMsg{Event: services.LogEvent{Line: ""}} // sentinel: channel closed
        }
        return services.LogEventMsg{Event: event}
    }
}
```

**Usage in RunViewModel.Update:**

```go
case services.RunEventMsg:
    m.events = append(m.events, msg.Event)
    m.jobRows = rebuildJobRows(m.events)
    if msg.Event.Kind == services.RunEventRunDone {
        m.phase = RunPhaseDone
        return m, nil
    }
    return m, events.WaitForRunEvent(m.eventCh)  // re-arm
```

**Run service bridge** — `RunService.RunPlan` starts the runner in a goroutine and writes `RunEvent` values to a buffered channel:

```go
func (s *RunService) RunPlan(ctx context.Context, req RunRequest) (<-chan RunEvent, error) {
    ch := make(chan RunEvent, 64)
    r := runner.NewRunner(/* ... */)
    r.Hooks = &runner.RunnerHooks{
        BeforeJob: func(jobID string) (bool, error) {
            ch <- RunEvent{Kind: RunEventJobStarted, JobID: jobID, Timestamp: time.Now()}
            return false, nil
        },
        AfterJobTerminal: func(jobID string, success bool, errText string) {
            kind := RunEventJobCompleted
            if !success { kind = RunEventJobFailed }
            ch <- RunEvent{Kind: kind, JobID: jobID, Error: errText, Timestamp: time.Now()}
        },
    }
    go func() {
        defer close(ch)
        _ = r.Run(req.Plan)
        ch <- RunEvent{Kind: RunEventRunDone, Timestamp: time.Now()}
    }()
    return ch, nil
}
```


### 6. State Machine for Plan-First Safety Flow

The Plan Studio enforces a linear safety flow. Execution can only be triggered after the user has reviewed the plan and confirmed the confirmation pane.

```
┌──────────────┐
│  FormFill    │  User fills scope/env/trigger form
└──────┬───────┘
       │ g (generate)
       ▼
┌──────────────┐
│  Generating  │  PlanGeneratedMsg pending (spinner shown)
└──────┬───────┘
       │ PlanGeneratedMsg{Result, nil}
       ▼
┌──────────────┐
│   Review     │  DAG + job list shown; d=dry-run, r=run, s=save, esc=back
└──────┬───────┘
       │ r (run) or d (dry-run)
       ▼
┌──────────────┐
│  Confirming  │  ConfirmationPane shown (see §7); enter=confirm, esc=cancel
└──────┬───────┘
       │ enter (confirm)
       ▼
┌──────────────┐
│   Running    │  RunPlan called; transitions to RunDashboard mode
└──────────────┘

Error path: PlanGeneratedMsg{Err != nil} → back to FormFill with error banner
Cancel path: esc at any phase → back to FormFill
```

```go
type PlanStudioPhase int

const (
    PhaseFormFill   PlanStudioPhase = iota
    PhaseGenerating
    PhaseReview
    PhaseConfirming
    PhaseRunning
)
```

Phase transitions are pure state mutations in `PlanStudioModel.Update`. No side effects occur until `PhaseConfirming → PhaseRunning` (when `RunPlan` is called via a `tea.Cmd`).


### 7. Confirmation Pane Data Structure

The confirmation pane is a modal overlay rendered over the Plan Studio Review phase. It must be dismissed with an explicit `enter` before execution proceeds.

```go
// internal/tui/model.go

// ConfirmationPane holds the data rendered in the execution confirmation modal.
type ConfirmationPane struct {
    // What will run
    Scope        string    // "full" | "changed" | "component: api-edge-worker" | "env: prod"
    Environments []string
    Components   []string
    JobCount     int
    PlanChecksum string

    // Risk signals
    DestructiveCapabilities []string  // e.g. ["terraform.apply", "helm.upgrade"]
    Gates                   []string  // promotion gate names that will be evaluated
    HasGates                bool

    // Execution config
    DryRun      bool
    Runner      string  // "local" | "github-actions" | "docker"
    Concurrency int
    RemoteState bool

    // Confirmation state
    Confirmed bool
}

// renderConfirmationPane renders the pane using Lip Gloss borders.
// Returns a string ready to be overlaid on the main view.
func renderConfirmationPane(p *ConfirmationPane, theme Theme) string
```

The pane is populated from `PlanResult` fields when the user presses `r` in the Review phase. It is cleared on `esc` or after the run starts.


### 8. `cmd/orun/command_tui.go` — Cobra Registration Pattern

```go
// cmd/orun/command_tui.go
package main

import (
    "context"
    "fmt"
    "os"

    tea "github.com/charmbracelet/bubbletea"
    "github.com/sourceplane/orun/internal/tui"
    "github.com/sourceplane/orun/internal/tui/services"
    "github.com/sourceplane/orun/internal/remotestate"
    "github.com/sourceplane/orun/internal/state"
    "github.com/sourceplane/orun/internal/statebackend"
    "github.com/spf13/cobra"
)

var (
    tuiRemoteState bool
    tuiBackendURL  string
)

var tuiCmd = &cobra.Command{
    Use:   "tui",
    Short: "Open the Orun Cockpit TUI",
    Long:  "Launch the interactive Orun Cockpit: browse components, generate plans, run, and inspect logs.",
    RunE: func(cmd *cobra.Command, args []string) error {
        return runTUI()
    },
}

func registerTuiCommand(root *cobra.Command) {
    root.AddCommand(tuiCmd)
    tuiCmd.Flags().BoolVar(&tuiRemoteState, "remote-state", false, "Connect to orun-backend for remote run state")
    tuiCmd.Flags().StringVar(&tuiBackendURL, "backend-url", "", "orun-backend URL (or set ORUN_BACKEND_URL)")
}

func runTUI() error {
    store := state.NewStore(storeDir())

    var backend statebackend.Backend
    if tuiRemoteState {
        backendURL := tuiBackendURL
        if backendURL == "" {
            backendURL = os.Getenv(backendURLEnvVar)
        }
        if backendURL == "" {
            return fmt.Errorf("--remote-state requires --backend-url or ORUN_BACKEND_URL")
        }
        b, err := newRemoteBackend(backendURL)
        if err != nil {
            return fmt.Errorf("remote state: %w", err)
        }
        defer b.Close(context.Background())
        backend = b
    } else {
        backend = statebackend.NewFileStateBackend(store)
    }

    svc := services.NewLiveOrunService(services.LiveServiceConfig{
        IntentFile: intentFile,
        IntentRoot: intentRoot,
        ConfigDir:  configDir,
        Store:      store,
        Backend:    backend,
        Version:    version,
    })

    m := tui.NewModel(svc)
    p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
    _, err := p.Run()
    return err
}
```

`registerTuiCommand` is called from `commands_root.go` `init()` alongside all other `registerXxxCommand` calls.


### 9. `go.mod` Additions

The Charm stack is not yet in `go.mod`. Add these pinned direct dependencies:

```
require (
    github.com/charmbracelet/bubbletea  v1.3.5
    github.com/charmbracelet/bubbles    v0.21.0
    github.com/charmbracelet/lipgloss   v1.1.0
)
```

**Version rationale:**
- `bubbletea v1.3.5` — latest stable v1 release; v1 API is stable and removes the deprecated `tea.Quit` in favour of `tea.Quit` cmd.
- `bubbles v0.21.0` — compatible with bubbletea v1; includes `list`, `viewport`, `textinput`, `spinner`, `help` components used by the TUI.
- `lipgloss v1.1.0` — compatible with bubbletea v1; stable CSS-like style API.

These three packages have no transitive dependencies outside the Go standard library and `golang.org/x/term` (already in `go.mod`). Run `go mod tidy` after adding them to populate `go.sum`.

**Indirect dependencies** that `go mod tidy` will add:
- `github.com/charmbracelet/x/ansi` (lipgloss dependency)
- `github.com/charmbracelet/x/term` (bubbletea dependency)
- `github.com/muesli/termenv` (lipgloss color detection)
- `github.com/lucasb-eyer/go-colorful` (lipgloss color math)
- `github.com/atotto/clipboard` (optional; only if copy-to-clipboard is implemented)


### 10. Property-Based Testing Approach for TUI State Transitions

The TUI's correctness properties are concentrated in two areas: (a) the root `Model` state machine (mode/panel transitions, overlay lifecycle) and (b) the `PlanStudioModel` phase state machine (plan-first safety flow). Both are pure functions of `(Model, tea.Msg) → (Model, tea.Cmd)` and are ideal for property-based testing.

**Library:** `github.com/flyingmutant/rapid` (Go property-based testing; no external dependency on Haskell/Python toolchains).

#### Properties to test

**Root model — mode and panel transitions:**

```go
// Property: ActiveMode is always a valid Mode constant after any key message.
// No key sequence should produce an out-of-range mode value.
rapid.Check(t, func(t *rapid.T) {
    m := tui.NewModel(mockSvc)
    keys := rapid.SliceOf(rapid.SampledFrom(allKeyMsgs)).Draw(t, "keys")
    for _, k := range keys {
        next, _ := m.Update(k)
        m = next.(tui.Model)
    }
    assert.True(t, m.ActiveMode() >= tui.ModeBrowse && m.ActiveMode() <= tui.ModeHistory)
})

// Property: Panel focus cycles Navigator → Main → Inspector → Navigator.
// After N tab presses, panel == N % 3.
rapid.Check(t, func(t *rapid.T) {
    m := tui.NewModel(mockSvc)
    n := rapid.IntRange(0, 20).Draw(t, "n")
    for i := 0; i < n; i++ {
        next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
        m = next.(tui.Model)
    }
    assert.Equal(t, tui.Panel(n%3), m.ActivePanel())
})

// Property: esc always closes any open overlay without changing the active mode.
rapid.Check(t, func(t *rapid.T) {
    m := tui.NewModel(mockSvc)
    // Open command palette
    m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
    m2Model := m2.(tui.Model)
    assert.True(t, m2Model.CommandPaletteVisible())
    // esc closes it
    m3, _ := m2Model.Update(tea.KeyMsg{Type: tea.KeyEsc})
    m3Model := m3.(tui.Model)
    assert.False(t, m3Model.CommandPaletteVisible())
    assert.Equal(t, m2Model.ActiveMode(), m3Model.ActiveMode())
})
```

**PlanStudioModel — phase state machine:**

```go
// Property: PhaseRunning is only reachable after PhaseConfirming with enter.
// No sequence of messages should skip the confirmation step.
rapid.Check(t, func(t *rapid.T) {
    m := views.NewPlanStudioModel(mockSvc)
    msgs := rapid.SliceOf(rapid.SampledFrom(planStudioMsgs)).Draw(t, "msgs")
    prevPhase := m.Phase()
    for _, msg := range msgs {
        next, _ := m.Update(msg)
        m = next.(views.PlanStudioModel)
        if m.Phase() == views.PhaseRunning {
            assert.Equal(t, views.PhaseConfirming, prevPhase,
                "PhaseRunning must only follow PhaseConfirming")
        }
        prevPhase = m.Phase()
    }
})

// Property: esc at any phase returns to PhaseFormFill (never leaves the studio in an invalid state).
rapid.Check(t, func(t *rapid.T) {
    m := views.NewPlanStudioModel(mockSvc)
    // Advance to a random phase
    phase := rapid.SampledFrom([]views.PlanStudioPhase{
        views.PhaseReview, views.PhaseConfirming,
    }).Draw(t, "phase")
    m = m.WithPhase(phase) // test helper setter
    next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
    assert.Equal(t, views.PhaseFormFill, next.(views.PlanStudioModel).Phase())
})

// Property: PlanGeneratedMsg with a non-nil error always transitions to PhaseFormFill.
rapid.Check(t, func(t *rapid.T) {
    m := views.NewPlanStudioModel(mockSvc).WithPhase(views.PhaseGenerating)
    next, _ := m.Update(services.PlanGeneratedMsg{Err: errors.New("planner error")})
    assert.Equal(t, views.PhaseFormFill, next.(views.PlanStudioModel).Phase())
})
```

**WorkspaceSnapshot immutability:**

```go
// Property: LoadWorkspace result is never mutated by view updates.
// The snapshot stored in Model should be the same pointer after any key sequence.
rapid.Check(t, func(t *rapid.T) {
    m := tui.NewModel(mockSvc)
    snap := &services.WorkspaceSnapshot{IntentName: "test"}
    m2, _ := m.Update(services.WorkspaceLoadedMsg{Snapshot: snap})
    keys := rapid.SliceOf(rapid.SampledFrom(allKeyMsgs)).Draw(t, "keys")
    current := m2.(tui.Model)
    for _, k := range keys {
        next, _ := current.Update(k)
        current = next.(tui.Model)
    }
    assert.Same(t, snap, current.Workspace())
})
```

Tests live in `internal/tui/model_pbt_test.go` and `internal/tui/views/plan_studio_pbt_test.go`. Run with `go test ./internal/tui/... -count=1`.


---

## Data Models

The following types are defined in `internal/tui/services/orun_service.go` and represent the read-only data exchanged between the service layer and TUI views.

```go
// WorkspaceSnapshot is the read-only view of the current intent root.
type WorkspaceSnapshot struct {
    IntentRoot   string
    IntentName   string
    Components   []ComponentSummary
    Environments []string
    Plans        []PlanSummary
    LoadedAt     time.Time
}

type ComponentSummary struct {
    Name          string
    Type          string   // composition type
    Domain        string
    Path          string
    Envs          []string // subscribed environments
    Profile       string   // default profile
    DependsOn     []string
    Changed       bool
    LastRunStatus string   // "success" | "failed" | "running" | ""
}

type PlanSummary struct {
    Checksum    string
    Name        string
    GeneratedAt time.Time
    JobCount    int
    Components  []string
}

// PlanResult is returned by GeneratePlan.
type PlanResult struct {
    Plan        *model.Plan
    Checksum    string
    JobCount    int
    Components  []string
    Warnings    []string
    GeneratedAt time.Time
}

// RunEvent is a single event from a streaming run.
type RunEvent struct {
    Kind      RunEventKind  // JobStarted | JobCompleted | JobFailed | StepStarted | StepCompleted | RunDone
    JobID     string
    StepID    string
    Component string
    Env       string
    Status    string
    Error     string
    Timestamp time.Time
}

type RunEventKind string
const (
    RunEventJobStarted    RunEventKind = "job_started"
    RunEventJobCompleted  RunEventKind = "job_completed"
    RunEventJobFailed     RunEventKind = "job_failed"
    RunEventStepStarted   RunEventKind = "step_started"
    RunEventStepCompleted RunEventKind = "step_completed"
    RunEventRunDone       RunEventKind = "run_done"
)

// LogEvent is a single line from a log tail.
type LogEvent struct {
    JobID     string
    StepID    string
    Line      string
    IsError   bool
    Timestamp time.Time
}

// RunSummary is one row in the History view.
type RunSummary struct {
    ExecID     string
    PlanID     string
    PlanName   string
    Status     string
    JobTotal   int
    JobDone    int
    JobFailed  int
    StartedAt  time.Time
    FinishedAt *time.Time
    Duration   time.Duration
    Trigger    string
    DryRun     bool
}

// ResourceDescription is returned by Describe for any resource ref.
type ResourceDescription struct {
    Kind    string
    Name    string
    Summary string
    Fields  []DescField
    Actions []string  // available key actions for this resource
}

type DescField struct {
    Label string
    Value string
    Dim   bool
}
```

**Validation Rules:**
- `ComponentSummary.LastRunStatus` must be one of `"success"`, `"failed"`, `"running"`, or `""`.
- `RunEvent.Kind` must be one of the defined `RunEventKind` constants.
- `PlanResult.Checksum` is a non-empty string identifying the plan deterministically.
- `WorkspaceSnapshot` is immutable after construction; views must not mutate it.

---

## Error Handling

### Error Propagation via ErrMsg

All async errors are surfaced through the `ErrMsg` type, which is the single error channel into the Bubble Tea event loop:

```go
type ErrMsg struct { Err error }
```

Any `tea.Cmd` that encounters an error wraps it in `ErrMsg` and returns it to `model.Update`. The root model stores the error in `lastErr` and renders an error banner at the top of the screen.

### Error Banner

When `lastErr != nil`, the root `View()` renders a styled error banner above the three-panel layout:

```
┌─ ERROR ──────────────────────────────────────────────────────────────────┐
│ planner: component "api-edge-worker" has unresolved dependency "db-core" │
│ Press any key to dismiss                                                 │
└──────────────────────────────────────────────────────────────────────────┘
```

Any subsequent key press clears `lastErr` (the next `Update` call sets it to `nil`).

### Error Scenarios and Recovery

| Scenario | Trigger | Response | Recovery |
|---|---|---|---|
| Workspace load failure | `WorkspaceLoadedMsg{Err != nil}` | Error banner; Navigator shows empty state | `ctrl+r` retries `LoadWorkspace` |
| Plan generation failure | `PlanGeneratedMsg{Err != nil}` | Error banner; Plan Studio returns to `PhaseFormFill` | User corrects scope and presses `g` again |
| Run start failure | `RunPlan` returns error | Error banner; Run Dashboard not entered | User returns to Plan Studio |
| Streaming run error | `RunEventMsg` with `Kind == RunEventJobFailed` | Job row marked failed with red icon; error text shown in Inspector | `r` retries the failed job |
| Log tail failure | `LogEventMsg` channel closed with error | Log viewport shows error line; follow mode disabled | User re-opens log explorer |
| Remote state unavailable | `ErrMsg` from backend call | Error banner; History view shows cached local runs | User checks `--backend-url` and retries |
| Context cancellation | `ctx.Done()` in any service method | Channel closed; `ErrMsg` with `context.Canceled` | Graceful shutdown; TUI exits cleanly |

### Context Cancellation

All `OrunService` methods accept a `context.Context`. The root model passes a cancellable context derived from the program's lifecycle. When the user presses `q` or `ctrl+c`, the context is cancelled before `tea.Quit` is returned, ensuring all in-flight goroutines (run streaming, log tailing) terminate cleanly.

---

## Testing Strategy

### Unit Testing Approach

Each view model (`BrowseModel`, `PlanStudioModel`, `RunViewModel`, etc.) is tested in isolation using `MockOrunService`. Tests verify:
- Correct `tea.Cmd` is returned for each input message
- State fields are updated correctly after `Update`
- `View()` output contains expected strings for key states

Test files follow the pattern `internal/tui/views/*_test.go`.

### Property-Based Testing Approach

The TUI's state machines are pure functions of `(Model, tea.Msg) → (Model, tea.Cmd)` and are ideal for property-based testing.

**Property Test Library**: `github.com/flyingmutant/rapid` (Go property-based testing; no external dependency on Haskell/Python toolchains).

Tests live in `internal/tui/model_pbt_test.go` and `internal/tui/views/plan_studio_pbt_test.go`. Run with `go test ./internal/tui/... -count=1`.

The properties tested are enumerated in the Correctness Properties section below. Key generators used:
- `rapid.SampledFrom(allKeyMsgs)` — random key messages from the full key binding map
- `rapid.SampledFrom(planStudioMsgs)` — messages relevant to Plan Studio phase transitions
- `rapid.IntRange(0, 20)` — random tab-press counts for panel cycle verification

### Integration Testing Approach

Integration tests use `LiveOrunService` against a fixture intent directory under `testdata/`. They verify:
- `LoadWorkspace` correctly discovers components from a real intent file
- `GeneratePlan` produces a valid `PlanResult` for a known fixture
- `ListRuns` returns results from a pre-populated state store fixture

These tests are tagged `//go:build integration` and run separately from unit tests.

---

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system — essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

The following properties must hold across all TUI state transitions:

### Property 1: Mode validity

`ActiveMode` is always in `[ModeBrowse, ModeHistory]` after any sequence of messages.

**Validates: Requirements 9.10, 14.1**

### Property 2: Panel cycle

After `n` tab presses from the initial state, `ActivePanel == n % 3`.

**Validates: Requirements 2.4, 14.2**

### Property 3: Overlay exclusivity

At most one overlay (command palette, help, confirmation) is visible at any time.

**Validates: Requirements 9.7, 14.3**

### Property 4: Confirmation gate

`PhaseRunning` in Plan Studio is only reachable immediately after `PhaseConfirming` with an `enter` key message.

**Validates: Requirements 5.1, 14.4**

### Property 5: Error recovery

A `PlanGeneratedMsg` with a non-nil error always returns the Plan Studio to `PhaseFormFill`.

**Validates: Requirements 4.4, 12.4, 14.5**

### Property 6: esc idempotence

Pressing `esc` at `PhaseFormFill` does not change the active mode or produce an error.

**Validates: Requirements 9.9, 14.6**

### Property 7: Workspace immutability

The `WorkspaceSnapshot` pointer stored in `Model` is never replaced by view-level key messages; only `WorkspaceLoadedMsg` replaces it.

**Validates: Requirements 14.7**

### Property 8: Channel safety

After a `RunEventRunDone` message, no further `WaitForRunEvent` commands are issued (the channel bridge is disarmed).

**Validates: Requirements 6.4, 14.8**

### Property 9: Log follow termination

When `Follow == false`, the log tail channel is closed after the last line of the log file is read; the TUI does not block.

**Validates: Requirements 7.7, 14.9**

### Property 10: Remote/local symmetry

`ListRuns` returns the same `RunSummary` shape regardless of whether the backend is `FileStateBackend` or `RemoteStateBackend`.

**Validates: Requirements 8.6, 11.5, 14.10**

---

## Dependencies

| Package | Version | Purpose |
|---|---|---|
| `github.com/charmbracelet/bubbletea` | `v1.3.5` | Elm-style TUI framework |
| `github.com/charmbracelet/bubbles` | `v0.21.0` | List, viewport, textinput, spinner, help widgets |
| `github.com/charmbracelet/lipgloss` | `v1.1.0` | Declarative terminal styling and layout |
| `github.com/flyingmutant/rapid` | `v1.1.0` | Property-based testing (test-only) |
| `github.com/sourceplane/orun/internal/loader` | (internal) | Intent loading |
| `github.com/sourceplane/orun/internal/planner` | (internal) | Plan generation |
| `github.com/sourceplane/orun/internal/runner` | (internal) | Plan execution |
| `github.com/sourceplane/orun/internal/state` | (internal) | Execution state store |
| `github.com/sourceplane/orun/internal/statebackend` | (internal) | Local and remote state backends |
| `github.com/sourceplane/orun/internal/model` | (internal) | Plan, Job, Component, Intent types |
| `github.com/sourceplane/orun/internal/discovery` | (internal) | Intent/component file discovery |
| `github.com/sourceplane/orun/internal/remotestate` | (internal) | Remote state client and token resolution |
| `github.com/sourceplane/orun/internal/ui` | (internal) | Color/style utilities (reused for non-TUI output) |

